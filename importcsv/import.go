package importcsv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/pubsub"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/business"
	"encore.app/wabantu/kb"
	"encore.app/wabantu/shared/types"
)

var tenantDB = sqldb.Named("tenant")

// ---------- Pub/Sub ----------

type ImportRequest struct {
	JobID         string            `json:"jobId"`
	TenantSchema  string            `json:"tenantSchema"`
	TenantID      string            `json:"tenantId"`
	TargetTable   string            `json:"targetTable"` // "business_catalog_item" (legacy queued: "knowledge_base_entry")
	Headers       []string          `json:"headers"`
	ColumnMapping map[string]string `json:"columnMapping"`
	Rows          [][]string        `json:"rows"`
	UploadedBy    string            `json:"uploadedBy"`
}

var ImportTopic = pubsub.NewTopic[ImportRequest]("file-import", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(ImportTopic, "import-processor", pubsub.SubscriptionConfig[ImportRequest]{
	Handler:     handleImport,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// ---------- Types ----------

type PreviewRequest struct {
	TargetTable string `json:"targetTable"` // "business_catalog_item" only
}

type PreviewResponse struct {
	JobID       string            `json:"jobId"`
	TargetTable string            `json:"targetTable"`
	Headers     []string          `json:"headers"`
	SampleRows  [][]string        `json:"sampleRows"`
	Suggestions map[string]string `json:"suggestions"`
	TotalRows   int               `json:"totalRows"`
}

type importStagingPayload struct {
	TenantSchema  string            `json:"tenantSchema"`
	TenantID      string            `json:"tenantId"`
	TargetTable   string            `json:"targetTable"`
	Headers       []string          `json:"headers"`
	Rows          [][]string        `json:"rows"`
	UploadedBy    string            `json:"uploadedBy"`
	ColumnMapping map[string]string `json:"columnMapping,omitempty"`
}

type ExecuteRequest struct {
	JobID         string            `json:"jobId"`
	TargetTable   string            `json:"targetTable"`
	ColumnMapping map[string]string `json:"columnMapping"`
}

type ExecuteResponse struct {
	Message string `json:"message"`
	JobID   string `json:"jobId"`
}

type ImportResult struct {
	TotalRows    int        `json:"totalRows"`
	SuccessCount int        `json:"successCount"`
	FailedCount  int        `json:"failedCount"`
	Errors       []RowError `json:"errors,omitempty"`
}

type RowError struct {
	Row     int    `json:"row"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

// ---------- Target table schemas ----------

var catalogColumns = map[string]bool{
	"external_code": true, "name": true, "description": true,
	"sell_price": true, "sell_unit": true, "is_active": true, "barcode": true,
}

// validColumns is the allowlist of import targets (preview suggestions and
// execute). Only business_catalog_item is importable from a file (CTO decision
// 2026-10-24); knowledge_base_entry was removed. processRow keeps its KB case
// only so already-queued file-import messages can still drain.
func validColumns(target string) map[string]bool {
	switch target {
	case "business_catalog_item":
		return catalogColumns
	default:
		return nil
	}
}

// ---------- API endpoints ----------

const importStagingTTL = 24 * time.Hour
const maxImportRows = 50_000

func importStagingKey(jobID string) string { return "import:staging:" + jobID }

// PreviewHTTP parses an uploaded CSV/XLSX, stores full rows in Redis (staging),
// and returns jobId + sample.
//
// Must be a raw endpoint: a typed Encore API marshals its request parameter
// across the API boundary, and *multipart.FileHeader only carries the uploaded
// bytes in unexported fields, so a typed `file *multipart.FileHeader` param can
// never be opened (always 400 "cannot open uploaded file"). Multipart fields:
// "file" (required) and "targetTable" (optional, default business_catalog_item);
// both names are relied on by FE lib/api/import.ts. Limits: see preview_upload.go.
//
//encore:api auth raw method=POST path=/api/v1/import/preview tag:owner
func PreviewHTTP(w http.ResponseWriter, req *http.Request) {
	servePreview(w, req, defaultPreviewDeps())
}

// ImportJobStatus returns import results stored after the worker finishes.
// Owner only (staff -> 403); only the tenant that ran the import can read it (else 404).
//
//encore:api auth method=GET path=/api/v1/import/status/:jobId tag:owner
func ImportJobStatus(ctx context.Context, jobId string) (*ImportResult, error) {
	user, _ := auth.Data().(*types.AuthUser)
	return importJobStatus(ctx, user, jobId, redisGetBytes)
}

func importJobStatus(ctx context.Context, user *types.AuthUser, jobID string, get func(ctx context.Context, key string) ([]byte, error)) (*ImportResult, error) {
	if err := requireImportOwner(user); err != nil {
		return nil, err
	}
	if jobID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "jobId required"}
	}
	raw, err := get(ctx, importResultKey(jobID))
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: msgImportJobNotFound}
	}
	return decodeImportResultForTenant(raw, user.TenantID)
}

// requireImportOwner mirrors the preview / catalog_image_http check: only
// owners (or an impersonating super_admin) may use the import endpoints.
func requireImportOwner(user *types.AuthUser) error {
	if !user.CanPerformOwnerActions() {
		return &errs.Error{Code: errs.PermissionDenied, Message: "forbidden"}
	}
	return nil
}

func redisGetBytes(ctx context.Context, key string) ([]byte, error) {
	return appauth.RedisClient().Get(ctx, key).Bytes()
}

const msgTargetMismatch = "targetTable does not match preview"

type executeDeps struct {
	get     func(ctx context.Context, key string) ([]byte, error)
	publish func(ctx context.Context, msg ImportRequest) error
	del     func(ctx context.Context, key string) error
}

func defaultExecuteDeps() executeDeps {
	return executeDeps{
		get: redisGetBytes,
		publish: func(ctx context.Context, msg ImportRequest) error {
			_, err := ImportTopic.Publish(ctx, msg)
			return err
		},
		del: func(ctx context.Context, key string) error {
			return appauth.RedisClient().Del(ctx, key).Err()
		},
	}
}

// Execute validates mapping and publishes import job to the queue.
// Owner only (staff -> 403). targetTable must equal the staged (preview)
// target and must be an allowed import target (business_catalog_item only).
//
//encore:api auth method=POST path=/api/v1/import/execute tag:owner
func Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	user, _ := auth.Data().(*types.AuthUser)
	return executeImport(ctx, user, req, defaultExecuteDeps())
}

func executeImport(ctx context.Context, user *types.AuthUser, req *ExecuteRequest, deps executeDeps) (*ExecuteResponse, error) {
	if err := requireImportOwner(user); err != nil {
		return nil, err
	}
	if req == nil || req.JobID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "jobId required (from preview)"}
	}

	staged, err := loadStaging(ctx, deps.get, req.JobID, user.TenantSchema, user.TenantID)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.TargetTable)
	if target == "" {
		target = staged.TargetTable
	}
	if target != staged.TargetTable {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: msgTargetMismatch}
	}
	valid := validColumns(target)
	if valid == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("unsupported target: %s", target)}
	}
	for _, col := range req.ColumnMapping {
		if col == "" || col == "-" {
			continue
		}
		if !valid[col] {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("invalid column: %s", col)}
		}
	}

	jobID := req.JobID
	err = deps.publish(ctx, ImportRequest{
		JobID:         jobID,
		TenantSchema:  user.TenantSchema,
		TenantID:      user.TenantID,
		TargetTable:   target,
		Headers:       staged.Headers,
		ColumnMapping: req.ColumnMapping,
		Rows:          staged.Rows,
		UploadedBy:    user.AccountID,
	})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to queue import"}
	}

	_ = deps.del(ctx, importStagingKey(jobID))

	rlog.Info("import queued", "user", user.AccountID, "target", target, "jobId", jobID)
	return &ExecuteResponse{
		Message: "Import job queued",
		JobID:   jobID,
	}, nil
}

func loadStaging(ctx context.Context, get func(ctx context.Context, key string) ([]byte, error), jobID, tenantSchema, tenantID string) (*importStagingPayload, error) {
	raw, err := get(ctx, importStagingKey(jobID))
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: msgImportStagingNotFound}
	}
	var p importStagingPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid staged import"}
	}
	if err := checkStagingTenant(&p, tenantSchema, tenantID); err != nil {
		return nil, err
	}
	if len(p.Headers) == 0 || len(p.Rows) == 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "staged import is empty"}
	}
	return &p, nil
}

// ---------- Column suggestion ----------

// columnAliases lists the header spellings (already normalised, see
// normalizeHeader) that suggest a target column. Order matters: it is the
// fixed iteration order and the final tie-break, so suggestions never depend
// on Go's random map order.
type columnAliases struct {
	column  string
	aliases []string
}

var suggestionAliases = []columnAliases{
	{"external_code", []string{"external_code", "kode", "kode_produk", "kode_barang", "kode_item", "item_code", "product_code", "sku", "code"}},
	{"barcode", []string{"barcode", "kode_barcode", "ean", "upc", "gtin"}},
	{"name", []string{"name", "nama", "nama_produk", "nama_barang", "product_name", "product", "produk", "item"}},
	{"description", []string{"description", "deskripsi", "keterangan", "desc"}},
	{"sell_price", []string{"sell_price", "harga_jual", "harga", "price"}},
	{"sell_unit", []string{"sell_unit", "satuan", "unit", "uom"}},
	{"is_active", []string{"is_active", "aktif", "active", "status"}},
}

// normalizeHeader lowercases, trims and turns every run of non-alphanumeric
// characters into a single "_" ("Kode Produk" -> "kode_produk",
// "Harga (Rp)" -> "harga_rp").
func normalizeHeader(h string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(strings.TrimSpace(h)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			b.WriteRune(r)
			continue
		}
		sep = true
	}
	return b.String()
}

// exactColumn returns the column whose name or alias equals the normalised
// header, in suggestionAliases order. It looks at every known column, valid
// for the target or not, so the caller can leave a header unmapped instead of
// picking a weaker match (e.g. "barcode" when there is no barcode column).
func exactColumn(norm string) string {
	for _, ca := range suggestionAliases {
		for _, alias := range ca.aliases {
			if norm == alias {
				return ca.column
			}
		}
	}
	return ""
}

// partialColumn returns the column with the longest alias contained in the
// normalised header ("barcode_produk" -> barcode via "barcode", not
// external_code via "code"). Ties keep the first column/alias in
// suggestionAliases order (strict > below). Like exactColumn it considers
// every known column; validity is checked by the caller.
func partialColumn(norm string) string {
	best, bestLen := "", 0
	for _, ca := range suggestionAliases {
		for _, alias := range ca.aliases {
			if len(alias) > bestLen && strings.Contains(norm, alias) {
				best, bestLen = ca.column, len(alias)
			}
		}
	}
	return best
}

// suggestMapping proposes header -> column for the preview UI. Deterministic:
//  1. exact pass over headers in file order (normalised header == column
//     name or alias), then
//  2. partial pass for still-unmapped headers (longest contained alias).
//
// Each column is suggested for at most one header and each header gets at
// most one column; when two headers want the same column, the exact match
// wins over a partial one and otherwise the earlier header wins. A header
// whose best column is taken or not valid for the target stays unmapped (no
// fallback to a weaker match, so "barcode" can never fall back to
// external_code via "code").
func suggestMapping(headers []string, target string) map[string]string {
	suggestions := make(map[string]string)
	valid := validColumns(target)
	if valid == nil {
		return suggestions
	}
	return suggestMappingFor(headers, valid)
}

// suggestMappingFor is suggestMapping over an explicit set of valid columns.
func suggestMappingFor(headers []string, valid map[string]bool) map[string]string {
	suggestions := make(map[string]string)
	taken := make(map[string]bool)
	norms := make([]string, len(headers))
	for i, h := range headers {
		norms[i] = normalizeHeader(h)
	}
	for _, pick := range []func(string) string{exactColumn, partialColumn} {
		for i, h := range headers {
			if _, done := suggestions[h]; done || norms[i] == "" {
				continue
			}
			col := pick(norms[i])
			if col == "" || !valid[col] || taken[col] {
				continue
			}
			suggestions[h] = col
			taken[col] = true
		}
	}
	return suggestions
}

// ---------- Import handler ----------

func handleImport(ctx context.Context, req ImportRequest) error {
	rlog.Info("processing import", "schema", req.TenantSchema, "target", req.TargetTable, "rows", len(req.Rows))

	result := ImportResult{TotalRows: len(req.Rows)}

	for i, row := range req.Rows {
		err := processRow(ctx, req.TenantSchema, req.TenantID, req.TargetTable, req.Headers, req.ColumnMapping, row, i)
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, RowError{
				Row:     i + 2, // 1-indexed, skip header
				Message: err.Error(),
			})
			continue
		}
		result.SuccessCount++
	}

	rlog.Info("import complete",
		"schema", req.TenantSchema,
		"target", req.TargetTable,
		"total", result.TotalRows,
		"success", result.SuccessCount,
		"failed", result.FailedCount,
	)
	if req.JobID != "" {
		b, _ := encodeImportResult(req.TenantID, result)
		_ = appauth.RedisClient().Set(ctx, importResultKey(req.JobID), b, 24*time.Hour).Err()
	}
	return nil
}

func importResultKey(jobID string) string {
	return "import:result:" + jobID
}

func processRow(ctx context.Context, schema, tenantID, target string, headers []string, mapping map[string]string, row []string, _ int) error {
	vals := make(map[string]string)
	for i, header := range headers {
		col := mapping[header]
		if col == "" || col == "-" {
			continue
		}
		if i < len(row) {
			vals[col] = strings.TrimSpace(row[i])
		}
	}

	switch target {
	case "business_catalog_item":
		code := vals["external_code"]
		name := vals["name"]
		if code == "" || name == "" {
			return fmt.Errorf("external_code and name required")
		}
		var price *float64
		if p := vals["sell_price"]; p != "" {
			f, err := business.ParsePrice(p)
			if err != nil {
				return fmt.Errorf("invalid sell_price: %w", err)
			}
			price = &f
		}
		active := true
		if v := strings.ToLower(vals["is_active"]); v == "0" || v == "false" || v == "tidak" || v == "no" {
			active = false
		}
		var desc, unit, barcode *string
		if v := vals["description"]; v != "" {
			desc = &v
		}
		if v := vals["sell_unit"]; v != "" {
			unit = &v
		}
		if v := vals["barcode"]; v != "" {
			barcode = &v
		}
		_, err := tenantDB.Exec(ctx, fmt.Sprintf(`
			INSERT INTO "%s".business_catalog_item
				(external_code, name, description, sell_price, sell_unit, is_active, barcode, source)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'import')
			ON CONFLICT (source, external_code) WHERE deleted_at IS NULL DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				sell_price = EXCLUDED.sell_price,
				sell_unit = EXCLUDED.sell_unit,
				is_active = EXCLUDED.is_active,
				barcode = EXCLUDED.barcode,
				updated_at = NOW()`, schema),
			code, name, desc, price, unit, active, barcode)
		if err != nil {
			return err
		}
		return business.IndexImportedCatalog(ctx, schema, tenantID, "import", code)

	case "knowledge_base_entry":
		q := vals["question"]
		a := vals["answer"]
		if q == "" || a == "" {
			return fmt.Errorf("question and answer required")
		}
		var cat *string
		if v := vals["category"]; v != "" {
			cat = &v
		}
		_, err := kb.InsertKBEntryWithIndex(ctx, schema, tenantID, q, a, cat, "import", true)
		return err

	default:
		return fmt.Errorf("unsupported target: %s", target)
	}
}
