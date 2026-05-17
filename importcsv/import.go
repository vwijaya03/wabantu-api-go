package importcsv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/shared/types"

	"github.com/xuri/excelize/v2"
)

// ---------- Pub/Sub ----------

type ImportRequest struct {
	TenantSchema  string            `json:"tenantSchema"`
	TargetTable   string            `json:"targetTable"` // "business_catalog_item" | "knowledge_base_entry"
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
	Headers    []string            `json:"headers"`
	SampleRows [][]string          `json:"sampleRows"`
	Suggestions map[string]string  `json:"suggestions"`
	TotalRows  int                 `json:"totalRows"`
}

type ExecuteRequest struct {
	TargetTable   string            `json:"targetTable"`
	ColumnMapping map[string]string `json:"columnMapping"`
}

type ExecuteResponse struct {
	Message string `json:"message"`
	JobID   string `json:"jobId"`
}

type ImportResult struct {
	TotalRows    int           `json:"totalRows"`
	SuccessCount int           `json:"successCount"`
	FailedCount  int           `json:"failedCount"`
	Errors       []RowError    `json:"errors,omitempty"`
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

// Preview parses an uploaded file and returns headers + sample rows.
//
//encore:api auth method=POST path=/import/preview tag:owner
func Preview(ctx context.Context, file *multipart.FileHeader) (*PreviewResponse, error) {
	u, _ := auth.UserID()
	rlog.Info("import preview", "user", u)

	target := "business_catalog_item"

	f, err := file.Open()
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "cannot open uploaded file"}
	}
	defer f.Close()

	headers, rows, err := parseFile(f, file.Filename)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("parse error: %s", err)}
	}

	sample := rows
	if len(sample) > 5 {
		sample = sample[:5]
	}

	suggestions := suggestMapping(headers, target)

	return &PreviewResponse{
		Headers:     headers,
		SampleRows:  sample,
		Suggestions: suggestions,
		TotalRows:   len(rows),
	}, nil
}

// Execute validates mapping and publishes import job to the queue.
//
//encore:api auth method=POST path=/import/execute tag:owner
func Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	uid, _ := auth.UserID()
	userData := auth.Data().(*types.AuthUser)

	valid := validColumns(req.TargetTable)
	if valid == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("unsupported target: %s", req.TargetTable)}
	}
	for _, col := range req.ColumnMapping {
		if col == "" || col == "-" {
			continue
		}
		if !valid[col] {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("invalid column: %s", col)}
		}
	}

	msgID, err := ImportTopic.Publish(ctx, ImportRequest{
		TenantSchema:  userData.TenantSchema,
		TargetTable:   req.TargetTable,
		ColumnMapping: req.ColumnMapping,
		UploadedBy:    string(uid),
	})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to queue import"}
	}

	rlog.Info("import queued", "user", uid, "target", req.TargetTable, "msgID", msgID)
	return &ExecuteResponse{
		Message: "Import job queued",
		JobID:   msgID,
	}, nil
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
		err := processRow(ctx, req.TenantSchema, req.TargetTable, req.ColumnMapping, row, i)
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
	return nil
}

func processRow(ctx context.Context, schema, target string, mapping map[string]string, row []string, _ int) error {
	// Build column→value map from the row
	_ = schema
	_ = target

	mapped := make(map[string]string)
	i := 0
	for header, col := range mapping {
		if col == "" || col == "-" {
			continue
		}
		_ = header
		if i < len(row) {
			mapped[col] = row[i]
		}
		i++
	}

	// Placeholder: actual DB insert would go here, dispatched by target table.
	// INSERT INTO {schema}.{target} (col1, col2, ...) VALUES ($1, $2, ...)
	rlog.Debug("import row", "mapped", mapped)
	return nil
}
