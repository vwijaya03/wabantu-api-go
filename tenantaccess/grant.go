package tenantaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("system")

const (
	ScopeFull    = "full"
	ScopeLimited = "limited"

	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusRevoked  = "revoked"
	StatusExpired  = "expired"
)

// Duration presets (hours).
const (
	Duration24h       = 24
	Duration7d        = 168
	Duration30d       = 720
	DurationPermanent = 0
)

// GrantInfo is an active approved grant for impersonation.
type GrantInfo struct {
	RequestID  string
	Scope      string
	Modules    []string
	ExpiresAt  *time.Time
}

// AccessRequest is a tenant access consent row.
type AccessRequest struct {
	ID                   string     `json:"id"`
	RequesterAccountID   string     `json:"requesterAccountId"`
	TenantID             string     `json:"tenantId"`
	Reason               string     `json:"reason"`
	RequestedScope       string     `json:"requestedScope"`
	RequestedModules     []string   `json:"requestedModules"`
	Status               string     `json:"status"`
	GrantedScope         *string    `json:"grantedScope,omitempty"`
	GrantedModules       []string   `json:"grantedModules"`
	DurationHours        *int       `json:"durationHours,omitempty"`
	ExpiresAt            *time.Time `json:"expiresAt,omitempty"`
	RespondedBy          *string    `json:"respondedBy,omitempty"`
	RespondedAt          *time.Time `json:"respondedAt,omitempty"`
	RejectReason         *string    `json:"rejectReason,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	RequesterName        string     `json:"requesterName,omitempty"`
	RequesterEmail       string     `json:"requesterEmail,omitempty"`
	TenantName           string     `json:"tenantName,omitempty"`
}

func formatTextArray(vals []string) string {
	if len(vals) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		escaped := strings.ReplaceAll(v, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		parts = append(parts, `"`+escaped+`"`)
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func parseTextArray(raw sql.NullString) []string {
	if !raw.Valid {
		return []string{}
	}
	s := strings.TrimSpace(raw.String)
	if s == "" || s == "{}" {
		return []string{}
	}
	if strings.HasPrefix(s, "[") {
		var tags []string
		if err := json.Unmarshal([]byte(s), &tags); err == nil && tags != nil {
			return tags
		}
	}
	inner := strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{")
	if inner == "" {
		return []string{}
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(p, `"`))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeScope(scope string) string {
	s := strings.ToLower(strings.TrimSpace(scope))
	if s == ScopeLimited {
		return ScopeLimited
	}
	return ScopeFull
}

func normalizeModules(scope string, modules []string) ([]string, error) {
	if scope != ScopeLimited {
		return []string{}, nil
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("modul wajib dipilih untuk akses terbatas")
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(modules))
	for _, m := range modules {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !types.ValidTenantModule(m) {
			return nil, fmt.Errorf("modul tidak dikenal: %s", m)
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("modul wajib dipilih untuk akses terbatas")
	}
	return out, nil
}

func grantModulesFromScope(scope string, modules []string) []string {
	if scope != ScopeLimited {
		return []string{}
	}
	return modules
}

func isGrantActive(expiresAt *time.Time, now time.Time) bool {
	if expiresAt == nil {
		return true
	}
	return expiresAt.After(now)
}

// ActiveGrant returns the latest approved, non-expired grant for requester+tenant.
func ActiveGrant(ctx context.Context, requesterAccountID, tenantID string) (*GrantInfo, error) {
	now := time.Now()
	var reqID, scope string
	var modulesRaw sql.NullString
	var expiresAt sql.NullTime
	err := db.QueryRow(ctx, `
		SELECT id, COALESCE(granted_scope, 'full'), granted_modules, expires_at
		FROM tenant_access_request
		WHERE requester_account_id = $1
		  AND tenant_id = $2
		  AND status = 'approved'
		ORDER BY responded_at DESC NULLS LAST, created_at DESC
		LIMIT 1`,
		requesterAccountID, tenantID,
	).Scan(&reqID, &scope, &modulesRaw, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var exp *time.Time
	if expiresAt.Valid {
		t := expiresAt.Time
		if !isGrantActive(&t, now) {
			_, _ = db.Exec(ctx, `
				UPDATE tenant_access_request
				SET status = 'expired', updated_at = NOW()
				WHERE id = $1 AND status = 'approved'`, reqID)
			return nil, nil
		}
		exp = &t
	}
	modules := parseTextArray(modulesRaw)
	if scope == ScopeFull {
		modules = []string{}
	}
	return &GrantInfo{
		RequestID: reqID,
		Scope:     scope,
		Modules:   modules,
		ExpiresAt: exp,
	}, nil
}

// HasPendingRequest checks for an existing pending request.
func HasPendingRequest(ctx context.Context, requesterAccountID, tenantID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tenant_access_request
			WHERE requester_account_id = $1 AND tenant_id = $2 AND status = 'pending'
		)`, requesterAccountID, tenantID,
	).Scan(&exists)
	return exists, err
}

func durationFromPreset(preset string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "24h", "24":
		return Duration24h, nil
	case "7d", "168":
		return Duration7d, nil
	case "30d", "720":
		return Duration30d, nil
	case "permanent", "permanen", "":
		return DurationPermanent, nil
	default:
		return 0, fmt.Errorf("durasi tidak valid")
	}
}

func expiresAtFromDuration(hours int, now time.Time) *time.Time {
	if hours <= 0 {
		return nil
	}
	t := now.Add(time.Duration(hours) * time.Hour)
	return &t
}

// ListOwnerAccountIDs returns active owner account IDs for a tenant.
func ListOwnerAccountIDs(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT id FROM tenant_account
		WHERE tenant_id = $1 AND role = 'owner' AND deleted_at IS NULL`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanAccessRequest(
	id, requesterID, tenantID, reason, reqScope, status string,
	reqModulesRaw, grantedModulesRaw sql.NullString,
	grantedScope sql.NullString,
	durationHours sql.NullInt32,
	expiresAt, respondedAt sql.NullTime,
	rejectReason, respondedBy sql.NullString,
	createdAt, updatedAt time.Time,
) AccessRequest {
	r := AccessRequest{
		ID:                 id,
		RequesterAccountID: requesterID,
		TenantID:           tenantID,
		Reason:             reason,
		RequestedScope:     reqScope,
		RequestedModules:   parseTextArray(reqModulesRaw),
		Status:             status,
		GrantedModules:     parseTextArray(grantedModulesRaw),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}
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
	return r
}
