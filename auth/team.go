package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"golang.org/x/crypto/bcrypt"

	"encore.app/wabantu/audit"
	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/system"
	"encore.app/wabantu/usage"
)

// TeamMember is a tenant account visible to the owner.
type TeamMember struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Name      *string    `json:"name"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"createdAt"`
	LastLogin *time.Time `json:"lastLoginAt,omitempty"`
}

type ListTeamResponse struct {
	Members []TeamMember `json:"members"`
	Total   int          `json:"total"`
}

type InviteStaffRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type InviteStaffResponse struct {
	Member TeamMember `json:"member"`
}

//encore:api auth method=GET path=/api/v1/team/members tag:owner
func ListTeamMembers(ctx context.Context) (*ListTeamResponse, error) {
	u, err := requireOwner(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := system.DB.Query(ctx, `
		SELECT id, email, name, role, created_at, last_login_at
		FROM tenant_account
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC`, u.TenantID)
	if err != nil {
		return nil, errs.Internal("list team failed")
	}
	defer rows.Close()

	members := make([]TeamMember, 0)
	for rows.Next() {
		var m TeamMember
		var name sql.NullString
		if err := rows.Scan(&m.ID, &m.Email, &name, &m.Role, &m.CreatedAt, &m.LastLogin); err != nil {
			return nil, errs.Internal("scan team member failed")
		}
		if name.Valid {
			n := name.String
			m.Name = &n
		}
		members = append(members, m)
	}
	return &ListTeamResponse{Members: members, Total: len(members)}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/team/members tag:owner
func InviteStaff(ctx context.Context, req *InviteStaffRequest) (*InviteStaffResponse, error) {
	u, err := requireOwner(ctx)
	if err != nil {
		return nil, err
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || len(req.Password) < 8 {
		return nil, errs.BadRequest("email and password (min 8) required")
	}

	var seatCount int
	if err := system.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM tenant_account WHERE tenant_id = $1 AND deleted_at IS NULL`,
		u.TenantID,
	).Scan(&seatCount); err != nil {
		return nil, errs.Internal("seat count failed")
	}
	plan := usage.TenantPlan(ctx, u.TenantSchema)
	_, _, limit := usage.CheckQuota(ctx, u.TenantSchema, "admin_seat")
	if limit > 0 && seatCount >= limit {
		return nil, errs.Forbidden(fmt.Sprintf("admin seat limit reached (%d) for plan %s", limit, plan))
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		return nil, errs.Internal("password hash failed")
	}
	emailHash := hashForLookup(email)
	name := strings.TrimSpace(req.Name)
	var namePtr *string
	if name != "" {
		namePtr = &name
	}

	var m TeamMember
	err = system.DB.QueryRow(ctx, `
		INSERT INTO tenant_account (email, email_hash, password_hash, name, tenant_id, role)
		VALUES ($1, $2, $3, $4, $5, 'staff')
		RETURNING id, email, name, role, created_at, last_login_at`,
		email, emailHash, string(hash), namePtr, u.TenantID,
	).Scan(&m.ID, &m.Email, &namePtr, &m.Role, &m.CreatedAt, &m.LastLogin)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return nil, errs.BadRequest("email already registered")
		}
		return nil, errs.Internal("invite staff failed")
	}
	m.Name = namePtr

	audit.Log(ctx, u.TenantID, u.AccountID, "team.invite", "tenant_account", m.ID, map[string]string{"email": email})

	return &InviteStaffResponse{Member: m}, nil
}

//encore:api auth method=DELETE path=/api/v1/team/members/:id tag:owner
func RemoveStaff(ctx context.Context, id string) error {
	u, err := requireOwner(ctx)
	if err != nil {
		return err
	}
	if id == u.AccountID {
		return errs.BadRequest("cannot remove yourself")
	}
	res, err := system.DB.Exec(ctx, `
		UPDATE tenant_account SET deleted_at = NOW(), deleted_by = $1, updated_at = NOW()
		WHERE id = $2 AND tenant_id = $3 AND role = 'staff' AND deleted_at IS NULL`,
		u.AccountID, id, u.TenantID)
	if err != nil {
		return errs.Internal("remove staff failed")
	}
	if res.RowsAffected() == 0 {
		return errs.NotFound("staff member not found")
	}
	audit.Log(ctx, u.TenantID, u.AccountID, "team.remove", "tenant_account", id, nil)
	return nil
}

func requireOwner(ctx context.Context) (*types.AuthUser, error) {
	userData, ok := auth.Data().(*types.AuthUser)
	if !ok || userData == nil {
		return nil, errs.Unauthenticated("not authenticated")
	}
	if userData.Role != "owner" {
		return nil, errs.Forbidden("owner access required")
	}
	return userData, nil
}
