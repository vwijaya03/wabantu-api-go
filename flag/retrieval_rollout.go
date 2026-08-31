package flag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"encore.dev/rlog"

	"encore.app/wabantu/kb"
	"encore.app/wabantu/shared/retrieval"
)

// EnableVectorForNewTenant enables RAG vector mode for a newly registered tenant.
func EnableVectorForNewTenant(ctx context.Context, tenantID string) error {
	_, err := SetTenantRetrievalMode(ctx, tenantID, string(retrieval.ModeVector), false)
	return err
}

// TenantRetrievalMode returns the effective retrieval mode for a tenant.
func TenantRetrievalMode(ctx context.Context, tenantID string) retrieval.RetrievalMode {
	return RetrievalMode(ctx, tenantID)
}

func loadFlag(ctx context.Context, key string) (FeatureFlag, bool, error) {
	row := db.QueryRow(ctx,
		`SELECT key, enabled_globally, tenant_ids, COALESCE(description,''), created_at, updated_at
		 FROM feature_flag WHERE key=$1`, key)
	f, err := scanFlag(row.Scan)
	if err == sql.ErrNoRows {
		return FeatureFlag{}, false, nil
	}
	if err != nil {
		return FeatureFlag{}, false, err
	}
	return f, true, nil
}

func tenantSchemaForID(ctx context.Context, tenantID string) (string, error) {
	var schema string
	err := db.QueryRow(ctx,
		`SELECT schema_name FROM tenant_company WHERE tenant_id = $1::uuid LIMIT 1`,
		tenantID,
	).Scan(&schema)
	return schema, err
}

func removeTenantFromFlag(ctx context.Context, key, tenantID string) error {
	f, ok, err := loadFlag(ctx, key)
	if err != nil || !ok {
		return err
	}
	next := removeID(f.TenantIDs, tenantID)
	if len(next) == len(f.TenantIDs) {
		return nil
	}
	idsJSON, _ := json.Marshal(next)
	_, err = db.Exec(ctx,
		`UPDATE feature_flag SET tenant_ids=$1, updated_at=NOW() WHERE key=$2`,
		idsJSON, key)
	invalidateCache(key)
	return err
}

func addTenantToFlag(ctx context.Context, key, description, tenantID string) error {
	f, ok, err := loadFlag(ctx, key)
	if err != nil {
		return err
	}
	if !ok {
		idsJSON, _ := json.Marshal([]string{tenantID})
		row := db.QueryRow(ctx,
			`INSERT INTO feature_flag (key, enabled_globally, tenant_ids, description)
			 VALUES ($1, false, $2, $3)
			 RETURNING key, enabled_globally, tenant_ids, COALESCE(description,''), created_at, updated_at`,
			key, idsJSON, description)
		if _, err := scanFlag(row.Scan); err != nil {
			return err
		}
		invalidateCache(key)
		return nil
	}
	if containsID(f.TenantIDs, tenantID) {
		return nil
	}
	next := append(append([]string{}, f.TenantIDs...), tenantID)
	idsJSON, _ := json.Marshal(next)
	_, err = db.Exec(ctx,
		`UPDATE feature_flag SET tenant_ids=$1, updated_at=NOW() WHERE key=$2`,
		idsJSON, key)
	invalidateCache(key)
	return err
}

// SetTenantRetrievalMode sets disabled|shadow|vector for one tenant and optionally backfills.
func SetTenantRetrievalMode(ctx context.Context, tenantID, mode string, backfill bool) (*SetRetrievalModeResponse, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantId required")
	}
	prev := RetrievalMode(ctx, tenantID)

	_ = removeTenantFromFlag(ctx, FlagRetrievalVector, tenantID)
	_ = removeTenantFromFlag(ctx, FlagRetrievalShadow, tenantID)

	switch mode {
	case string(retrieval.ModeVector):
		if err := addTenantToFlag(ctx, FlagRetrievalVector, "RAG vector retrieval per tenant", tenantID); err != nil {
			return nil, err
		}
	case string(retrieval.ModeShadow):
		if err := addTenantToFlag(ctx, FlagRetrievalShadow, "RAG shadow retrieval per tenant", tenantID); err != nil {
			return nil, err
		}
	case string(retrieval.ModeDisabled), "":
		mode = string(retrieval.ModeDisabled)
	default:
		return nil, fmt.Errorf("invalid mode: %s", mode)
	}

	resp := &SetRetrievalModeResponse{
		TenantID: tenantID,
		Mode:     mode,
		Previous: string(prev),
	}

	shouldBackfill := backfill && (mode == string(retrieval.ModeShadow) || mode == string(retrieval.ModeVector))
	if shouldBackfill {
		schema, err := tenantSchemaForID(ctx, tenantID)
		if err != nil {
			return resp, err
		}
		kbN, catN, err := kb.EnqueueRAGBackfillForTenant(ctx, schema, tenantID, 500)
		if err != nil {
			rlog.Warn("rag backfill on flag enable failed", "tenant", tenantID, "err", err)
			return resp, err
		}
		resp.KBEnqueued = kbN
		resp.CatalogEnqueued = catN
		rlog.Info("rag backfill enqueued", "tenant", tenantID, "kb", kbN, "catalog", catN, "mode", mode)
	}
	return resp, nil
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func removeID(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// triggerBackfillForNewlyEnabled compares tenant lists and backfills tenants newly added to retrieval flags.
func triggerBackfillForNewlyEnabled(ctx context.Context, key string, before, after FeatureFlag) {
	if key != FlagRetrievalVector && key != FlagRetrievalShadow {
		return
	}
	if after.EnabledGlobally && !before.EnabledGlobally {
		// global enable — skip mass backfill here (ops can run reindex per tenant)
		return
	}
	added := newlyAddedTenantIDs(before.TenantIDs, after.TenantIDs)
	for _, tenantID := range added {
		schema, err := tenantSchemaForID(ctx, tenantID)
		if err != nil {
			rlog.Warn("rag backfill schema lookup failed", "tenant", tenantID, "err", err)
			continue
		}
		kbN, catN, err := kb.EnqueueRAGBackfillForTenant(ctx, schema, tenantID, 500)
		if err != nil {
			rlog.Warn("auto backfill after flag update failed", "tenant", tenantID, "err", err)
			continue
		}
		rlog.Info("rag backfill enqueued from flag", "tenant", tenantID, "kb", kbN, "catalog", catN, "flag", key)
	}
}

func newlyAddedTenantIDs(before, after []string) []string {
	var out []string
	for _, id := range after {
		if !containsID(before, id) {
			out = append(out, id)
		}
	}
	return out
}

func retrievalModeForKey(key string) string {
	if key == FlagRetrievalVector {
		return string(retrieval.ModeVector)
	}
	return string(retrieval.ModeShadow)
}

type SetRetrievalModeRequest struct {
	TenantID string `json:"tenantId"`
	Mode     string `json:"mode"` // disabled | shadow | vector
}

type SetRetrievalModeResponse struct {
	TenantID        string `json:"tenantId"`
	Mode            string `json:"mode"`
	Previous        string `json:"previous"`
	KBEnqueued      int    `json:"kbEnqueued"`
	CatalogEnqueued int    `json:"catalogEnqueued"`
}

type GetRetrievalModeResponse struct {
	TenantID string `json:"tenantId"`
	Mode     string `json:"mode"`
}

//encore:api auth method=GET path=/api/v1/flags/retrieval-mode/:tenantId
func GetRetrievalMode(ctx context.Context, tenantId string) (*GetRetrievalModeResponse, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	return &GetRetrievalModeResponse{
		TenantID: tenantId,
		Mode:     string(TenantRetrievalMode(ctx, tenantId)),
	}, nil
}

//encore:api auth method=PUT path=/api/v1/flags/retrieval-mode
func SetRetrievalMode(ctx context.Context, req *SetRetrievalModeRequest) (*SetRetrievalModeResponse, error) {
	if _, err := requireSuperAdmin(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	return SetTenantRetrievalMode(ctx, req.TenantID, req.Mode, true)
}
