package tenantaccess

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/errs"

	"encore.app/wabantu/audit"
	"encore.app/wabantu/notification"
)

type CreateRequestParams struct {
	TenantID         string   `json:"tenantId"`
	Reason           string   `json:"reason"`
	RequestedScope   string   `json:"requestedScope"`
	RequestedModules []string `json:"requestedModules,omitempty"`
}

type ListByRequesterParams struct {
	TenantID string `query:"tenantId"`
}

type ListByRequesterResponse struct {
	Requests []AccessRequest `json:"requests"`
}

type ListForTenantResponse struct {
	Requests []AccessRequest `json:"requests"`
}

type RespondParams struct {
	Action         string   `json:"action"` // approve | reject
	GrantedScope   string   `json:"grantedScope,omitempty"`
	GrantedModules []string `json:"grantedModules,omitempty"`
	DurationPreset string   `json:"durationPreset,omitempty"` // 24h | 7d | 30d | permanent
	RejectReason   string   `json:"rejectReason,omitempty"`
}

type RespondResponse struct {
	Request AccessRequest `json:"request"`
}

type RevokeResponse struct {
	Request AccessRequest `json:"request"`
}

// CreateRequest inserts a new access request (super_admin requester).
func CreateRequest(ctx context.Context, requesterAccountID string, p *CreateRequestParams) (*AccessRequest, error) {
	if p == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "parameter tidak valid"}
	}
	tenantID := strings.TrimSpace(p.TenantID)
	reason := strings.TrimSpace(p.Reason)
	if tenantID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "tenant wajib dipilih"}
	}
	if reason == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "alasan wajib diisi"}
	}
	scope := normalizeScope(p.RequestedScope)
	modules, err := normalizeModules(scope, p.RequestedModules)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}

	var tenantName string
	err = db.QueryRow(ctx, `
		SELECT name FROM tenant WHERE id = $1 AND deleted_at IS NULL`, tenantID,
	).Scan(&tenantName)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "tenant tidak ditemukan"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "lookup tenant gagal"}
	}

	ownerIDs, err := ListOwnerAccountIDs(ctx, tenantID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "lookup owner gagal"}
	}
	if len(ownerIDs) == 0 {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "tenant tidak memiliki owner aktif — persetujuan tidak dapat diajukan",
		}
	}

	pending, err := HasPendingRequest(ctx, requesterAccountID, tenantID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "cek request gagal"}
	}
	if pending {
		return nil, &errs.Error{Code: errs.AlreadyExists, Message: "sudah ada permintaan pending untuk tenant ini"}
	}

	var id string
	var createdAt, updatedAt time.Time
	err = db.QueryRow(ctx, `
		INSERT INTO tenant_access_request (
			requester_account_id, tenant_id, reason,
			requested_scope, requested_modules, status
		) VALUES ($1, $2, $3, $4, $5::text[], 'pending')
		RETURNING id, created_at, updated_at`,
		requesterAccountID, tenantID, reason, scope, formatTextArray(modules),
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_tar_pending_requester_tenant") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "sudah ada permintaan pending untuk tenant ini"}
		}
		return nil, &errs.Error{Code: errs.Internal, Message: "buat permintaan gagal"}
	}

	req := AccessRequest{
		ID:                 id,
		RequesterAccountID: requesterAccountID,
		TenantID:           tenantID,
		Reason:             reason,
		RequestedScope:     scope,
		RequestedModules:   modules,
		Status:             StatusPending,
		TenantName:         tenantName,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}

	audit.Log(ctx, tenantID, requesterAccountID, "tenant_access.request", "tenant_access_request", id, map[string]string{
		"scope": scope,
	})

	title := "Permintaan akses tenant"
	body := fmt.Sprintf("Staff platform meminta akses ke tenant %s", tenantName)
	link := "/dashboard/access-requests"
	_ = notification.CreateForAccounts(ctx, ownerIDs, "tenant_access.request", title, body, link)

	return &req, nil
}

// ListByRequester returns requests created by the requester.
func ListByRequester(ctx context.Context, requesterAccountID string, tenantID string) ([]AccessRequest, error) {
	args := []any{requesterAccountID}
	where := "WHERE r.requester_account_id = $1"
	if tid := strings.TrimSpace(tenantID); tid != "" {
		where += " AND r.tenant_id = $2"
		args = append(args, tid)
	}
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT r.id, r.requester_account_id, r.tenant_id, r.reason,
			r.requested_scope, r.requested_modules, r.status,
			r.granted_scope, r.granted_modules, r.duration_hours, r.expires_at,
			r.responded_by, r.responded_at, r.reject_reason,
			r.created_at, r.updated_at,
			COALESCE(t.name, ''), COALESCE(req.name, ''), COALESCE(req.email, '')
		FROM tenant_access_request r
		JOIN tenant t ON t.id = r.tenant_id
		JOIN tenant_account req ON req.id = r.requester_account_id
		%s
		ORDER BY r.created_at DESC
		LIMIT 100`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AccessRequest
	for rows.Next() {
		var r AccessRequest
		var reqModules, grantedModules sql.NullString
		var grantedScope sql.NullString
		var durationHours sql.NullInt32
		var expiresAt, respondedAt sql.NullTime
		var rejectReason, respondedBy sql.NullString
		if err := rows.Scan(
			&r.ID, &r.RequesterAccountID, &r.TenantID, &r.Reason,
			&r.RequestedScope, &reqModules, &r.Status,
			&grantedScope, &grantedModules, &durationHours, &expiresAt,
			&respondedBy, &respondedAt, &rejectReason,
			&r.CreatedAt, &r.UpdatedAt,
			&r.TenantName, &r.RequesterName, &r.RequesterEmail,
		); err != nil {
			return nil, err
		}
		r.RequestedModules = parseTextArray(reqModules)
		r.GrantedModules = parseTextArray(grantedModules)
		if grantedScope.Valid {
			s := grantedScope.String
			r.GrantedScope = &s
		}
		if durationHours.Valid {
			h := int(durationHours.Int32)
			r.DurationHours = &h
		}
		if expiresAt.Valid {
			t := expiresAt.Time
			r.ExpiresAt = &t
		}
		if respondedAt.Valid {
			t := respondedAt.Time
			r.RespondedAt = &t
		}
		if rejectReason.Valid {
			s := rejectReason.String
			r.RejectReason = &s
		}
		if respondedBy.Valid {
			s := respondedBy.String
			r.RespondedBy = &s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListForTenant returns all requests for the owner's tenant.
func ListForTenant(ctx context.Context, tenantID string) ([]AccessRequest, error) {
	rows, err := db.Query(ctx, `
		SELECT r.id, r.requester_account_id, r.tenant_id, r.reason,
			r.requested_scope, r.requested_modules, r.status,
			r.granted_scope, r.granted_modules, r.duration_hours, r.expires_at,
			r.responded_by, r.responded_at, r.reject_reason,
			r.created_at, r.updated_at,
			COALESCE(t.name, ''), COALESCE(req.name, ''), COALESCE(req.email, '')
		FROM tenant_access_request r
		JOIN tenant t ON t.id = r.tenant_id
		JOIN tenant_account req ON req.id = r.requester_account_id
		WHERE r.tenant_id = $1
		ORDER BY
			CASE r.status WHEN 'pending' THEN 0 ELSE 1 END,
			r.created_at DESC
		LIMIT 200`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AccessRequest
	for rows.Next() {
		var r AccessRequest
		var reqModules, grantedModules sql.NullString
		var grantedScope sql.NullString
		var durationHours sql.NullInt32
		var expiresAt, respondedAt sql.NullTime
		var rejectReason, respondedBy sql.NullString
		if err := rows.Scan(
			&r.ID, &r.RequesterAccountID, &r.TenantID, &r.Reason,
			&r.RequestedScope, &reqModules, &r.Status,
			&grantedScope, &grantedModules, &durationHours, &expiresAt,
			&respondedBy, &respondedAt, &rejectReason,
			&r.CreatedAt, &r.UpdatedAt,
			&r.TenantName, &r.RequesterName, &r.RequesterEmail,
		); err != nil {
			return nil, err
		}
		r.RequestedModules = parseTextArray(reqModules)
		r.GrantedModules = parseTextArray(grantedModules)
		if grantedScope.Valid {
			s := grantedScope.String
			r.GrantedScope = &s
		}
		if durationHours.Valid {
			h := int(durationHours.Int32)
			r.DurationHours = &h
		}
		if expiresAt.Valid {
			t := expiresAt.Time
			r.ExpiresAt = &t
		}
		if respondedAt.Valid {
			t := respondedAt.Time
			r.RespondedAt = &t
		}
		if rejectReason.Valid {
			s := rejectReason.String
			r.RejectReason = &s
		}
		if respondedBy.Valid {
			s := respondedBy.String
			r.RespondedBy = &s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Respond approves or rejects a pending request (tenant owner).
func Respond(ctx context.Context, requestID, ownerAccountID, tenantID string, p *RespondParams) (*AccessRequest, error) {
	if p == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "parameter tidak valid"}
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	if action != "approve" && action != "reject" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "action harus approve atau reject"}
	}

	var cur AccessRequest
	var reqModules, grantedModules sql.NullString
	var grantedScope sql.NullString
	var durationHours sql.NullInt32
	var expiresAt, respondedAt sql.NullTime
	var rejectReason, respondedBy sql.NullString
	err := db.QueryRow(ctx, `
		SELECT r.id, r.requester_account_id, r.tenant_id, r.reason,
			r.requested_scope, r.requested_modules, r.status,
			r.granted_scope, r.granted_modules, r.duration_hours, r.expires_at,
			r.responded_by, r.responded_at, r.reject_reason,
			r.created_at, r.updated_at,
			COALESCE(t.name, ''), COALESCE(req.name, ''), COALESCE(req.email, '')
		FROM tenant_access_request r
		JOIN tenant t ON t.id = r.tenant_id
		JOIN tenant_account req ON req.id = r.requester_account_id
		WHERE r.id = $1 AND r.tenant_id = $2`,
		requestID, tenantID,
	).Scan(
		&cur.ID, &cur.RequesterAccountID, &cur.TenantID, &cur.Reason,
		&cur.RequestedScope, &reqModules, &cur.Status,
		&grantedScope, &grantedModules, &durationHours, &expiresAt,
		&respondedBy, &respondedAt, &rejectReason,
		&cur.CreatedAt, &cur.UpdatedAt,
		&cur.TenantName, &cur.RequesterName, &cur.RequesterEmail,
	)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "permintaan tidak ditemukan"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "lookup permintaan gagal"}
	}
	if cur.Status != StatusPending {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "permintaan sudah diproses"}
	}

	now := time.Now()
	if action == "reject" {
		reason := strings.TrimSpace(p.RejectReason)
		if reason == "" {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "alasan penolakan wajib diisi"}
		}
		_, err = db.Exec(ctx, `
			UPDATE tenant_access_request
			SET status = 'rejected', reject_reason = $1,
				responded_by = $2, responded_at = $3, updated_at = $3
			WHERE id = $4 AND status = 'pending'`,
			reason, ownerAccountID, now, requestID)
		if err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "tolak permintaan gagal"}
		}
		cur.Status = StatusRejected
		cur.RejectReason = &reason
		cur.RespondedBy = &ownerAccountID
		cur.RespondedAt = &now

		audit.Log(ctx, tenantID, ownerAccountID, "tenant_access.reject", "tenant_access_request", requestID, map[string]string{
			"reason": reason,
		})
		title := "Permintaan akses ditolak"
		body := fmt.Sprintf("Owner menolak akses ke %s: %s", cur.TenantName, reason)
		_ = notification.CreateForAccounts(ctx, []string{cur.RequesterAccountID}, "tenant_access.rejected", title, body, "/dashboard/admin")
		return &cur, nil
	}

	scope := normalizeScope(p.GrantedScope)
	if scope == ScopeFull && strings.TrimSpace(p.GrantedScope) == "" {
		scope = normalizeScope(cur.RequestedScope)
	}
	modules, err := normalizeModules(scope, p.GrantedModules)
	if err != nil {
		// fallback to requested modules when approving limited
		if scope == ScopeLimited && len(p.GrantedModules) == 0 {
			modules, err = normalizeModules(scope, cur.RequestedModules)
		}
		if err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
		}
	}
	hours, err := durationFromPreset(p.DurationPreset)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	exp := expiresAtFromDuration(hours, now)
	var durationVal *int
	if hours > 0 {
		durationVal = &hours
	}
	grantedModList := grantModulesFromScope(scope, modules)

	var expArg any
	if exp != nil {
		expArg = *exp
	}
	_, err = db.Exec(ctx, `
		UPDATE tenant_access_request
		SET status = 'approved',
			granted_scope = $1,
			granted_modules = $2::text[],
			duration_hours = $3,
			expires_at = $4,
			responded_by = $5,
			responded_at = $6,
			updated_at = $6
		WHERE id = $7 AND status = 'pending'`,
		scope, formatTextArray(grantedModList), durationVal, expArg, ownerAccountID, now, requestID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "setujui permintaan gagal"}
	}

	cur.Status = StatusApproved
	gs := scope
	cur.GrantedScope = &gs
	cur.GrantedModules = grantedModList
	cur.DurationHours = durationVal
	cur.ExpiresAt = exp
	cur.RespondedBy = &ownerAccountID
	cur.RespondedAt = &now

	audit.Log(ctx, tenantID, ownerAccountID, "tenant_access.approve", "tenant_access_request", requestID, map[string]interface{}{
		"scope": scope, "modules": grantedModules, "durationHours": hours,
	})

	title := "Permintaan akses disetujui"
	body := fmt.Sprintf("Owner menyetujui akses ke %s", cur.TenantName)
	_ = notification.CreateForAccounts(ctx, []string{cur.RequesterAccountID}, "tenant_access.approved", title, body, "/dashboard/admin")

	return &cur, nil
}

// Revoke revokes an active approved grant.
func Revoke(ctx context.Context, requestID, ownerAccountID, tenantID string) (*AccessRequest, error) {
	var cur AccessRequest
	var reqModules, grantedModules sql.NullString
	var grantedScope sql.NullString
	var durationHours sql.NullInt32
	var expiresAt, respondedAt sql.NullTime
	var rejectReason, respondedBy sql.NullString
	err := db.QueryRow(ctx, `
		SELECT r.id, r.requester_account_id, r.tenant_id, r.reason,
			r.requested_scope, r.requested_modules, r.status,
			r.granted_scope, r.granted_modules, r.duration_hours, r.expires_at,
			r.responded_by, r.responded_at, r.reject_reason,
			r.created_at, r.updated_at,
			COALESCE(t.name, ''), COALESCE(req.name, ''), COALESCE(req.email, '')
		FROM tenant_access_request r
		JOIN tenant t ON t.id = r.tenant_id
		JOIN tenant_account req ON req.id = r.requester_account_id
		WHERE r.id = $1 AND r.tenant_id = $2`,
		requestID, tenantID,
	).Scan(
		&cur.ID, &cur.RequesterAccountID, &cur.TenantID, &cur.Reason,
		&cur.RequestedScope, &reqModules, &cur.Status,
		&grantedScope, &grantedModules, &durationHours, &expiresAt,
		&respondedBy, &respondedAt, &rejectReason,
		&cur.CreatedAt, &cur.UpdatedAt,
		&cur.TenantName, &cur.RequesterName, &cur.RequesterEmail,
	)
	if err == sql.ErrNoRows {
		return nil, &errs.Error{Code: errs.NotFound, Message: "permintaan tidak ditemukan"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "lookup permintaan gagal"}
	}
	if cur.Status != StatusApproved {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "hanya grant aktif yang dapat dicabut"}
	}
	cur.RequestedModules = parseTextArray(reqModules)
	cur.GrantedModules = parseTextArray(grantedModules)
	if grantedScope.Valid {
		s := grantedScope.String
		cur.GrantedScope = &s
	}
	if durationHours.Valid {
		h := int(durationHours.Int32)
		cur.DurationHours = &h
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		cur.ExpiresAt = &t
	}
	now := time.Now()
	if cur.ExpiresAt != nil && !cur.ExpiresAt.After(now) {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "grant sudah kedaluwarsa"}
	}

	_, err = db.Exec(ctx, `
		UPDATE tenant_access_request
		SET status = 'revoked', updated_at = $1
		WHERE id = $2 AND status = 'approved'`,
		now, requestID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "cabut grant gagal"}
	}

	cur.Status = StatusRevoked
	cur.UpdatedAt = now

	audit.Log(ctx, tenantID, ownerAccountID, "tenant_access.revoke", "tenant_access_request", requestID, nil)

	title := "Akses tenant dicabut"
	body := fmt.Sprintf("Owner mencabut akses ke %s", cur.TenantName)
	_ = notification.CreateForAccounts(ctx, []string{cur.RequesterAccountID}, "tenant_access.revoked", title, body, "/dashboard/admin")

	return &cur, nil
}
