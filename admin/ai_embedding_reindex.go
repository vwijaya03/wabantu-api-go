package admin

import (
	"context"
	"fmt"
	"sync"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appflag "encore.app/wabantu/flag"
	"encore.app/wabantu/kb"
	"encore.app/wabantu/shared/llm"
	"encore.app/wabantu/shared/retrieval"
)

// Changing the embedding model invalidates every stored vector of the affected namespaces
// (OpenAI and Gemini vector spaces are not comparable). PUT/DELETE therefore needs an explicit
// confirmReindex and only wipes + enqueues; embedding runs in the retrieval-index worker.

const (
	reindexModeInline     = "inline"
	reindexModeBackground = "background"

	maxConcurrentReindex = 4
)

// AIReindexInfo reports the reindex triggered by an embedding model change.
type AIReindexInfo struct {
	Tenants        int    `json:"tenants"`
	Mode           string `json:"mode"` // inline | background
	FromModel      string `json:"fromModel"`
	ToModel        string `json:"toModel"`
	ProgressHint   string `json:"progressHint"`
	QueuedTenantID string `json:"queuedTenantId,omitempty"`
}

// effectiveEmbeddingModel mirrors the runtime resolver (tenant override → global → OpenAI default)
// but reads the DB directly so the admin handler sees its own just-written rows.
func effectiveEmbeddingModel(ctx context.Context, tenantID string) (string, error) {
	store := llm.NewSQLStore(db.Stdlib())
	if store == nil {
		return retrieval.EmbeddingModel, nil
	}
	if tenantID != "" {
		cfg, ok, err := store.LoadFunctionConfig(ctx, llm.FnEmbedding, tenantID)
		if err != nil {
			return "", err
		}
		if ok {
			return retrieval.EmbeddingModelFromChain(cfg.Chain), nil
		}
	}
	cfg, ok, err := store.LoadFunctionConfig(ctx, llm.FnEmbedding, "")
	if err != nil {
		return "", err
	}
	if !ok {
		return retrieval.EmbeddingModel, nil
	}
	return retrieval.EmbeddingModelFromChain(cfg.Chain), nil
}

// requireReindexConfirmation fails with FailedPrecondition when the model changes without confirmation.
func requireReindexConfirmation(from, to string, confirmed bool) error {
	if from == to || confirmed {
		return nil
	}
	return &errs.Error{
		Code: errs.FailedPrecondition,
		Message: fmt.Sprintf("mengganti model embedding (%s → %s) menghapus semua vektor namespace terkait dan mengantrikan reindex; "+
			"kirim confirmReindex=true untuk melanjutkan", from, to),
	}
}

// requireEmbeddingCredentials refuses a switch to a model this environment cannot embed with.
func requireEmbeddingCredentials(model string) error {
	if model == retrieval.GeminiEmbeddingModel && retrieval.GeminiAPIKeyFromEnv() == "" {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "GeminiAPIKey belum diset di environment ini; model Gemini belum bisa dipakai untuk embedding"}
	}
	return nil
}

type reindexTarget struct{ tenantID, schema string }

// reindexTargets lists tenants whose effective model changes: the one tenant for an override, or every
// tenant without its own embedding override for a global change. Tenants with retrieval disabled are skipped.
func reindexTargets(ctx context.Context, tenantID string) ([]reindexTarget, error) {
	var (
		query string
		args  []any
	)
	if tenantID != "" {
		query = `SELECT tc.tenant_id::text, tc.schema_name FROM tenant_company tc WHERE tc.tenant_id = $1::uuid`
		args = []any{tenantID}
	} else {
		query = `
			SELECT tc.tenant_id::text, tc.schema_name
			FROM tenant_company tc
			JOIN tenant t ON t.id = tc.tenant_id AND t.deleted_at IS NULL
			WHERE NOT EXISTS (
			  SELECT 1 FROM ai_function_config c
			  WHERE c.function = 'embedding' AND c.tenant_id = tc.tenant_id::text)
			ORDER BY tc.tenant_id`
	}
	rows, err := db.Stdlib().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reindexTarget
	for rows.Next() {
		var t reindexTarget
		if err := rows.Scan(&t.tenantID, &t.schema); err != nil {
			return nil, err
		}
		if appflag.TenantRetrievalMode(ctx, t.tenantID) == retrieval.ModeDisabled {
			continue
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func rebuildTenantForModelChange(ctx context.Context, t reindexTarget) error {
	res, err := kb.RebuildTenantRetrievalIndex(ctx, t.schema, t.tenantID, kb.RebuildTenantRetrievalOptions{
		WipePinecone: true, IncludeKB: true, IncludeCatalog: true,
	})
	if err != nil {
		return err
	}
	rlog.Info("embedding model change: tenant reindex queued",
		"tenantId", t.tenantID, "model", res.EmbeddingModel, "kbEnqueued", res.KBEnqueued, "catalogEnqueued", res.CatalogEnqueued)
	return nil
}

// triggerEmbeddingReindex wipes + enqueues affected tenants. A single tenant runs inline (enqueue only);
// a global change fans out in a detached, bounded worker so the HTTP request never waits on N tenants.
func triggerEmbeddingReindex(ctx context.Context, tenantID, from, to string) (*AIReindexInfo, error) {
	targets, err := reindexTargets(ctx, tenantID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal memuat daftar tenant untuk reindex"}
	}
	info := &AIReindexInfo{
		Tenants: len(targets), FromModel: from, ToModel: to,
		ProgressHint: "pantau GET /api/v1/flags/retrieval-indexing/:tenantId atau halaman Admin → AI Retrieval",
	}
	if tenantID != "" {
		info.Mode = reindexModeInline
		info.QueuedTenantID = tenantID
		for _, t := range targets {
			if err := rebuildTenantForModelChange(ctx, t); err != nil {
				rlog.Warn("embedding model change: tenant reindex failed", "tenantId", t.tenantID, "err", err)
				return nil, &errs.Error{Code: errs.Internal, Message: "konfigurasi tersimpan tetapi reindex gagal diantrikan; jalankan Reset indexing"}
			}
		}
		return info, nil
	}
	info.Mode = reindexModeBackground
	if len(targets) == 0 {
		return info, nil
	}
	go func() {
		bg := context.Background()
		sem := make(chan struct{}, maxConcurrentReindex)
		var wg sync.WaitGroup
		for _, t := range targets {
			t := t
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-sem; wg.Done() }()
				if err := rebuildTenantForModelChange(bg, t); err != nil {
					rlog.Warn("embedding model change: tenant reindex failed", "tenantId", t.tenantID, "err", err)
				}
			}()
		}
		wg.Wait()
	}()
	return info, nil
}
