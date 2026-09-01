package business

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/ai"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

const (
	catalogTextMaxLen  = 12000
	catalogTextMinLen  = 10
	catalogTextSource  = "text_import"
)

type PreviewCatalogTextRequest struct {
	Text string `json:"text"`
}

func catalogTextStagingKey(jobID string) string {
	return "catalog:text:staging:" + jobID
}

// PreviewCatalogTextImport parses seller text with AI and stages draft rows.
//
//encore:api auth method=POST path=/api/v1/business/catalog/import-text/preview tag:owner
func PreviewCatalogTextImport(ctx context.Context, req *PreviewCatalogTextRequest) (*CatalogImagePreviewResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can preview catalog text import")
	}
	if req == nil {
		return nil, apperr.BadRequest("text is required")
	}
	text := strings.TrimSpace(req.Text)
	if len(text) < catalogTextMinLen {
		return nil, apperr.BadRequest("teks terlalu pendek — minimal 10 karakter")
	}
	if len(text) > catalogTextMaxLen {
		return nil, apperr.BadRequest(fmt.Sprintf("teks maksimal %d karakter", catalogTextMaxLen))
	}

	if _, _, err := ensureAIQuota(ctx, user.TenantSchema); err != nil {
		return nil, err
	}

	rawJSON, compUsage, err := ai.ExtractCatalogFromText(ctx, secrets.AnthropicAPIKey, text)
	if err != nil {
		rlog.Warn("catalog text AI failed", "err", err, "tenant", user.TenantSchema)
		return nil, apperr.BadRequest("AI gagal memproses teks — coba perbaiki format atau coba lagi")
	}

	items, parentTitle, err := parseCatalogVisionJSON(rawJSON)
	if err != nil {
		rlog.Warn("catalog text parse failed", "err", err, "tenant", user.TenantSchema)
		return nil, apperr.BadRequest("hasil AI tidak valid — coba perbaiki teks atau coba lagi")
	}
	if len(items) == 0 {
		return nil, apperr.BadRequest("tidak ada produk terdeteksi dari teks")
	}

	tokens := compUsage.InputTokens + compUsage.OutputTokens
	if tokens > 0 {
		_ = usage.RecordEvent(ctx, user.TenantSchema, "ai_token", tokens, nil)
		_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
			TenantSchema: user.TenantSchema,
			TenantID:     user.TenantID,
			Purpose:      usage.PurposeCatalogImport,
			Path:         "catalog_text_preview",
			Reason:       "text_extract",
			Model:        ai.DefaultHaikuAPIID(),
			Tier:         "haiku",
			LLMUsed:      true,
			InputTokens:  compUsage.InputTokens,
			OutputTokens: compUsage.OutputTokens,
		})
	}

	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	jobID := fmt.Sprintf("ctxt_%d", time.Now().UnixNano())
	staging := catalogImageStaging{
		TenantSchema: user.TenantSchema,
		UploadedBy:   user.AccountID,
		ParentTitle:  parentTitle,
		Items:        items,
		InputTokens:  compUsage.InputTokens,
		OutputTokens: compUsage.OutputTokens,
		Model:        ai.DefaultHaikuAPIID(),
	}
	raw, _ := json.Marshal(staging)
	if err := appauth.RedisClient().Set(ctx, catalogTextStagingKey(jobID), raw, catalogImageStagingTTL).Err(); err != nil {
		return nil, apperr.Internal("failed to stage catalog import")
	}

	return &CatalogImagePreviewResponse{
		JobID:               jobID,
		ParentTitle:         parentTitle,
		Items:               items,
		InputTokens:         compUsage.InputTokens,
		OutputTokens:        compUsage.OutputTokens,
		TokensUsed:          tokens,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(rem, lim),
	}, nil
}

// GetCatalogTextImportDraft returns staged rows for the confirm page.
//
//encore:api auth method=GET path=/api/v1/business/catalog/import-text/draft/:jobId tag:owner
func GetCatalogTextImportDraft(ctx context.Context, jobId string) (*CatalogImagePreviewResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can view catalog text import draft")
	}
	staging, err := loadCatalogTextStaging(ctx, jobId, user.TenantSchema)
	if err != nil {
		return nil, err
	}
	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	return &CatalogImagePreviewResponse{
		JobID:               jobId,
		ParentTitle:         staging.ParentTitle,
		Items:               staging.Items,
		InputTokens:         staging.InputTokens,
		OutputTokens:        staging.OutputTokens,
		TokensUsed:          staging.InputTokens + staging.OutputTokens,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(rem, lim),
	}, nil
}

// CommitCatalogTextImport saves confirmed rows to business_catalog_item (no extra AI tokens).
//
//encore:api auth method=POST path=/api/v1/business/catalog/import-text/draft/:jobId/commit tag:owner
func CommitCatalogTextImport(ctx context.Context, jobId string, req *CommitCatalogImageRequest) (*CommitCatalogImageResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can commit catalog text import")
	}
	if req == nil || len(req.Items) == 0 {
		return nil, apperr.BadRequest("items are required")
	}
	if _, err := loadCatalogTextStaging(ctx, jobId, user.TenantSchema); err != nil {
		return nil, err
	}
	res, err := commitCatalogDraftItems(ctx, user, req.Items, catalogTextSource)
	if err != nil {
		return nil, err
	}
	_ = appauth.RedisClient().Del(ctx, catalogTextStagingKey(jobId)).Err()
	res.JobID = jobId
	return res, nil
}

func loadCatalogTextStaging(ctx context.Context, jobID, tenantSchema string) (*catalogImageStaging, error) {
	raw, err := appauth.RedisClient().Get(ctx, catalogTextStagingKey(jobID)).Bytes()
	if err != nil {
		return nil, apperr.NotFound("draft import tidak ditemukan atau sudah kedaluwarsa")
	}
	var st catalogImageStaging
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, apperr.Internal("invalid staging payload")
	}
	if st.TenantSchema != tenantSchema {
		return nil, apperr.Forbidden("draft import tidak valid untuk tenant ini")
	}
	return &st, nil
}

func commitCatalogDraftItems(ctx context.Context, user *types.AuthUser, items []CatalogImageDraftItem, source string) (*CommitCatalogImageResponse, error) {
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}

	var saved, skipped int
	for _, it := range items {
		if !it.Include {
			skipped++
			continue
		}
		code := strings.TrimSpace(it.ExternalCode)
		name := strings.TrimSpace(it.Name)
		if code == "" || name == "" {
			skipped++
			continue
		}
		var desc *string
		if d := strings.TrimSpace(it.Description); d != "" {
			desc = &d
		}
		unit := strings.TrimSpace(it.SellUnit)
		if unit == "" {
			unit = "pcs"
		}
		price := it.SellPrice

		_, err := ts.ExecContext(ctx, `
			INSERT INTO business_catalog_item
				(external_code, name, description, sell_price, sell_unit, is_active, source)
			VALUES ($1,$2,$3,$4,$5,true,$6)
			ON CONFLICT (source, external_code) WHERE deleted_at IS NULL DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				sell_price = EXCLUDED.sell_price,
				sell_unit = EXCLUDED.sell_unit,
				is_active = true,
				updated_at = NOW()`, code, name, desc, price, unit, source)
		if err != nil {
			rlog.Warn("catalog import commit row failed", "code", code, "source", source, "err", err)
			skipped++
			continue
		}
		saved++
	}

	msg := fmt.Sprintf("%d produk disimpan ke katalog", saved)
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d dilewati)", skipped)
	}

	return &CommitCatalogImageResponse{
		SavedCount:   saved,
		SkippedCount: skipped,
		Message:      msg,
	}, nil
}
