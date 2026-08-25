package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/ai"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

const (
	catalogImageStagingTTL = 24 * time.Hour
	// CatalogImageMaxBytes — batas per file (selaras dengan web-frontend).
	CatalogImageMaxBytes = 5 << 20 // 5 MiB
	// CatalogImageMaxBatchBytes — total semua file dalam satu request.
	CatalogImageMaxBatchBytes = 20 << 20 // 20 MiB
	// CatalogImageMaxFilesPerBatch — maks. screenshot per klik "Proses dengan AI".
	CatalogImageMaxFilesPerBatch = 5
	catalogImageMinBytes         = 1024 // 1 KiB
	catalogImageMaxItems         = 50
	catalogImageSource           = "image_import"
)

// CatalogImageLimits documents upload constraints for clients.
type CatalogImageLimits struct {
	MaxBytes           int64    `json:"maxBytes"`
	MaxMegabytes       int      `json:"maxMegabytes"`
	MaxBatchBytes      int64    `json:"maxBatchBytes"`
	MaxBatchMegabytes  int      `json:"maxBatchMegabytes"`
	MaxFilesPerBatch   int      `json:"maxFilesPerBatch"`
	MinBytes           int64    `json:"minBytes"`
	AllowedMIME        []string `json:"allowedMime"`
	AllowedExt         []string `json:"allowedExt"`
	MaxItemsPerJob     int      `json:"maxItemsPerJob"`
}

// GetCatalogImageLimits returns server-enforced upload limits (no auth required beyond login).
//
//encore:api auth method=GET path=/api/v1/business/catalog/import-image-limits tag:owner
func GetCatalogImageLimits(ctx context.Context) (*CatalogImageLimits, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can view catalog import limits")
	}
	return &CatalogImageLimits{
		MaxBytes:          CatalogImageMaxBytes,
		MaxMegabytes:      5,
		MaxBatchBytes:     CatalogImageMaxBatchBytes,
		MaxBatchMegabytes: 20,
		MaxFilesPerBatch:  CatalogImageMaxFilesPerBatch,
		MinBytes:          catalogImageMinBytes,
		AllowedMIME:       []string{"image/jpeg", "image/png", "image/webp"},
		AllowedExt:        []string{".jpg", ".jpeg", ".png", ".webp"},
		MaxItemsPerJob:    catalogImageMaxItems,
	}, nil
}

// CatalogImageDraftItem is one row shown on the confirm page before save.
type CatalogImageDraftItem struct {
	ExternalCode string   `json:"externalCode"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	SellPrice    *float64 `json:"sellPrice,omitempty"`
	SellUnit     string   `json:"sellUnit,omitempty"`
	Include      bool     `json:"include"`
}

type catalogImageStaging struct {
	TenantSchema    string                  `json:"tenantSchema"`
	UploadedBy      string                  `json:"uploadedBy"`
	ParentTitle     string                  `json:"parentTitle"`
	Items           []CatalogImageDraftItem `json:"items"`
	SourceFilenames []string                `json:"sourceFilenames,omitempty"`
	ImagesProcessed int                     `json:"imagesProcessed"`
	InputTokens     int                     `json:"inputTokens"`
	OutputTokens    int                     `json:"outputTokens"`
	Model           string                  `json:"model"`
}

type CatalogImagePreviewResponse struct {
	JobID               string                  `json:"jobId"`
	ParentTitle         string                  `json:"parentTitle,omitempty"`
	Items               []CatalogImageDraftItem `json:"items"`
	SourceFilenames     []string                `json:"sourceFilenames,omitempty"`
	ImagesProcessed     int                     `json:"imagesProcessed"`
	Warnings            []string                `json:"warnings,omitempty"`
	InputTokens         int                     `json:"inputTokens"`
	OutputTokens        int                     `json:"outputTokens"`
	TokensUsed          int                     `json:"tokensUsed"`
	TokenQuotaRemaining int                     `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int                     `json:"tokenQuotaLimit"`
	QuotaNotice         string                  `json:"quotaNotice"`
}

type CommitCatalogImageRequest struct {
	Items []CatalogImageDraftItem `json:"items"`
}

type CommitCatalogImageResponse struct {
	JobID       string `json:"jobId"`
	SavedCount  int    `json:"savedCount"`
	SkippedCount int   `json:"skippedCount"`
	Message     string `json:"message"`
}

type catalogVisionExtract struct {
	ParentTitle string `json:"parentTitle"`
	Items       []struct {
		ExternalCode string   `json:"externalCode"`
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		SellPrice    float64  `json:"sellPrice"`
		SellUnit     string   `json:"sellUnit"`
	} `json:"items"`
}

func catalogImageStagingKey(jobID string) string {
	return "catalog:image:staging:" + jobID
}

func catalogImageErrMessage(err error) string {
	var e *errs.Error
	if errors.As(err, &e) && strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return err.Error()
}

func aiTokenQuotaNotice(remaining, limit int) string {
	if limit <= 0 {
		return "Setiap penggunaan fitur AI (termasuk import dari gambar) akan mengurangi kuota token AI bulanan toko Anda."
	}
	return fmt.Sprintf(
		"Setiap penggunaan fitur AI (termasuk import dari gambar) akan mengurangi kuota token AI bulanan toko Anda. Sisa kuota token bulan ini: %d dari %d.",
		remaining, limit,
	)
}

func ensureAIQuota(ctx context.Context, tenantSchema string) (remaining, limit int, err error) {
	allowed, rem, lim := usage.CheckQuota(ctx, tenantSchema, "ai_token")
	if !allowed {
		return rem, lim, apperr.Forbidden("kuota token AI bulan ini sudah habis — tidak dapat memproses gambar dengan AI")
	}
	return rem, lim, nil
}

type catalogImageFileResult struct {
	items        []CatalogImageDraftItem
	parentTitle  string
	inputTokens  int
	outputTokens int
	warning      string
}

func readCatalogImageBytes(fh *multipart.FileHeader) ([]byte, string, error) {
	if fh == nil {
		return nil, "", apperr.BadRequest("file is required")
	}
	if fh.Size > CatalogImageMaxBytes {
		return nil, "", apperr.BadRequest(fmt.Sprintf("ukuran gambar maksimal %d MB per file", CatalogImageMaxBytes/(1<<20)))
	}
	if fh.Size > 0 && fh.Size < catalogImageMinBytes {
		return nil, "", apperr.BadRequest("file gambar terlalu kecil atau rusak")
	}
	ext := catalogImageExt(fh)
	if ext == "" {
		return nil, "", apperr.BadRequest("format gambar: JPG, PNG, atau WEBP")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, "", apperr.BadRequest("cannot open uploaded file")
	}
	defer f.Close()
	imageBytes, err := io.ReadAll(io.LimitReader(f, CatalogImageMaxBytes+1))
	if err != nil {
		return nil, "", apperr.Internal("read image failed")
	}
	if len(imageBytes) > int(CatalogImageMaxBytes) {
		return nil, "", apperr.BadRequest(fmt.Sprintf("ukuran gambar maksimal %d MB per file", CatalogImageMaxBytes/(1<<20)))
	}
	if len(imageBytes) < catalogImageMinBytes {
		return nil, "", apperr.BadRequest("file gambar terlalu kecil atau rusak")
	}
	return imageBytes, mimeFromExt(ext), nil
}

func processCatalogImageFile(
	ctx context.Context,
	user *types.AuthUser,
	fh *multipart.FileHeader,
) (catalogImageFileResult, error) {
	var out catalogImageFileResult
	imageBytes, mediaType, err := readCatalogImageBytes(fh)
	if err != nil {
		return out, err
	}
	rawJSON, compUsage, err := ai.ExtractCatalogFromScreenshot(ctx, secrets.AnthropicAPIKey, imageBytes, mediaType)
	if err != nil {
		rlog.Warn("catalog image vision failed", "err", err, "file", fh.Filename, "tenant", user.TenantSchema)
		return out, fmt.Errorf("AI gagal membaca %s", fh.Filename)
	}
	items, parentTitle, err := parseCatalogVisionJSON(rawJSON)
	if err != nil {
		return out, fmt.Errorf("hasil AI tidak valid untuk %s: %w", fh.Filename, err)
	}
	if len(items) == 0 {
		out.warning = fmt.Sprintf("%s: tidak ada produk terdeteksi", fh.Filename)
		return out, nil
	}
	out.items = items
	out.parentTitle = parentTitle
	out.inputTokens = compUsage.InputTokens
	out.outputTokens = compUsage.OutputTokens
	return out, nil
}

func mergeCatalogDraftItems(
	existing []CatalogImageDraftItem,
	seen map[string]struct{},
	incoming []CatalogImageDraftItem,
) []CatalogImageDraftItem {
	for _, it := range incoming {
		if len(existing) >= catalogImageMaxItems {
			break
		}
		code := strings.TrimSpace(it.ExternalCode)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		existing = append(existing, it)
	}
	return existing
}

func previewCatalogFromMultipart(
	ctx context.Context,
	user *types.AuthUser,
	files []*multipart.FileHeader,
) (*CatalogImagePreviewResponse, error) {
	if len(files) == 0 {
		return nil, apperr.BadRequest("pilih minimal satu file gambar")
	}
	if len(files) > CatalogImageMaxFilesPerBatch {
		return nil, apperr.BadRequest(fmt.Sprintf("maksimal %d gambar per proses", CatalogImageMaxFilesPerBatch))
	}

	var totalSize int64
	for _, fh := range files {
		totalSize += fh.Size
	}
	if totalSize > CatalogImageMaxBatchBytes {
		return nil, apperr.BadRequest(fmt.Sprintf("total ukuran semua gambar maksimal %d MB", CatalogImageMaxBatchBytes/(1<<20)))
	}

	if _, _, err := ensureAIQuota(ctx, user.TenantSchema); err != nil {
		return nil, err
	}

	var (
		allItems    []CatalogImageDraftItem
		seen        = make(map[string]struct{})
		parentTitle string
		sourceNames []string
		warnings    []string
		inTok       int
		outTok      int
		processed   int
	)

	for _, fh := range files {
		allowed, _, _ := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
		if !allowed {
			warnings = append(warnings, "kuota token habis — sisa gambar tidak diproses")
			break
		}

		res, err := processCatalogImageFile(ctx, user, fh)
		if err != nil {
			warnings = append(warnings, catalogImageErrMessage(err))
			continue
		}
		if res.warning != "" {
			warnings = append(warnings, res.warning)
			continue
		}

		tokens := res.inputTokens + res.outputTokens
		if tokens > 0 {
			_ = usage.RecordEvent(ctx, user.TenantSchema, "ai_token", tokens, nil)
			_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
				TenantSchema: user.TenantSchema,
				TenantID:     user.TenantID,
				Purpose:      usage.PurposeCatalogImport,
				Path:         "catalog_image_preview",
				Reason:       "vision_extract",
				Model:        ai.DefaultHaikuAPIID(),
				Tier:         "haiku",
				LLMUsed:      true,
				InputTokens:  res.inputTokens,
				OutputTokens: res.outputTokens,
			})
		}
		inTok += res.inputTokens
		outTok += res.outputTokens
		processed++
		sourceNames = append(sourceNames, fh.Filename)
		if parentTitle == "" && res.parentTitle != "" {
			parentTitle = res.parentTitle
		}
		allItems = mergeCatalogDraftItems(allItems, seen, res.items)
		if len(allItems) >= catalogImageMaxItems {
			warnings = append(warnings, fmt.Sprintf("batas %d produk per job tercapai — sisa gambar diabaikan", catalogImageMaxItems))
			break
		}
	}

	if processed == 0 {
		msg := "tidak ada gambar yang berhasil diproses"
		if len(warnings) > 0 {
			msg = strings.Join(warnings, "; ")
		}
		return nil, apperr.BadRequest(msg)
	}
	if len(allItems) == 0 {
		msg := "tidak ada produk terdeteksi dari gambar yang diproses"
		if len(warnings) > 0 {
			msg += " (" + strings.Join(warnings, "; ") + ")"
		}
		return nil, apperr.BadRequest(msg)
	}

	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	jobID := fmt.Sprintf("cimg_%d", time.Now().UnixNano())
	staging := catalogImageStaging{
		TenantSchema:    user.TenantSchema,
		UploadedBy:      user.AccountID,
		ParentTitle:     parentTitle,
		Items:           allItems,
		SourceFilenames: sourceNames,
		ImagesProcessed: processed,
		InputTokens:     inTok,
		OutputTokens:    outTok,
		Model:           ai.DefaultHaikuAPIID(),
	}
	raw, _ := json.Marshal(staging)
	if err := appauth.RedisClient().Set(ctx, catalogImageStagingKey(jobID), raw, catalogImageStagingTTL).Err(); err != nil {
		return nil, apperr.Internal("failed to stage catalog import")
	}

	return &CatalogImagePreviewResponse{
		JobID:               jobID,
		ParentTitle:         parentTitle,
		Items:               allItems,
		SourceFilenames:     sourceNames,
		ImagesProcessed:     processed,
		Warnings:            warnings,
		InputTokens:         inTok,
		OutputTokens:        outTok,
		TokensUsed:          inTok + outTok,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(rem, lim),
	}, nil
}

// GetCatalogImageImportDraft returns staged rows for the confirm page.
//
//encore:api auth method=GET path=/api/v1/business/catalog/import-image/draft/:jobId tag:owner
func GetCatalogImageImportDraft(ctx context.Context, jobId string) (*CatalogImagePreviewResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can view catalog import draft")
	}
	staging, err := loadCatalogImageStaging(ctx, jobId, user.TenantSchema)
	if err != nil {
		return nil, err
	}
	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	return &CatalogImagePreviewResponse{
		JobID:               jobId,
		ParentTitle:         staging.ParentTitle,
		Items:               staging.Items,
		SourceFilenames:     staging.SourceFilenames,
		ImagesProcessed:     staging.ImagesProcessed,
		InputTokens:         staging.InputTokens,
		OutputTokens:        staging.OutputTokens,
		TokensUsed:          staging.InputTokens + staging.OutputTokens,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         aiTokenQuotaNotice(rem, lim),
	}, nil
}

// CommitCatalogImageImport saves confirmed rows to business_catalog_item (no extra AI tokens).
//
//encore:api auth method=POST path=/api/v1/business/catalog/import-image/draft/:jobId/commit tag:owner
func CommitCatalogImageImport(ctx context.Context, jobId string, req *CommitCatalogImageRequest) (*CommitCatalogImageResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can commit catalog import")
	}
	if req == nil || len(req.Items) == 0 {
		return nil, apperr.BadRequest("items are required")
	}
	staging, err := loadCatalogImageStaging(ctx, jobId, user.TenantSchema)
	if err != nil {
		return nil, err
	}
	_ = staging

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer closeTenantConn(conn)

	var saved, skipped int
	for _, it := range req.Items {
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

		_, err := conn.ExecContext(ctx, `
			INSERT INTO business_catalog_item
				(external_code, name, description, sell_price, sell_unit, is_active, source)
			VALUES ($1,$2,$3,$4,$5,true,$6)
			ON CONFLICT (source, external_code) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				sell_price = EXCLUDED.sell_price,
				sell_unit = EXCLUDED.sell_unit,
				is_active = true,
				updated_at = NOW()`, code, name, desc, price, unit, catalogImageSource)
		if err != nil {
			rlog.Warn("catalog image commit row failed", "code", code, "err", err)
			skipped++
			continue
		}
		saved++
	}

	_ = appauth.RedisClient().Del(ctx, catalogImageStagingKey(jobId)).Err()

	msg := fmt.Sprintf("%d produk disimpan ke katalog", saved)
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d dilewati)", skipped)
	}

	return &CommitCatalogImageResponse{
		JobID:       jobId,
		SavedCount:  saved,
		SkippedCount: skipped,
		Message:     msg,
	}, nil
}

func loadCatalogImageStaging(ctx context.Context, jobID, tenantSchema string) (*catalogImageStaging, error) {
	raw, err := appauth.RedisClient().Get(ctx, catalogImageStagingKey(jobID)).Bytes()
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

func parseCatalogVisionJSON(raw string) ([]CatalogImageDraftItem, string, error) {
	extracts, err := decodeCatalogVisionPayloads(raw)
	if err != nil {
		return nil, "", err
	}
	if len(extracts) == 0 {
		return nil, "", fmt.Errorf("JSON tidak valid: tidak ada data produk")
	}

	var (
		out    []CatalogImageDraftItem
		seen   = map[string]struct{}{}
		parent string
	)
	for _, ext := range extracts {
		if parent == "" {
			parent = strings.TrimSpace(ext.ParentTitle)
		}
		var batch []CatalogImageDraftItem
		batch, seen = visionRowsToDraftItems(ext, parent, seen)
		out = append(out, batch...)
		if len(out) >= catalogImageMaxItems {
			out = out[:catalogImageMaxItems]
			break
		}
	}
	if len(out) == 0 {
		return nil, parent, fmt.Errorf("tidak ada baris produk dalam JSON AI")
	}
	return out, parent, nil
}

// decodeCatalogVisionPayloads accepts one JSON object, several objects back-to-back, or a JSON array.
func decodeCatalogVisionPayloads(raw string) ([]catalogVisionExtract, error) {
	raw = strings.TrimSpace(ai.SanitizeVisionJSON(raw))
	if raw == "" {
		return nil, fmt.Errorf("JSON kosong")
	}

	dec := json.NewDecoder(strings.NewReader(raw))
	var out []catalogVisionExtract
	for {
		var chunk json.RawMessage
		err := dec.Decode(&chunk)
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(out) > 0 {
				break
			}
			return nil, fmt.Errorf("JSON tidak valid: %w", err)
		}

		chunk = bytes.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		if chunk[0] == '[' {
			var arr []catalogVisionExtract
			if err := json.Unmarshal(chunk, &arr); err != nil {
				return nil, fmt.Errorf("JSON tidak valid: %w", err)
			}
			out = append(out, arr...)
			continue
		}
		var ext catalogVisionExtract
		if err := json.Unmarshal(chunk, &ext); err != nil {
			return nil, fmt.Errorf("JSON tidak valid: %w", err)
		}
		out = append(out, ext)
	}
	return out, nil
}

func visionRowsToDraftItems(
	ext catalogVisionExtract,
	fallbackParent string,
	seen map[string]struct{},
) ([]CatalogImageDraftItem, map[string]struct{}) {
	parent := strings.TrimSpace(ext.ParentTitle)
	if parent == "" {
		parent = strings.TrimSpace(fallbackParent)
	}
	var out []CatalogImageDraftItem
	for i, row := range ext.Items {
		name := strings.TrimSpace(row.Name)
		if name == "" && parent != "" {
			name = parent
		}
		if name == "" {
			name = fmt.Sprintf("Produk %d", i+1)
		}
		code := strings.TrimSpace(row.ExternalCode)
		if code == "" {
			code = slugCatalogCode(name, i)
		}
		code = trimCode(code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			code = fmt.Sprintf("%s-%d", code, len(seen)+1)
		}
		seen[code] = struct{}{}

		var price *float64
		if row.SellPrice > 0 {
			p := row.SellPrice
			price = &p
		}
		unit := strings.TrimSpace(row.SellUnit)
		if unit == "" {
			unit = "pcs"
		}
		out = append(out, CatalogImageDraftItem{
			ExternalCode: code,
			Name:         name,
			Description:  strings.TrimSpace(row.Description),
			SellPrice:    price,
			SellUnit:     unit,
			Include:      true,
		})
	}
	return out, seen
}

var nonAlnumCode = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugCatalogCode(name string, idx int) string {
	s := strings.ToUpper(nonAlnumCode.ReplaceAllString(strings.TrimSpace(name), "-"))
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = s[:48]
	}
	if s == "" {
		return fmt.Sprintf("ITEM-%d", idx+1)
	}
	return s
}

func trimCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) > 64 {
		code = code[:64]
	}
	return code
}

func catalogImageExt(fh *multipart.FileHeader) string {
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return ext
	}
	ct := strings.ToLower(strings.TrimSpace(fh.Header.Get("Content-Type")))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	return ""
}

func mimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
