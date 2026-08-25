package finance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/aivision"
	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

// secrets — nama struct wajib "secrets" agar Encore menyuntikkan nilai (sama dengan service business/ai).
var secrets struct {
	AnthropicAPIKey   string
	DataEncryptionKey string
}

const (
	txnImageStagingTTL       = 24 * time.Hour
	txnImageMaxBytes         = 5 << 20
	txnImageMaxBatchBytes    = 20 << 20
	txnImageMaxFilesPerBatch = 5
	txnImageMinBytes         = 1024
	txnImageMaxItems         = 50
	txnImageSourceTag        = "image-import"
)

// TransactionImageLimits documents upload constraints for clients.
type TransactionImageLimits struct {
	MaxBytes          int64    `json:"maxBytes"`
	MaxMegabytes      int      `json:"maxMegabytes"`
	MaxBatchBytes     int64    `json:"maxBatchBytes"`
	MaxBatchMegabytes int      `json:"maxBatchMegabytes"`
	MaxFilesPerBatch  int      `json:"maxFilesPerBatch"`
	MinBytes          int64    `json:"minBytes"`
	AllowedMIME       []string `json:"allowedMime"`
	AllowedExt        []string `json:"allowedExt"`
	MaxItemsPerJob    int      `json:"maxItemsPerJob"`
}

// TransactionImageDraftItem is one row on the confirm page before save.
type TransactionImageDraftItem struct {
	DraftKey         string   `json:"draftKey"`
	Type             string   `json:"type"`
	TypeSignals      []string `json:"typeSignals,omitempty"`
	Amount           float64  `json:"amount"`
	Description      string   `json:"description"`
	TransactionDate  string   `json:"transactionDate"`
	WalletID         string   `json:"walletId,omitempty"`
	CategoryID       string   `json:"categoryId,omitempty"`
	WalletNameHint   string   `json:"walletNameHint,omitempty"`
	CategoryNameHint string   `json:"categoryNameHint,omitempty"`
	Include          bool     `json:"include"`
}

type transactionImageStaging struct {
	TenantSchema    string                      `json:"tenantSchema"`
	UploadedBy      string                      `json:"uploadedBy"`
	Items           []TransactionImageDraftItem `json:"items"`
	SourceFilenames []string                    `json:"sourceFilenames,omitempty"`
	ImagesProcessed int                         `json:"imagesProcessed"`
	InputTokens     int                         `json:"inputTokens"`
	OutputTokens    int                         `json:"outputTokens"`
	Model           string                      `json:"model"`
}

type TransactionImagePreviewResponse struct {
	JobID               string                      `json:"jobId"`
	Items               []TransactionImageDraftItem `json:"items"`
	SourceFilenames     []string                    `json:"sourceFilenames,omitempty"`
	ImagesProcessed     int                         `json:"imagesProcessed"`
	Warnings            []string                    `json:"warnings,omitempty"`
	InputTokens         int                         `json:"inputTokens"`
	OutputTokens        int                         `json:"outputTokens"`
	TokensUsed          int                         `json:"tokensUsed"`
	TokenQuotaRemaining int                         `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int                         `json:"tokenQuotaLimit"`
	QuotaNotice         string                      `json:"quotaNotice"`
}

type CommitTransactionImageRequest struct {
	Items []TransactionImageDraftItem `json:"items"`
}

type CommitTransactionImageResponse struct {
	JobID        string `json:"jobId"`
	SavedCount   int    `json:"savedCount"`
	SkippedCount int    `json:"skippedCount"`
	Message      string `json:"message"`
}

type transactionVisionExtract struct {
	Items []struct {
		Type             string   `json:"type"`
		TypeSignals      []string `json:"typeSignals"`
		Amount           float64  `json:"amount"`
		Description      string   `json:"description"`
		TransactionDate  string   `json:"transactionDate"`
		WalletNameHint   string   `json:"walletNameHint"`
		CategoryNameHint string   `json:"categoryNameHint"`
	} `json:"items"`
}

func transactionImageStagingKey(jobID string) string {
	return "finance:txn:image:staging:" + jobID
}

func txnImageErrMessage(err error) string {
	var e *errs.Error
	if errors.As(err, &e) && strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return err.Error()
}

func txnImageQuotaNotice(remaining, limit int) string {
	if limit <= 0 {
		return "Setiap penggunaan fitur AI (termasuk import transaksi dari gambar) akan mengurangi kuota token AI bulanan toko Anda."
	}
	return fmt.Sprintf(
		"Setiap penggunaan fitur AI (termasuk import transaksi dari gambar) akan mengurangi kuota token AI bulanan toko Anda. Sisa kuota token bulan ini: %d dari %d.",
		remaining, limit,
	)
}

func ensureTxnImageAIQuota(ctx context.Context, tenantSchema string) (remaining, limit int, err error) {
	allowed, rem, lim := usage.CheckQuota(ctx, tenantSchema, "ai_token")
	if !allowed {
		return rem, lim, appErrs.Forbidden("kuota token AI bulan ini sudah habis — tidak dapat memproses gambar dengan AI")
	}
	return rem, lim, nil
}

//encore:api auth method=GET path=/api/v1/finance/transactions/import-image-limits tag:owner
func GetTransactionImageLimits(ctx context.Context) (*TransactionImageLimits, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	return &TransactionImageLimits{
		MaxBytes:          txnImageMaxBytes,
		MaxMegabytes:      5,
		MaxBatchBytes:     txnImageMaxBatchBytes,
		MaxBatchMegabytes: 20,
		MaxFilesPerBatch:  txnImageMaxFilesPerBatch,
		MinBytes:          txnImageMinBytes,
		AllowedMIME:       []string{"image/jpeg", "image/png", "image/webp"},
		AllowedExt:        []string{".jpg", ".jpeg", ".png", ".webp"},
		MaxItemsPerJob:    txnImageMaxItems,
	}, nil
}

//encore:api auth method=GET path=/api/v1/finance/transactions/import-image/draft/:jobId tag:owner
func GetTransactionImageImportDraft(ctx context.Context, jobId string) (*TransactionImagePreviewResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	staging, err := loadTransactionImageStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	_, rem, lim := usage.CheckQuota(ctx, u.TenantSchema, "ai_token")
	return &TransactionImagePreviewResponse{
		JobID:               jobId,
		Items:               staging.Items,
		SourceFilenames:     staging.SourceFilenames,
		ImagesProcessed:     staging.ImagesProcessed,
		InputTokens:         staging.InputTokens,
		OutputTokens:        staging.OutputTokens,
		TokensUsed:          staging.InputTokens + staging.OutputTokens,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         txnImageQuotaNotice(rem, lim),
	}, nil
}

//encore:api auth method=POST path=/api/v1/finance/transactions/import-image/draft/:jobId/commit tag:owner
func CommitTransactionImageImport(ctx context.Context, jobId string, req *CommitTransactionImageRequest) (*CommitTransactionImageResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if req == nil || len(req.Items) == 0 {
		return nil, appErrs.BadRequest("items are required")
	}
	if _, err := loadTransactionImageStaging(ctx, jobId, u.TenantSchema); err != nil {
		return nil, err
	}

	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	defaultWallet, err := resolveDefaultExpenseWallet(ctx, sch, q)
	if err != nil {
		return nil, err
	}

	var saved, skipped int
	walletsToRefresh := make(map[string]struct{})

	for _, it := range req.Items {
		if !it.Include {
			skipped++
			continue
		}
		txType := normalizeTxnImageType(it.Type)
		if txType == "" {
			skipped++
			continue
		}
		if it.Amount <= 0 {
			skipped++
			continue
		}
		desc := strings.TrimSpace(it.Description)
		if desc == "" {
			skipped++
			continue
		}

		walletID := strings.TrimSpace(it.WalletID)
		if walletID == "" {
			walletID, _ = resolveWalletByNameHint(ctx, sch, q, it.WalletNameHint)
		}
		if walletID == "" {
			walletID = defaultWallet
		}
		if err := assertWalletAccessible(ctx, sch, q, u, walletID); err != nil {
			skipped++
			continue
		}

		txDate := strings.TrimSpace(it.TransactionDate)
		if txDate == "" {
			txDate = financeToday(ctx, sch, q)
		}
		if err := ensurePeriodUnlocked(ctx, sch, q, walletPeriod(txDate)); err != nil {
			return nil, err
		}

		var catID *string
		if cid := strings.TrimSpace(it.CategoryID); cid != "" {
			catID = &cid
		} else if cid, ok := resolveCategoryByHint(ctx, sch, q, it.CategoryNameHint, txType); ok {
			catID = &cid
		}

		ref := fmt.Sprintf("imgimport:%s:%s", jobId, strings.TrimSpace(it.DraftKey))
		var exists bool
		if err := qrow(ctx, sch, q, `
			SELECT EXISTS(
			  SELECT 1 FROM fin_transaction
			  WHERE reference_no=$1 AND deleted_at IS NULL LIMIT 1
			)`, ref).Scan(&exists); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if exists {
			skipped++
			continue
		}

		tags := []string{txnImageSourceTag}
		var id string
		err = qrow(ctx, sch, q, `
			INSERT INTO fin_transaction
			 (type, amount, currency, wallet_id, category_id, description,
			  reference_no, transaction_date, status, tags, created_by)
			 VALUES ($1,$2,'IDR',$3,$4,$5,$6,$7,'approved',$8,$9)
			 RETURNING id`,
			txType, it.Amount, walletID, catID, desc, ref, txDate, tags, u.AccountID,
		).Scan(&id)
		if err != nil {
			rlog.Warn("transaction image commit row failed", "draftKey", it.DraftKey, "err", err)
			skipped++
			continue
		}
		walletsToRefresh[walletID] = struct{}{}
		saved++
		auditFinance(ctx, sch, q, u, "transaction", id, "create", nil, map[string]any{
			"type": txType, "amount": it.Amount, "source": txnImageSourceTag,
		})
	}

	for w := range walletsToRefresh {
		refreshWallets(ctx, sch, q, w, nil)
	}
	if saved > 0 {
		_ = usage.RecordEvent(ctx, u.TenantSchema, "fin_transaction_created", saved, nil)
	}
	_ = appauth.RedisClient().Del(ctx, transactionImageStagingKey(jobId)).Err()

	msg := fmt.Sprintf("%d transaksi disimpan", saved)
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d dilewati)", skipped)
	}
	return &CommitTransactionImageResponse{
		JobID:        jobId,
		SavedCount:   saved,
		SkippedCount: skipped,
		Message:      msg,
	}, nil
}

func previewTransactionsFromMultipart(
	ctx context.Context,
	user *types.AuthUser,
	files []*multipart.FileHeader,
) (*TransactionImagePreviewResponse, error) {
	if len(files) == 0 {
		return nil, appErrs.BadRequest("pilih minimal satu file gambar")
	}
	if len(files) > txnImageMaxFilesPerBatch {
		return nil, appErrs.BadRequest(fmt.Sprintf("maksimal %d gambar per proses", txnImageMaxFilesPerBatch))
	}
	var totalSize int64
	for _, fh := range files {
		totalSize += fh.Size
	}
	if totalSize > txnImageMaxBatchBytes {
		return nil, appErrs.BadRequest(fmt.Sprintf("total ukuran semua gambar maksimal %d MB", txnImageMaxBatchBytes/(1<<20)))
	}
	if _, _, err := ensureTxnImageAIQuota(ctx, user.TenantSchema); err != nil {
		return nil, err
	}

	var (
		allItems    []TransactionImageDraftItem
		seen        = make(map[string]struct{})
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
		res, err := processTransactionImageFile(ctx, user, fh)
		if err != nil {
			warnings = append(warnings, txnImageErrMessage(err))
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
				Purpose:      usage.PurposeTransactionImport,
				Path:         "transaction_image_preview",
				Reason:       "vision_extract",
				Model:        "claude-haiku-4-5-20251001",
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
		allItems = mergeTransactionDraftItems(allItems, seen, res.items)
		if len(allItems) >= txnImageMaxItems {
			warnings = append(warnings, fmt.Sprintf("batas %d transaksi per job tercapai", txnImageMaxItems))
			break
		}
	}

	if processed == 0 {
		msg := "tidak ada gambar yang berhasil diproses"
		if len(warnings) > 0 {
			msg = strings.Join(warnings, "; ")
		}
		return nil, appErrs.BadRequest(msg)
	}
	if len(allItems) == 0 {
		msg := "tidak ada transaksi terdeteksi dari gambar"
		if len(warnings) > 0 {
			msg += " (" + strings.Join(warnings, "; ") + ")"
		}
		return nil, appErrs.BadRequest(msg)
	}

	_, rem, lim := usage.CheckQuota(ctx, user.TenantSchema, "ai_token")
	jobID := fmt.Sprintf("tximg_%d", time.Now().UnixNano())
	staging := transactionImageStaging{
		TenantSchema:    user.TenantSchema,
		UploadedBy:      user.AccountID,
		Items:           allItems,
		SourceFilenames: sourceNames,
		ImagesProcessed: processed,
		InputTokens:     inTok,
		OutputTokens:    outTok,
		Model:           "claude-haiku-4-5-20251001",
	}
	raw, _ := json.Marshal(staging)
	if err := appauth.RedisClient().Set(ctx, transactionImageStagingKey(jobID), raw, txnImageStagingTTL).Err(); err != nil {
		return nil, appErrs.Internal("failed to stage transaction import")
	}

	return &TransactionImagePreviewResponse{
		JobID:               jobID,
		Items:               allItems,
		SourceFilenames:     sourceNames,
		ImagesProcessed:     processed,
		Warnings:            warnings,
		InputTokens:         inTok,
		OutputTokens:        outTok,
		TokensUsed:          inTok + outTok,
		TokenQuotaRemaining: rem,
		TokenQuotaLimit:     lim,
		QuotaNotice:         txnImageQuotaNotice(rem, lim),
	}, nil
}

type txnImageFileResult struct {
	items        []TransactionImageDraftItem
	inputTokens  int
	outputTokens int
	warning      string
}

func processTransactionImageFile(ctx context.Context, user *types.AuthUser, fh *multipart.FileHeader) (txnImageFileResult, error) {
	var out txnImageFileResult
	imageBytes, mediaType, err := readTxnImageBytes(fh)
	if err != nil {
		return out, err
	}
	rawJSON, compUsage, err := aivision.ExtractTransactionsFromScreenshot(ctx, secrets.AnthropicAPIKey, imageBytes, mediaType)
	if err != nil {
		rlog.Warn("transaction image vision failed", "err", err, "file", fh.Filename, "tenant", user.TenantSchema)
		return out, fmt.Errorf("AI gagal membaca %s", fh.Filename)
	}
	items, err := parseTransactionVisionJSON(rawJSON)
	if err != nil {
		return out, fmt.Errorf("hasil AI tidak valid untuk %s: %w", fh.Filename, err)
	}
	if len(items) == 0 {
		out.warning = fmt.Sprintf("%s: tidak ada transaksi terdeteksi", fh.Filename)
		return out, nil
	}
	out.items = items
	out.inputTokens = compUsage.InputTokens
	out.outputTokens = compUsage.OutputTokens
	return out, nil
}

func mergeTransactionDraftItems(
	existing []TransactionImageDraftItem,
	seen map[string]struct{},
	incoming []TransactionImageDraftItem,
) []TransactionImageDraftItem {
	for _, it := range incoming {
		if len(existing) >= txnImageMaxItems {
			break
		}
		key := txnDraftDedupeKey(it)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		existing = append(existing, it)
	}
	return existing
}

func txnDraftDedupeKey(it TransactionImageDraftItem) string {
	return fmt.Sprintf("%s|%.2f|%s|%s", it.Type, it.Amount, it.TransactionDate, strings.ToLower(strings.TrimSpace(it.Description)))
}

func loadTransactionImageStaging(ctx context.Context, jobID, tenantSchema string) (*transactionImageStaging, error) {
	raw, err := appauth.RedisClient().Get(ctx, transactionImageStagingKey(jobID)).Bytes()
	if err != nil {
		return nil, appErrs.NotFound("draft import tidak ditemukan atau sudah kedaluwarsa")
	}
	var st transactionImageStaging
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, appErrs.Internal("invalid staging payload")
	}
	if st.TenantSchema != tenantSchema {
		return nil, appErrs.Forbidden("draft import tidak valid untuk tenant ini")
	}
	return &st, nil
}

func parseTransactionVisionJSON(raw string) ([]TransactionImageDraftItem, error) {
	extracts, err := decodeTransactionVisionPayloads(raw)
	if err != nil {
		return nil, err
	}
	var out []TransactionImageDraftItem
	for _, ext := range extracts {
		for _, row := range ext.Items {
			txType := normalizeTxnImageType(row.Type)
			amt := row.Amount
			if amt < 0 {
				amt = -amt
				if txType == "" {
					txType = "expense"
				}
			}
			if amt <= 0 {
				continue
			}
			if txType == "" {
				txType = inferTxnTypeFromSignals(row.TypeSignals)
			}
			if txType == "" {
				continue
			}
			desc := strings.TrimSpace(row.Description)
			if desc == "" {
				continue
			}
			out = append(out, TransactionImageDraftItem{
				DraftKey:         uuid.New().String(),
				Type:             txType,
				TypeSignals:      row.TypeSignals,
				Amount:           amt,
				Description:      desc,
				TransactionDate:  normalizeTxnDate(row.TransactionDate),
				WalletNameHint:   strings.TrimSpace(row.WalletNameHint),
				CategoryNameHint: strings.TrimSpace(row.CategoryNameHint),
				Include:          true,
			})
			if len(out) >= txnImageMaxItems {
				return out, nil
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tidak ada baris transaksi dalam JSON AI")
	}
	return out, nil
}

func decodeTransactionVisionPayloads(raw string) ([]transactionVisionExtract, error) {
	raw = strings.TrimSpace(aivision.SanitizeVisionJSON(raw))
	if raw == "" {
		return nil, fmt.Errorf("JSON kosong")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	var out []transactionVisionExtract
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
			var arr []transactionVisionExtract
			if err := json.Unmarshal(chunk, &arr); err != nil {
				return nil, fmt.Errorf("JSON tidak valid: %w", err)
			}
			out = append(out, arr...)
			continue
		}
		var ext transactionVisionExtract
		if err := json.Unmarshal(chunk, &ext); err != nil {
			return nil, fmt.Errorf("JSON tidak valid: %w", err)
		}
		out = append(out, ext)
	}
	return out, nil
}

func normalizeTxnImageType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "income", "pemasukan", "masuk", "credit", "cr":
		return "income"
	case "expense", "pengeluaran", "keluar", "debit", "db":
		return "expense"
	default:
		return ""
	}
}

func inferTxnTypeFromSignals(signals []string) string {
	incomeScore, expenseScore := 0, 0
	for _, s := range signals {
		s = strings.ToLower(s)
		switch {
		case strings.Contains(s, "green"), strings.Contains(s, "plus"), strings.Contains(s, "income"), strings.Contains(s, "pemasukan"), strings.Contains(s, "masuk"):
			incomeScore++
		case strings.Contains(s, "red"), strings.Contains(s, "minus"), strings.Contains(s, "expense"), strings.Contains(s, "pengeluaran"), strings.Contains(s, "keluar"):
			expenseScore++
		}
	}
	if incomeScore > expenseScore {
		return "income"
	}
	if expenseScore > incomeScore {
		return "expense"
	}
	return ""
}

func normalizeTxnDate(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if t, err := time.Parse("2006-01-02", d); err == nil {
		return t.Format("2006-01-02")
	}
	return ""
}

func resolveWalletByNameHint(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, hint string) (string, bool) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return "", false
	}
	var id string
	err := qrow(ctx, sch, q, `
		SELECT id::text FROM fin_wallet
		WHERE deleted_at IS NULL AND is_active = true AND name ILIKE $1
		ORDER BY display_order, created_at LIMIT 1`, "%"+hint+"%").Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}

func resolveCategoryByHint(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, hint, txnType string) (string, bool) {
	hint = strings.TrimSpace(hint)
	kind := "expense"
	if txnType == "income" {
		kind = "income"
	}
	if hint != "" {
		var id string
		err := qrow(ctx, sch, q, `
			SELECT id::text FROM fin_category
			WHERE deleted_at IS NULL
			  AND (type = $1 OR type = 'any')
			  AND name ILIKE $2
			ORDER BY display_order, created_at LIMIT 1`, kind, "%"+hint+"%").Scan(&id)
		if err == nil {
			return id, true
		}
	}
	var id string
	err := qrow(ctx, sch, q, `
		SELECT id::text FROM fin_category
		WHERE deleted_at IS NULL AND type = $1
		ORDER BY display_order, created_at LIMIT 1`, kind).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, true
}

func readTxnImageBytes(fh *multipart.FileHeader) ([]byte, string, error) {
	if fh == nil {
		return nil, "", appErrs.BadRequest("file is required")
	}
	if fh.Size > txnImageMaxBytes {
		return nil, "", appErrs.BadRequest(fmt.Sprintf("ukuran gambar maksimal %d MB per file", txnImageMaxBytes/(1<<20)))
	}
	if fh.Size > 0 && fh.Size < txnImageMinBytes {
		return nil, "", appErrs.BadRequest("file gambar terlalu kecil atau rusak")
	}
	ext := txnImageExt(fh)
	if ext == "" {
		return nil, "", appErrs.BadRequest("format gambar: JPG, PNG, atau WEBP")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, "", appErrs.BadRequest("cannot open uploaded file")
	}
	defer f.Close()
	imageBytes, err := io.ReadAll(io.LimitReader(f, txnImageMaxBytes+1))
	if err != nil {
		return nil, "", appErrs.Internal("read image failed")
	}
	if len(imageBytes) > int(txnImageMaxBytes) {
		return nil, "", appErrs.BadRequest(fmt.Sprintf("ukuran gambar maksimal %d MB per file", txnImageMaxBytes/(1<<20)))
	}
	if len(imageBytes) < txnImageMinBytes {
		return nil, "", appErrs.BadRequest("file gambar terlalu kecil atau rusak")
	}
	return imageBytes, txnImageMimeFromExt(ext), nil
}

func txnImageExt(fh *multipart.FileHeader) string {
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

func txnImageMimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
