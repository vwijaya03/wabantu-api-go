package importcsv

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/pubsub"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/business"
	"encore.app/wabantu/shared/types"

	"github.com/xuri/excelize/v2"
)

var tenantDB = sqldb.Named("tenant")

// ---------- Pub/Sub ----------

type ImportRequest struct {
	JobID         string            `json:"jobId"`
	TenantSchema  string            `json:"tenantSchema"`
	TargetTable   string            `json:"targetTable"` // "business_catalog_item" | "knowledge_base_entry"
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
	TargetTable string `json:"targetTable"` // "business_catalog_item" | "knowledge_base_entry"
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

var kbColumns = map[string]bool{
	"question": true, "answer": true, "category": true,
}

func validColumns(target string) map[string]bool {
	switch target {
	case "business_catalog_item":
		return catalogColumns
	case "knowledge_base_entry":
		return kbColumns
	default:
		return nil
	}
}

// ---------- API endpoints ----------

const importStagingTTL = 24 * time.Hour
const maxImportRows = 50_000

func importStagingKey(jobID string) string { return "import:staging:" + jobID }

// Preview parses an uploaded file, stores full rows in Redis (staging), returns jobId + sample.
//
//encore:api auth method=POST path=/api/v1/import/preview tag:owner
func Preview(ctx context.Context, file *multipart.FileHeader) (*PreviewResponse, error) {
	u, _ := auth.UserID()
	userData, _ := auth.Data().(*types.AuthUser)
	rlog.Info("import preview", "user", u)

	target := "business_catalog_item"
	if userData == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}

	f, err := file.Open()
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "cannot open uploaded file"}
	}
	defer f.Close()

	headers, rows, err := parseFile(f, file.Filename)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("parse error: %s", err)}
	}
	if len(rows) > maxImportRows {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("max %d rows per import", maxImportRows)}
	}

	jobID := fmt.Sprintf("imp_%d", time.Now().UnixNano())
	payload := importStagingPayload{
		TenantSchema: userData.TenantSchema,
		TargetTable:  target,
		Headers:      headers,
		Rows:         rows,
		UploadedBy:   string(u),
	}
	raw, _ := json.Marshal(payload)
	if err := appauth.RedisClient().Set(ctx, importStagingKey(jobID), raw, importStagingTTL).Err(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to stage import file"}
	}

	sample := rows
	if len(sample) > 5 {
		sample = sample[:5]
	}

	suggestions := suggestMapping(headers, target)

	return &PreviewResponse{
		JobID:       jobID,
		TargetTable: target,
		Headers:     headers,
		SampleRows:  sample,
		Suggestions: suggestions,
		TotalRows:   len(rows),
	}, nil
}

// ImportJobStatus returns import results stored after the worker finishes.
//
//encore:api auth method=GET path=/api/v1/import/status/:jobId tag:owner
func ImportJobStatus(ctx context.Context, jobId string) (*ImportResult, error) {
	if jobId == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "jobId required"}
	}
	raw, err := appauth.RedisClient().Get(ctx, importResultKey(jobId)).Bytes()
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "import job not found or still processing"}
	}
	var result ImportResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid import result"}
	}
	return &result, nil
}

// Execute validates mapping and publishes import job to the queue.
//
//encore:api auth method=POST path=/api/v1/import/execute tag:owner
func Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	uid, _ := auth.UserID()
	userData := auth.Data().(*types.AuthUser)

	if req.JobID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "jobId required (from preview)"}
	}

	staged, err := loadStaging(ctx, req.JobID, userData.TenantSchema)
	if err != nil {
		return nil, err
	}
	target := req.TargetTable
	if target == "" {
		target = staged.TargetTable
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
	_, err = ImportTopic.Publish(ctx, ImportRequest{
		JobID:         jobID,
		TenantSchema:  userData.TenantSchema,
		TargetTable:   target,
		Headers:       staged.Headers,
		ColumnMapping: req.ColumnMapping,
		Rows:          staged.Rows,
		UploadedBy:    string(uid),
	})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to queue import"}
	}

	_ = appauth.RedisClient().Del(ctx, importStagingKey(jobID)).Err()

	rlog.Info("import queued", "user", uid, "target", target, "jobId", jobID)
	return &ExecuteResponse{
		Message: "Import job queued",
		JobID:   jobID,
	}, nil
}

func loadStaging(ctx context.Context, jobID, tenantSchema string) (*importStagingPayload, error) {
	raw, err := appauth.RedisClient().Get(ctx, importStagingKey(jobID)).Bytes()
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "import preview expired or not found — upload again"}
	}
	var p importStagingPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid staged import"}
	}
	if p.TenantSchema != tenantSchema {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "import job belongs to another tenant"}
	}
	if len(p.Headers) == 0 || len(p.Rows) == 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "staged import is empty"}
	}
	return &p, nil
}

// ---------- File parsing ----------

func parseFile(r io.Reader, filename string) ([]string, [][]string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".csv":
		return parseCSV(r)
	case ".xlsx":
		return parseXLSX(r)
	default:
		return nil, nil, fmt.Errorf("unsupported file type: %s (use .csv or .xlsx)", ext)
	}
}

func parseCSV(r io.Reader) ([]string, [][]string, error) {
	reader := csv.NewReader(r)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("CSV parse: %w", err)
	}
	if len(records) < 2 {
		return nil, nil, fmt.Errorf("file must have a header row + at least one data row")
	}
	return records[0], records[1:], nil
}

func parseXLSX(r io.Reader) ([]string, [][]string, error) {
	rc, ok := r.(io.ReadCloser)
	if !ok {
		rc = io.NopCloser(r)
	}
	defer rc.Close()

	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("XLSX parse: %w", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	if sheet == "" {
		return nil, nil, fmt.Errorf("no sheets found in XLSX")
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, nil, fmt.Errorf("read sheet: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("file must have a header row + at least one data row")
	}
	return rows[0], rows[1:], nil
}

// ---------- Column suggestion ----------

func suggestMapping(headers []string, target string) map[string]string {
	suggestions := make(map[string]string)
	valid := validColumns(target)
	if valid == nil {
		return suggestions
	}

	aliases := map[string][]string{
		"external_code": {"kode", "sku", "code", "external_code", "item_code"},
		"name":          {"nama", "name", "product", "produk", "item"},
		"description":   {"deskripsi", "description", "desc", "keterangan"},
		"sell_price":    {"harga", "price", "sell_price", "harga_jual"},
		"sell_unit":     {"satuan", "unit", "sell_unit", "uom"},
		"is_active":     {"aktif", "active", "is_active", "status"},
		"barcode":       {"barcode", "kode_barcode"},
		"question":      {"pertanyaan", "question", "q", "tanya"},
		"answer":        {"jawaban", "answer", "a", "jawab"},
		"category":      {"kategori", "category", "cat"},
	}

	for _, h := range headers {
		hl := strings.ToLower(strings.TrimSpace(h))
		for col, alts := range aliases {
			if !valid[col] {
				continue
			}
			for _, alt := range alts {
				if hl == alt || strings.Contains(hl, alt) {
					suggestions[h] = col
					break
				}
			}
			if _, ok := suggestions[h]; ok {
				break
			}
		}
	}
	return suggestions
}

// ---------- Import handler ----------

func handleImport(ctx context.Context, req ImportRequest) error {
	rlog.Info("processing import", "schema", req.TenantSchema, "target", req.TargetTable, "rows", len(req.Rows))

	result := ImportResult{TotalRows: len(req.Rows)}

	for i, row := range req.Rows {
		err := processRow(ctx, req.TenantSchema, req.TargetTable, req.Headers, req.ColumnMapping, row, i)
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
		b, _ := json.Marshal(result)
		_ = appauth.RedisClient().Set(ctx, importResultKey(req.JobID), b, 24*time.Hour).Err()
	}
	return nil
}

func importResultKey(jobID string) string {
	return "import:result:" + jobID
}

func processRow(ctx context.Context, schema, target string, headers []string, mapping map[string]string, row []string, _ int) error {
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
			ON CONFLICT (source, external_code) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				sell_price = EXCLUDED.sell_price,
				sell_unit = EXCLUDED.sell_unit,
				is_active = EXCLUDED.is_active,
				barcode = EXCLUDED.barcode,
				updated_at = NOW()`, schema),
			code, name, desc, price, unit, active, barcode)
		return err

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
		_, err := tenantDB.Exec(ctx, fmt.Sprintf(`
			INSERT INTO "%s".knowledge_base_entry (question, answer, category, source)
			VALUES ($1,$2,$3,'import')`, schema), q, a, cat)
		return err

	default:
		return fmt.Errorf("unsupported target: %s", target)
	}
}
