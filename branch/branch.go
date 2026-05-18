package branch

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/entitlement"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

type Branch struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	IsDefault bool      `json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
}

type ListBranchesResponse struct {
	Branches []Branch `json:"branches"`
}

type CreateBranchRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type CreateBranchResponse struct {
	Branch Branch `json:"branch"`
}

//encore:api auth method=GET path=/api/v1/branches
func ListBranches(ctx context.Context) (*ListBranchesResponse, error) {
	u, err := user(ctx)
	if err != nil {
		return nil, err
	}
	if err := entitlement.Require(ctx, u.TenantSchema, entitlement.FeatureMultiBranch); err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal("database connection failed")
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT id, name, slug, is_default, created_at
		FROM branch WHERE deleted_at IS NULL ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, appErrs.Internal("list branches failed")
	}
	defer rows.Close()

	out := make([]Branch, 0)
	for rows.Next() {
		var b Branch
		if err := rows.Scan(&b.ID, &b.Name, &b.Slug, &b.IsDefault, &b.CreatedAt); err != nil {
			return nil, appErrs.Internal("scan branch failed")
		}
		out = append(out, b)
	}
	return &ListBranchesResponse{Branches: out}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/branches tag:owner
func CreateBranch(ctx context.Context, req *CreateBranchRequest) (*CreateBranchResponse, error) {
	u, err := user(ctx)
	if err != nil {
		return nil, err
	}
	if err := entitlement.Require(ctx, u.TenantSchema, entitlement.FeatureMultiBranch); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if name == "" || slug == "" || !slugRe.MatchString(slug) {
		return nil, appErrs.BadRequest("name and slug required (slug: lowercase alphanumeric)")
	}

	conn, err := tConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal("database connection failed")
	}
	defer conn.Close()

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM branch WHERE deleted_at IS NULL`).Scan(&count); err != nil {
		return nil, appErrs.Internal("branch count failed")
	}
	isDefault := count == 0

	var b Branch
	err = conn.QueryRowContext(ctx, `
		INSERT INTO branch (name, slug, is_default)
		VALUES ($1, $2, $3)
		RETURNING id, name, slug, is_default, created_at`,
		name, slug, isDefault,
	).Scan(&b.ID, &b.Name, &b.Slug, &b.IsDefault, &b.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, appErrs.BadRequest("branch slug already exists")
		}
		return nil, appErrs.Internal("create branch failed")
	}
	return &CreateBranchResponse{Branch: b}, nil
}

func user(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	return u, nil
}

func tConn(ctx context.Context, schema string) (*sql.Conn, error) {
	return appdb.TenantConn(ctx, db.Stdlib(), schema)
}

func DefaultBranchID(ctx context.Context, schema string) (string, error) {
	conn, err := tConn(ctx, schema)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var id string
	err = conn.QueryRowContext(ctx, `
		SELECT id FROM branch WHERE deleted_at IS NULL AND is_default = true LIMIT 1`,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// EnsureDefaultBranch creates a default branch for tenants without multi-branch (single branch).
func EnsureDefaultBranch(ctx context.Context, schema string) error {
	conn, err := tConn(ctx, schema)
	if err != nil {
		return err
	}
	defer conn.Close()
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM branch WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = conn.ExecContext(ctx, `
		INSERT INTO branch (name, slug, is_default) VALUES ('Cabang Utama', 'main', true)`)
	return err
}
