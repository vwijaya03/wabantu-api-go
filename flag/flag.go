package flag

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("system")

// ---------- types ----------

type FeatureFlag struct {
	Key             string   `json:"key"`
	EnabledGlobally bool     `json:"enabledGlobally"`
	TenantIDs       []string `json:"tenantIds"`
	Description     string   `json:"description"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type CreateFlagParams struct {
	Key             string   `json:"key"`
	EnabledGlobally bool     `json:"enabledGlobally"`
	TenantIDs       []string `json:"tenantIds"`
	Description     string   `json:"description"`
}

type UpdateFlagParams struct {
	EnabledGlobally *bool    `json:"enabledGlobally,omitempty"`
	TenantIDs       []string `json:"tenantIds,omitempty"`
	Description     *string  `json:"description,omitempty"`
}

type ListFlagsResponse struct {
	Flags []FeatureFlag `json:"flags"`
}

type CheckFlagResponse struct {
	Enabled bool `json:"enabled"`
}

// ---------- in-memory cache (60s TTL) ----------

var flagCache sync.Map

type cachedFlag struct {
	flag      FeatureFlag
	expiresAt time.Time
}

func getCached(key string) (*FeatureFlag, bool) {
	v, ok := flagCache.Load(key)
	if !ok {
		return nil, false
	}
	e := v.(*cachedFlag)
	if time.Now().After(e.expiresAt) {
		flagCache.Delete(key)
		return nil, false
	}
	return &e.flag, true
}

func setCache(f FeatureFlag) {
	flagCache.Store(f.Key, &cachedFlag{flag: f, expiresAt: time.Now().Add(60 * time.Second)})
}

func invalidateCache(key string) { flagCache.Delete(key) }

// ---------- helpers ----------

func requireSuperAdmin() (*types.AuthUser, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || u.Role != "super_admin" {
		return nil, appErrs.Forbidden("super_admin access required")
	}
	return u, nil
}

func scanFlag(scan func(dest ...any) error) (FeatureFlag, error) {
	var f FeatureFlag
	var idsRaw []byte
	if err := scan(&f.Key, &f.EnabledGlobally, &idsRaw, &f.Description, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return f, err
	}
	if len(idsRaw) > 0 {
		_ = json.Unmarshal(idsRaw, &f.TenantIDs)
	}
	if f.TenantIDs == nil {
		f.TenantIDs = []string{}
	}
	return f, nil
}

// ---------- endpoints ----------

//encore:api auth method=GET path=/api/v1/flags
func ListFlags(ctx context.Context) (*ListFlagsResponse, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}

	rows, err := db.Query(ctx,
		`SELECT key, enabled_globally, tenant_ids, COALESCE(description,''), created_at, updated_at
		 FROM feature_flag ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flags := make([]FeatureFlag, 0)
	for rows.Next() {
		f, err := scanFlag(rows.Scan)
		if err != nil {
			return nil, err
		}
		flags = append(flags, f)
	}
	return &ListFlagsResponse{Flags: flags}, nil
}

//encore:api auth method=POST path=/api/v1/flags
func CreateFlag(ctx context.Context, p *CreateFlagParams) (*FeatureFlag, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	if p.Key == "" {
		return nil, appErrs.BadRequest("key is required")
	}
	if p.TenantIDs == nil {
		p.TenantIDs = []string{}
	}

	idsJSON, _ := json.Marshal(p.TenantIDs)
	row := db.QueryRow(ctx,
		`INSERT INTO feature_flag (key, enabled_globally, tenant_ids, description)
		 VALUES ($1,$2,$3,$4)
		 RETURNING key, enabled_globally, tenant_ids, COALESCE(description,''), created_at, updated_at`,
		p.Key, p.EnabledGlobally, idsJSON, p.Description)

	f, err := scanFlag(row.Scan)
	if err != nil {
		return nil, err
	}
	setCache(f)
	return &f, nil
}

//encore:api auth method=PATCH path=/api/v1/flags/:key
func UpdateFlag(ctx context.Context, key string, req *UpdateFlagParams) (*FeatureFlag, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}

	sets := []string{}
	args := []any{}
	idx := 1

	if req.EnabledGlobally != nil {
		sets = append(sets, fmt.Sprintf("enabled_globally=$%d", idx))
		args = append(args, *req.EnabledGlobally)
		idx++
	}
	if req.TenantIDs != nil {
		idsJSON, _ := json.Marshal(req.TenantIDs)
		sets = append(sets, fmt.Sprintf("tenant_ids=$%d", idx))
		args = append(args, idsJSON)
		idx++
	}
	if req.Description != nil {
		sets = append(sets, fmt.Sprintf("description=$%d", idx))
		args = append(args, *req.Description)
		idx++
	}
	if len(sets) == 0 {
		return nil, appErrs.BadRequest("no fields to update")
	}

	sets = append(sets, "updated_at=NOW()")
	args = append(args, key)

	q := fmt.Sprintf(
		`UPDATE feature_flag SET %s WHERE key=$%d
		 RETURNING key, enabled_globally, tenant_ids, COALESCE(description,''), created_at, updated_at`,
		joinStrings(sets, ", "), idx)

	f, err := scanFlag(db.QueryRow(ctx, q, args...).Scan)
	if err != nil {
		return nil, err
	}
	setCache(f)
	return &f, nil
}

//encore:api auth method=GET path=/api/v1/flags/check/:key
func CheckFlag(ctx context.Context, key string) (*CheckFlagResponse, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil {
		return nil, appErrs.Unauthenticated("missing auth data")
	}
	return &CheckFlagResponse{Enabled: IsEnabled(ctx, key, u.TenantID)}, nil
}

// IsEnabled checks whether a feature flag is active for the given tenant.
// Safe to call from any service — uses an in-memory cache with 60 s TTL.
func IsEnabled(ctx context.Context, key, tenantID string) bool {
	if f, ok := getCached(key); ok {
		return flagEnabled(f, tenantID)
	}

	row := db.QueryRow(ctx,
		`SELECT key, enabled_globally, tenant_ids, COALESCE(description,''), created_at, updated_at
		 FROM feature_flag WHERE key=$1`, key)

	f, err := scanFlag(row.Scan)
	if err != nil {
		return false
	}
	setCache(f)
	return flagEnabled(&f, tenantID)
}

func flagEnabled(f *FeatureFlag, tenantID string) bool {
	if f.EnabledGlobally {
		return true
	}
	for _, id := range f.TenantIDs {
		if id == tenantID {
			return true
		}
	}
	return false
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
