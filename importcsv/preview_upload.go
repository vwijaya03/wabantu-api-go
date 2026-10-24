package importcsv

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/shared/types"

	"github.com/xuri/excelize/v2"
)

// Upload limits for POST /api/v1/import/preview.
const (
	// PreviewMaxBodyBytes is the hard cap on the whole multipart request body,
	// enforced with http.MaxBytesReader (and a Content-Length fast path).
	// Anything larger is rejected with 413. 10 MiB comfortably holds a
	// 50,000-row catalog CSV/XLSX.
	PreviewMaxBodyBytes int64 = 10 << 20
	// previewMaxMemoryBytes is the in-memory budget for ParseMultipartForm;
	// parts beyond it spill to a temp file (still bounded by PreviewMaxBodyBytes)
	// and are removed after the request.
	previewMaxMemoryBytes int64 = 8 << 20
	// xlsxUnzipSizeLimit caps the total declared uncompressed size of all
	// entries in an XLSX (zip bomb guard; archive/zip also rejects entries that
	// inflate beyond their declared size).
	xlsxUnzipSizeLimit int64 = 64 << 20
	// xlsxUnzipXMLSizeLimit caps the per-worksheet / shared-strings XML kept in
	// memory; must be <= xlsxUnzipSizeLimit.
	xlsxUnzipXMLSizeLimit int64 = 16 << 20

	previewFileField   = "file"
	previewTargetField = "targetTable"
	defaultTarget      = "business_catalog_item"
	previewSampleRows  = 5
)

// Client-facing messages are generic; details go to rlog only.
const (
	msgTooLarge       = "file terlalu besar (maks 10 MB)"
	msgBadForm        = "gagal membaca form upload"
	msgFileRequired   = "field 'file' wajib diisi"
	msgBadTarget      = "targetTable tidak didukung"
	msgUnsupported    = "tipe file tidak didukung (gunakan .csv atau .xlsx)"
	msgCSVInvalid     = "file CSV tidak valid"
	msgXLSXInvalid    = "file XLSX tidak valid atau terlalu besar setelah diekstrak"
	msgNeedHeaderData = "file harus berisi baris header dan minimal satu baris data"
	msgStageFailed    = "gagal menyimpan file import, coba lagi"
)

var (
	errTooManyRows    = fmt.Errorf("max %d rows per import", maxImportRows)
	errNeedHeaderData = errors.New("file must have a header row + at least one data row")
	errNoSheet        = errors.New("no sheets found in XLSX")
)

type previewDeps struct {
	authenticate func(ctx context.Context, r *http.Request) (*types.AuthUser, error)
	stage        func(ctx context.Context, jobID string, payload []byte) error
	now          func() time.Time
}

func defaultPreviewDeps() previewDeps {
	return previewDeps{
		authenticate: appauth.AuthenticateHTTP,
		stage: func(ctx context.Context, jobID string, payload []byte) error {
			return appauth.RedisClient().Set(ctx, importStagingKey(jobID), payload, importStagingTTL).Err()
		},
		now: time.Now,
	}
}

func servePreview(w http.ResponseWriter, req *http.Request, deps previewDeps) {
	ctx := req.Context()

	user, err := deps.authenticate(ctx, req)
	if err != nil || user == nil {
		writePreviewError(w, http.StatusUnauthorized, "unauthenticated", "not authenticated")
		return
	}
	if !user.CanPerformOwnerActions() {
		writePreviewError(w, http.StatusForbidden, "permission_denied", "forbidden")
		return
	}

	if req.ContentLength > PreviewMaxBodyBytes {
		rlog.Warn("import preview rejected: body too large", "contentLength", req.ContentLength, "tenant", user.TenantSchema)
		writePreviewError(w, http.StatusRequestEntityTooLarge, "resource_exhausted", msgTooLarge)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, PreviewMaxBodyBytes)
	if err := req.ParseMultipartForm(previewMaxMemoryBytes); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			rlog.Warn("import preview rejected: body too large", "limit", mbe.Limit, "tenant", user.TenantSchema)
			writePreviewError(w, http.StatusRequestEntityTooLarge, "resource_exhausted", msgTooLarge)
			return
		}
		rlog.Warn("import preview: parse multipart failed", "err", err, "tenant", user.TenantSchema)
		writePreviewError(w, http.StatusBadRequest, "invalid_argument", msgBadForm)
		return
	}
	defer func() { _ = req.MultipartForm.RemoveAll() }()

	target := strings.TrimSpace(req.FormValue(previewTargetField))
	if target == "" {
		target = defaultTarget
	}
	// Preview only stages catalog imports (CTO decision 2026-10-24); KB and any
	// other target are rejected here. Execute is unchanged.
	if target != defaultTarget {
		writePreviewError(w, http.StatusBadRequest, "invalid_argument", msgBadTarget)
		return
	}

	files := req.MultipartForm.File[previewFileField]
	if len(files) == 0 {
		writePreviewError(w, http.StatusBadRequest, "invalid_argument", msgFileRequired)
		return
	}
	fh := files[0]

	headers, rows, status, msg := readUploadedTable(fh)
	if status != 0 {
		rlog.Warn("import preview rejected", "reason", msg, "file", fh.Filename, "size", fh.Size, "tenant", user.TenantSchema)
		writePreviewError(w, status, "invalid_argument", msg)
		return
	}

	jobID := fmt.Sprintf("imp_%d", deps.now().UnixNano())
	raw, err := json.Marshal(importStagingPayload{
		TenantSchema: user.TenantSchema,
		TargetTable:  target,
		Headers:      headers,
		Rows:         rows,
		UploadedBy:   user.AccountID,
	})
	if err != nil {
		rlog.Error("import preview: marshal staging failed", "err", err, "tenant", user.TenantSchema)
		writePreviewError(w, http.StatusInternalServerError, "internal", msgStageFailed)
		return
	}
	if err := deps.stage(ctx, jobID, raw); err != nil {
		rlog.Error("import preview: stage failed", "err", err, "tenant", user.TenantSchema, "jobId", jobID)
		writePreviewError(w, http.StatusInternalServerError, "internal", msgStageFailed)
		return
	}
	rlog.Info("import preview staged", "user", user.AccountID, "tenant", user.TenantSchema, "target", target, "rows", len(rows), "jobId", jobID)

	sample := rows
	if len(sample) > previewSampleRows {
		sample = sample[:previewSampleRows]
	}
	// Same JSON body the typed endpoint declared (bare PreviewResponse).
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(&PreviewResponse{
		JobID:       jobID,
		TargetTable: target,
		Headers:     headers,
		SampleRows:  sample,
		Suggestions: suggestMapping(headers, target),
		TotalRows:   len(rows),
	})
}

// writePreviewError writes Encore's standard error body ({code,message,details})
// so clients see the same error shape the typed endpoint produced.
func writePreviewError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "details": nil})
}

// readUploadedTable validates extension + content and parses the table.
// It returns a non-zero HTTP status and a client-safe message on rejection.
func readUploadedTable(fh *multipart.FileHeader) ([]string, [][]string, int, string) {
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if ext != ".csv" && ext != ".xlsx" {
		return nil, nil, http.StatusBadRequest, msgUnsupported
	}
	f, err := fh.Open()
	if err != nil {
		rlog.Error("import preview: open part failed", "err", err)
		return nil, nil, http.StatusBadRequest, msgBadForm
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		rlog.Error("import preview: read part failed", "err", err)
		return nil, nil, http.StatusBadRequest, msgBadForm
	}
	head = head[:n]
	if !sniffMatches(ext, head) {
		return nil, nil, http.StatusBadRequest, msgUnsupported
	}
	body := io.MultiReader(bytes.NewReader(head), f)

	var headers []string
	var rows [][]string
	if ext == ".csv" {
		headers, rows, err = parseCSV(body)
	} else {
		headers, rows, err = parseXLSX(body)
	}
	switch {
	case err == nil:
		return headers, rows, 0, ""
	case errors.Is(err, errTooManyRows):
		return nil, nil, http.StatusBadRequest, errTooManyRows.Error()
	case errors.Is(err, errNeedHeaderData):
		return nil, nil, http.StatusBadRequest, msgNeedHeaderData
	default:
		rlog.Warn("import preview: parse failed", "ext", ext, "err", err)
		if ext == ".csv" {
			return nil, nil, http.StatusBadRequest, msgCSVInvalid
		}
		return nil, nil, http.StatusBadRequest, msgXLSXInvalid
	}
}

var zipMagic = []byte("PK\x03\x04")

// sniffMatches checks the leading bytes against the declared extension:
// XLSX must be a zip container; CSV must look like UTF-8 text (no NUL bytes,
// not a zip/binary file).
func sniffMatches(ext string, head []byte) bool {
	switch ext {
	case ".xlsx":
		return bytes.HasPrefix(head, zipMagic)
	case ".csv":
		if len(head) == 0 || bytes.HasPrefix(head, zipMagic) || bytes.IndexByte(head, 0) >= 0 {
			return false
		}
		if !strings.HasPrefix(http.DetectContentType(head), "text/") {
			return false
		}
		// Allow a multi-byte rune cut at the 512-byte boundary.
		trimmed := head
		for i := 0; i < utf8.UTFMax-1 && len(trimmed) > 0 && !utf8.Valid(trimmed); i++ {
			trimmed = trimmed[:len(trimmed)-1]
		}
		return utf8.Valid(trimmed)
	}
	return false
}

func parseCSV(r io.Reader) ([]string, [][]string, error) {
	reader := csv.NewReader(r)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	var records [][]string
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("CSV parse: %w", err)
		}
		records = append(records, rec)
		if len(records) > maxImportRows+1 { // +1 header
			return nil, nil, errTooManyRows
		}
	}
	if len(records) < 2 {
		return nil, nil, errNeedHeaderData
	}
	records[0][0] = strings.TrimPrefix(records[0][0], "\uFEFF")
	return records[0], records[1:], nil
}

func parseXLSX(r io.Reader) ([]string, [][]string, error) {
	f, err := excelize.OpenReader(r, excelize.Options{
		UnzipSizeLimit:    xlsxUnzipSizeLimit,
		UnzipXMLSizeLimit: xlsxUnzipXMLSizeLimit,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("XLSX parse: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	if sheet == "" {
		return nil, nil, errNoSheet
	}
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, nil, fmt.Errorf("read sheet: %w", err)
	}
	defer func() { _ = it.Close() }()

	var rows [][]string
	for it.Next() {
		cols, err := it.Columns()
		if err != nil {
			return nil, nil, fmt.Errorf("read row: %w", err)
		}
		rows = append(rows, cols)
		if len(rows) > maxImportRows+1 {
			return nil, nil, errTooManyRows
		}
	}
	if err := it.Error(); err != nil {
		return nil, nil, fmt.Errorf("read sheet: %w", err)
	}
	// GetRows semantics: drop trailing empty rows.
	for len(rows) > 0 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	if len(rows) < 2 {
		return nil, nil, errNeedHeaderData
	}
	return rows[0], rows[1:], nil
}
