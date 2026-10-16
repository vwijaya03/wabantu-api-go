package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"github.com/google/uuid"

	"encore.app/wabantu/audit"
	"encore.app/wabantu/ai"
	"encore.app/wabantu/shared/llm"
	"encore.app/wabantu/shared/retrieval"
)

// ─── AI provider config (BE-06) — contract: docs/API_AI_CONFIG.md ───────────
// All endpoints: auth + tag:super_admin + requireSuperAdmin (role from the auth
// session, never from params). Changes apply within the resolver TTL (≤30s);
// the local process is invalidated immediately.

type AIChainLink struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type AIFunctionConfig struct {
	Function   string        `json:"function"`
	TenantID   *string       `json:"tenantId"`
	Chain      []AIChainLink `json:"chain"`
	MaxRetries int           `json:"maxRetries"`
	// Reindex is set when an embedding model change queued a reindex (PUT only).
	Reindex *AIReindexInfo `json:"reindex,omitempty"`
}

type ListAIFunctionConfigParams struct {
	TenantID string `query:"tenantId"`
}

type ListAIFunctionConfigResponse struct {
	Configs []AIFunctionConfig `json:"configs"`
}

// ListAIFunctionConfig returns global configs, plus overrides of ?tenantId= when given.
//
//encore:api auth method=GET path=/api/v1/admin/ai/function-config tag:super_admin
func ListAIFunctionConfig(ctx context.Context, p *ListAIFunctionConfigParams) (*ListAIFunctionConfigResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	tenantID, err := optionalTenant(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Stdlib().QueryContext(ctx, `
		SELECT function, tenant_id, chain, max_retries
		FROM ai_function_config
		WHERE tenant_id IS NULL OR ($1 <> '' AND tenant_id = $1)
		ORDER BY function, tenant_id NULLS FIRST`, tenantID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal memuat konfigurasi AI"}
	}
	defer rows.Close()
	out := &ListAIFunctionConfigResponse{Configs: []AIFunctionConfig{}}
	for rows.Next() {
		var (
			fn  string
			tid sql.NullString
			raw []byte
			mr  int
		)
		if err := rows.Scan(&fn, &tid, &raw, &mr); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "gagal membaca konfigurasi AI"}
		}
		var chain []AIChainLink
		if err := json.Unmarshal(raw, &chain); err != nil {
			rlog.Warn("ai config: bad chain json", "function", fn)
			continue
		}
		c := AIFunctionConfig{Function: fn, Chain: chain, MaxRetries: mr}
		if tid.Valid {
			v := tid.String
			c.TenantID = &v
		}
		out.Configs = append(out.Configs, c)
	}
	return out, rows.Err()
}

type PutAIFunctionConfigParams struct {
	TenantID   *string       `json:"tenantId"`
	Chain      []AIChainLink `json:"chain"`
	MaxRetries int           `json:"maxRetries"`
	// ConfirmReindex is required when the embedding model changes: vectors of the affected
	// namespaces are wiped and re-queued (the request returns after enqueue, not after embedding).
	ConfirmReindex bool `json:"confirmReindex,omitempty"`
}

// PutAIFunctionConfig upserts the global (tenantId=null) or tenant override chain.
//
//encore:api auth method=PUT path=/api/v1/admin/ai/function-config/:function tag:super_admin
func PutAIFunctionConfig(ctx context.Context, function string, p *PutAIFunctionConfigParams) (*AIFunctionConfig, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	fn := llm.Function(strings.TrimSpace(function))
	chain := toLLMChain(p.Chain)
	if err := llm.ValidateFunctionConfig(fn, chain, p.MaxRetries); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	tenantID := ""
	if p.TenantID != nil {
		if tenantID, err = requireTenant(ctx, *p.TenantID); err != nil {
			return nil, err
		}
	}
	var embedFrom, embedTo string
	if fn == llm.FnEmbedding {
		embedTo = retrieval.EmbeddingModelFromChain(chain)
		if err := requireEmbeddingCredentials(embedTo); err != nil {
			return nil, err
		}
		if embedFrom, err = effectiveEmbeddingModel(ctx, tenantID); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "gagal membaca konfigurasi embedding saat ini"}
		}
		if err := requireReindexConfirmation(embedFrom, embedTo, p.ConfirmReindex); err != nil {
			return nil, err
		}
	}
	raw, _ := json.Marshal(chain)
	if tenantID == "" {
		_, err = db.Stdlib().ExecContext(ctx, `
			INSERT INTO ai_function_config (function, tenant_id, chain, max_retries, updated_by, updated_at)
			VALUES ($1, NULL, $2::jsonb, $3, $4, now())
			ON CONFLICT (function) WHERE tenant_id IS NULL
			DO UPDATE SET chain = EXCLUDED.chain, max_retries = EXCLUDED.max_retries,
			              updated_by = EXCLUDED.updated_by, updated_at = now()`,
			string(fn), string(raw), p.MaxRetries, user.AccountID)
	} else {
		_, err = db.Stdlib().ExecContext(ctx, `
			INSERT INTO ai_function_config (function, tenant_id, chain, max_retries, updated_by, updated_at)
			VALUES ($1, $2, $3::jsonb, $4, $5, now())
			ON CONFLICT (function, tenant_id)
			DO UPDATE SET chain = EXCLUDED.chain, max_retries = EXCLUDED.max_retries,
			              updated_by = EXCLUDED.updated_by, updated_at = now()`,
			string(fn), tenantID, string(raw), p.MaxRetries, user.AccountID)
	}
	if err != nil {
		rlog.Error("ai config upsert failed", "function", fn, "err", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal menyimpan konfigurasi AI"}
	}
	llm.NotifyConfigChanged()
	auditAIConfig(ctx, user.AccountID, tenantID, "ai_function_config_put", string(fn), map[string]any{
		"chain": chain, "maxRetries": p.MaxRetries, "actorEmail": user.Email,
	})
	out := &AIFunctionConfig{Function: string(fn), Chain: fromLLMChain(chain), MaxRetries: p.MaxRetries}
	if tenantID != "" {
		out.TenantID = &tenantID
	}
	if fn == llm.FnEmbedding && embedFrom != embedTo {
		info, err := triggerEmbeddingReindex(ctx, tenantID, embedFrom, embedTo)
		if err != nil {
			return nil, err
		}
		out.Reindex = info
	}
	return out, nil
}

type DeleteAIFunctionConfigParams struct {
	TenantID string `query:"tenantId"`
	// ConfirmReindex is required when removing an embedding override changes the effective model.
	ConfirmReindex bool `query:"confirmReindex"`
}

type DeleteAIFunctionConfigResponse struct {
	Deleted bool           `json:"deleted"`
	Reindex *AIReindexInfo `json:"reindex,omitempty"`
}

// DeleteAIFunctionConfig removes a tenant override (?tenantId= required; global rows are not deletable here).
//
//encore:api auth method=DELETE path=/api/v1/admin/ai/function-config/:function tag:super_admin
func DeleteAIFunctionConfig(ctx context.Context, function string, p *DeleteAIFunctionConfigParams) (*DeleteAIFunctionConfigResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	fn := llm.Function(strings.TrimSpace(function))
	if _, ok := llm.FunctionProviders[fn]; !ok {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "function tidak dikenal"}
	}
	if strings.TrimSpace(p.TenantID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "tenantId wajib (hanya override tenant yang bisa dihapus)"}
	}
	tenantID, err := requireTenant(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	var embedFrom, embedTo string
	if fn == llm.FnEmbedding {
		if embedFrom, err = effectiveEmbeddingModel(ctx, tenantID); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "gagal membaca konfigurasi embedding saat ini"}
		}
		if embedTo, err = effectiveEmbeddingModel(ctx, ""); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "gagal membaca konfigurasi embedding global"}
		}
		if err := requireReindexConfirmation(embedFrom, embedTo, p.ConfirmReindex); err != nil {
			return nil, err
		}
	}
	res, err := db.Stdlib().ExecContext(ctx,
		`DELETE FROM ai_function_config WHERE function = $1 AND tenant_id = $2`, string(fn), tenantID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal menghapus override"}
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		llm.NotifyConfigChanged()
		auditAIConfig(ctx, user.AccountID, tenantID, "ai_function_config_delete", string(fn), map[string]any{"actorEmail": user.Email})
	}
	resp := &DeleteAIFunctionConfigResponse{Deleted: n > 0}
	if n > 0 && fn == llm.FnEmbedding && embedFrom != embedTo {
		info, err := triggerEmbeddingReindex(ctx, tenantID, embedFrom, embedTo)
		if err != nil {
			return nil, err
		}
		resp.Reindex = info
	}
	return resp, nil
}

type AIProviderInfo struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
}

type GetAICatalogResponse struct {
	Providers []AIProviderInfo `json:"providers"`
	Models    []llm.ModelInfo  `json:"models"`
}

// GetAICatalog lists providers (enabled in DB, available = credentials present at runtime) and allowlisted models.
//
//encore:api auth method=GET path=/api/v1/admin/ai/catalog tag:super_admin
func GetAICatalog(ctx context.Context) (*GetAICatalogResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	ai.SyncProviderAvailabilityForCatalog()
	retrieval.SyncOpenAIProviderAvailability()
	enabled := map[string]bool{}
	rows, err := db.Stdlib().QueryContext(ctx, `SELECT id, enabled FROM ai_providers`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var en bool
			if rows.Scan(&id, &en) == nil {
				enabled[id] = en
			}
		}
	}
	out := &GetAICatalogResponse{Models: llm.CatalogModels()}
	for _, id := range llm.KnownProviders() {
		en, ok := enabled[id]
		if !ok {
			en = true // no row = not disabled
		}
		out.Providers = append(out.Providers, AIProviderInfo{ID: id, Enabled: en, Available: llm.ProviderAvailable(id)})
	}
	return out, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

func toLLMChain(in []AIChainLink) []llm.ChainLink {
	out := make([]llm.ChainLink, 0, len(in))
	for _, l := range in {
		out = append(out, llm.ChainLink{Provider: strings.TrimSpace(l.Provider), Model: strings.TrimSpace(l.Model), TimeoutMS: l.TimeoutMS})
	}
	return out
}

func fromLLMChain(in []llm.ChainLink) []AIChainLink {
	out := make([]AIChainLink, 0, len(in))
	for _, l := range in {
		out = append(out, AIChainLink{Provider: l.Provider, Model: l.Model, TimeoutMS: l.TimeoutMS})
	}
	return out
}

func optionalTenant(ctx context.Context, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return requireTenant(ctx, raw)
}

// requireTenant: tenantId must be a UUID of an existing tenant.
func requireTenant(ctx context.Context, raw string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", &errs.Error{Code: errs.InvalidArgument, Message: "tenantId tidak valid"}
	}
	var exists bool
	if err := db.Stdlib().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant WHERE id = $1::uuid)`, id.String()).Scan(&exists); err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		return "", &errs.Error{Code: errs.Internal, Message: "gagal memeriksa tenant"}
	}
	if !exists {
		return "", &errs.Error{Code: errs.NotFound, Message: "tenant tidak ditemukan"}
	}
	return id.String(), nil
}

func auditAIConfig(ctx context.Context, actorID, tenantID, action, fn string, changes map[string]any) {
	rlog.Info("ai config changed", "action", action, "function", fn, "tenantId", tenantID, "actor", actorID)
	audit.Log(ctx, tenantID, actorID, action, "ai_function_config", fn, changes)
}
