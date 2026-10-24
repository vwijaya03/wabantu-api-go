package importcsv

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/shared/types"
)

type stageRecorder struct {
	calls   int
	jobID   string
	payload importStagingPayload
	err     error
}

func (s *stageRecorder) stage(_ context.Context, jobID string, raw []byte) error {
	s.calls++
	s.jobID = jobID
	if s.err != nil {
		return s.err
	}
	return json.Unmarshal(raw, &s.payload)
}

func ownerUser() *types.AuthUser {
	return &types.AuthUser{AccountID: "acc_owner", TenantID: "ten_1", TenantSchema: "t_import_test", Role: "owner"}
}

func testDeps(user *types.AuthUser, rec *stageRecorder) previewDeps {
	return previewDeps{
		authenticate: func(context.Context, *http.Request) (*types.AuthUser, error) {
			if user == nil {
				return nil, errors.New("no user")
			}
			return user, nil
		},
		stage: rec.stage,
		now:   func() time.Time { return time.Unix(0, 42) },
	}
}

type part struct {
	field, filename string
	content         []byte
}

func multipartBody(t *testing.T, parts ...part) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.filename != "" {
			fw, err := mw.CreateFormFile(p.field, p.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write(p.content); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := mw.WriteField(p.field, string(p.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func uploadReq(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	body, ct := multipartBody(t,
		part{field: "file", filename: filename, content: content},
		part{field: "targetTable", content: []byte("business_catalog_item")},
	)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", body)
	req.Header.Set("Content-Type", ct)
	return req
}

func do(t *testing.T, req *http.Request, deps previewDeps) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	servePreview(rr, req, deps)
	return rr
}

func errBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("error body not JSON: %q", rr.Body.String())
	}
	if _, ok := m["code"]; !ok {
		t.Fatalf("error body missing code: %v", m)
	}
	return m
}

func buildXLSX(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPreview_CSV_OK(t *testing.T) {
	rec := &stageRecorder{}
	csv := "\uFEFFNama,Kode,Harga\nItem Ü,SKU1,10000\nItem B,SKU2,2500\n"
	rr := do(t, buildCurlLikeUpload(t, "katalog.CSV", []byte(csv)), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp PreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.JobID != "imp_42" || resp.TargetTable != "business_catalog_item" || resp.TotalRows != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Headers[0] != "Nama" || resp.Suggestions["Nama"] != "name" || resp.Suggestions["Kode"] != "external_code" {
		t.Fatalf("headers/suggestions = %v %v", resp.Headers, resp.Suggestions)
	}
	if resp.SampleRows[0][0] != "Item Ü" {
		t.Fatalf("sample = %v", resp.SampleRows)
	}
	// Same JSON keys as the typed PreviewResponse.
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &keys)
	for _, k := range []string{"jobId", "targetTable", "headers", "sampleRows", "suggestions", "totalRows"} {
		if _, ok := keys[k]; !ok {
			t.Fatalf("missing key %q in %s", k, rr.Body.String())
		}
	}
	if rec.calls != 1 || rec.jobID != "imp_42" {
		t.Fatalf("stage calls=%d job=%q", rec.calls, rec.jobID)
	}
	if rec.payload.TenantSchema != "t_import_test" || rec.payload.UploadedBy != "acc_owner" || len(rec.payload.Rows) != 2 {
		t.Fatalf("staged payload = %+v", rec.payload)
	}
}

func TestPreview_DefaultAndKBTarget(t *testing.T) {
	rec := &stageRecorder{}
	body, ct := multipartBody(t, part{field: "file", filename: "x.csv", content: []byte("a,b\n1,2\n")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", body)
	req.Header.Set("Content-Type", ct)
	rr := do(t, req, testDeps(ownerUser(), rec))
	if rr.Code != http.StatusOK || rec.payload.TargetTable != "business_catalog_item" {
		t.Fatalf("default target: status=%d target=%q", rr.Code, rec.payload.TargetTable)
	}

	body, ct = multipartBody(t,
		part{field: "file", filename: "x.csv", content: []byte("pertanyaan,jawaban\nq,a\n")},
		part{field: "targetTable", content: []byte("knowledge_base_entry")},
	)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", body)
	req.Header.Set("Content-Type", ct)
	rr = do(t, req, testDeps(ownerUser(), rec))
	if rr.Code != http.StatusOK || rec.payload.TargetTable != "knowledge_base_entry" {
		t.Fatalf("kb target: status=%d target=%q", rr.Code, rec.payload.TargetTable)
	}
}

func TestPreview_XLSX_OK(t *testing.T) {
	rec := &stageRecorder{}
	x := buildXLSX(t, [][]any{{"Nama", "Kode", "Harga"}, {"Item 😀", "SKU1", 10000}, {"Item B", "SKU2", 2500}})
	rr := do(t, uploadReq(t, "katalog.xlsx", x), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp PreviewResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.TotalRows != 2 || resp.Headers[1] != "Kode" || resp.SampleRows[0][0] != "Item 😀" || resp.SampleRows[0][2] != "10000" {
		t.Fatalf("resp = %+v", resp)
	}
	if rec.calls != 1 || rec.payload.TenantSchema != "t_import_test" {
		t.Fatalf("stage = %+v", rec)
	}
}

func TestPreview_NoToken_401(t *testing.T) {
	rec := &stageRecorder{}
	deps := testDeps(nil, rec)
	deps.authenticate = appauth.AuthenticateHTTP // real auth: no Authorization header/cookie
	rr := do(t, uploadReq(t, "x.csv", []byte("a,b\n1,2\n")), deps)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	errBody(t, rr)
	if rec.calls != 0 {
		t.Fatal("must not stage without auth")
	}
}

func TestPreview_StaffForbidden_403(t *testing.T) {
	rec := &stageRecorder{}
	u := ownerUser()
	u.Role = "staff"
	rr := do(t, uploadReq(t, "x.csv", []byte("a,b\n1,2\n")), testDeps(u, rec))
	if rr.Code != http.StatusForbidden || rec.calls != 0 {
		t.Fatalf("status=%d calls=%d", rr.Code, rec.calls)
	}
}

func oversizeCSV() []byte {
	var b bytes.Buffer
	b.WriteString("nama,kode\n")
	line := strings.Repeat("x", 100) + ",SKU\n"
	for int64(b.Len()) <= PreviewMaxBodyBytes {
		b.WriteString(line)
	}
	return b.Bytes()
}

func TestPreview_Oversize_ContentLength_413(t *testing.T) {
	rec := &stageRecorder{}
	rr := do(t, uploadReq(t, "big.csv", oversizeCSV()), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusRequestEntityTooLarge || rec.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", rr.Code, rec.calls, rr.Body.String())
	}
	errBody(t, rr)
}

// Chunked / unknown length body: only http.MaxBytesReader can stop it.
func TestPreview_Oversize_Streamed_413(t *testing.T) {
	rec := &stageRecorder{}
	req := uploadReq(t, "big.csv", oversizeCSV())
	req.Body = io.NopCloser(struct{ io.Reader }{req.Body})
	req.ContentLength = -1
	rr := do(t, req, testDeps(ownerUser(), rec))
	if rr.Code != http.StatusRequestEntityTooLarge || rec.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", rr.Code, rec.calls, rr.Body.String())
	}
}

func TestPreview_WrongType_400(t *testing.T) {
	x := buildXLSX(t, [][]any{{"a"}, {"b"}})
	cases := []struct {
		name, filename string
		content        []byte
	}{
		{"xls extension", "old.xls", []byte("a,b\n1,2\n")},
		{"txt extension", "x.txt", []byte("a,b\n1,2\n")},
		{"no extension", "x", []byte("a,b\n1,2\n")},
		{"zip disguised as csv", "x.csv", x},
		{"binary NUL as csv", "x.csv", []byte("a,b\x00\x01\x02\n1,2\n")},
		{"png as csv", "x.csv", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)},
		{"text disguised as xlsx", "x.xlsx", []byte("a,b\n1,2\n")},
		{"empty csv", "x.csv", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &stageRecorder{}
			rr := do(t, uploadReq(t, tc.filename, tc.content), testDeps(ownerUser(), rec))
			if rr.Code != http.StatusBadRequest || rec.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", rr.Code, rec.calls, rr.Body.String())
			}
			errBody(t, rr)
		})
	}
}

// zipBombXLSX re-packs a valid XLSX and adds a highly compressible entry whose
// uncompressed size exceeds xlsxUnzipSizeLimit while the upload stays tiny.
func zipBombXLSX(t *testing.T, bombName string, bombSize int64) []byte {
	t.Helper()
	src := buildXLSX(t, [][]any{{"Nama", "Kode"}, {"A", "1"}})
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		rc, _ := f.Open()
		_, _ = io.Copy(w, rc)
		_ = rc.Close()
	}
	w, err := zw.Create(bombName)
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte(" "), 1<<20)
	for written := int64(0); written < bombSize; written += int64(len(chunk)) {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPreview_XLSXZipBomb_Rejected(t *testing.T) {
	for _, name := range []string{"xl/media/bomb.bin", "xl/worksheets/sheet1.xml"} {
		t.Run(name, func(t *testing.T) {
			bomb := zipBombXLSX(t, name, xlsxUnzipSizeLimit+(8<<20))
			if int64(len(bomb)) >= PreviewMaxBodyBytes {
				t.Fatalf("bomb should be small on the wire, got %d bytes", len(bomb))
			}
			rec := &stageRecorder{}
			rr := do(t, uploadReq(t, "bomb.xlsx", bomb), testDeps(ownerUser(), rec))
			if rr.Code != http.StatusBadRequest || rec.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", rr.Code, rec.calls, rr.Body.String())
			}
			if m := errBody(t, rr); m["message"] != msgXLSXInvalid {
				t.Fatalf("message=%v", m["message"])
			}
		})
	}
}

func TestPreview_MissingFields_400(t *testing.T) {
	cases := map[string]func() *http.Request{
		"no file field": func() *http.Request {
			body, ct := multipartBody(t, part{field: "targetTable", content: []byte("business_catalog_item")})
			r := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", body)
			r.Header.Set("Content-Type", ct)
			return r
		},
		"file under wrong field name": func() *http.Request {
			body, ct := multipartBody(t, part{field: "files", filename: "x.csv", content: []byte("a,b\n1,2\n")})
			r := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", body)
			r.Header.Set("Content-Type", ct)
			return r
		},
		"not multipart (json)": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", strings.NewReader(`{"Filename":"x.csv"}`))
			r.Header.Set("Content-Type", "application/json")
			return r
		},
		"unsupported targetTable": func() *http.Request {
			body, ct := multipartBody(t,
				part{field: "file", filename: "x.csv", content: []byte("a,b\n1,2\n")},
				part{field: "targetTable", content: []byte(`contact"; DROP TABLE x; --`)},
			)
			r := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", body)
			r.Header.Set("Content-Type", ct)
			return r
		},
		"header only": func() *http.Request { return uploadReq(t, "x.csv", []byte("a,b\n")) },
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &stageRecorder{}
			rr := do(t, mk(), testDeps(ownerUser(), rec))
			if rr.Code != http.StatusBadRequest || rec.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", rr.Code, rec.calls, rr.Body.String())
			}
			errBody(t, rr)
		})
	}
}

func TestPreview_TooManyRows_400(t *testing.T) {
	var b strings.Builder
	b.WriteString("a,b\n")
	for i := 0; i <= maxImportRows; i++ {
		fmt.Fprintf(&b, "%d,x\n", i)
	}
	rec := &stageRecorder{}
	rr := do(t, uploadReq(t, "x.csv", []byte(b.String())), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusBadRequest || rec.calls != 0 {
		t.Fatalf("status=%d calls=%d", rr.Code, rec.calls)
	}
}

func TestPreview_GenericErrors_NoInternals(t *testing.T) {
	// Malformed CSV: csv package error text must not leak.
	rec := &stageRecorder{}
	rr := do(t, uploadReq(t, "x.csv", []byte("a,b\n1\n")), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
	if m := errBody(t, rr); m["message"] != msgCSVInvalid {
		t.Fatalf("message=%v", m["message"])
	}

	// Staging (Redis) failure: 500 with generic message only.
	rec = &stageRecorder{err: errors.New("dial tcp 10.0.0.5:6379: connection refused")}
	rr = do(t, uploadReq(t, "x.csv", []byte("a,b\n1,2\n")), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "6379") || strings.Contains(rr.Body.String(), "dial") {
		t.Fatalf("internal error leaked: %s", rr.Body.String())
	}
}

// A bomb entry that lies about its uncompressed size (declares 1 KiB, inflates
// to ~80 MiB) passes the declared-size check, but archive/zip refuses to inflate
// past the declared size (ErrFormat), so excelize only ever buffers ~1 KiB of it.
// The upload is then handled normally (the bogus media entry is irrelevant).
func TestPreview_XLSXZipBomb_ForgedSize_NotInflated(t *testing.T) {
	src := buildXLSX(t, [][]any{{"Nama", "Kode"}, {"A", "1"}})
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		rc, _ := f.Open()
		_, _ = io.Copy(w, rc)
		_ = rc.Close()
	}
	var deflated bytes.Buffer
	fw, _ := flate.NewWriter(&deflated, flate.BestCompression)
	crc := crc32.NewIEEE()
	chunk := bytes.Repeat([]byte(" "), 1<<20)
	for i := 0; i < 80; i++ {
		_, _ = fw.Write(chunk)
		_, _ = crc.Write(chunk)
	}
	_ = fw.Close()
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "xl/media/forged.bin",
		Method:             zip.Deflate,
		CRC32:              crc.Sum32(),
		CompressedSize64:   uint64(deflated.Len()),
		UncompressedSize64: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(deflated.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	forged := out.Bytes()
	zr2, err := zip.NewReader(bytes.NewReader(forged), int64(len(forged)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr2.File {
		if f.Name != "xl/media/forged.bin" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(io.Discard, rc)
		_ = rc.Close()
		if !errors.Is(err, zip.ErrFormat) || n > 1024+(64<<10) {
			t.Fatalf("archive/zip inflated %d bytes past declared size (err=%v)", n, err)
		}
	}

	rec := &stageRecorder{}
	rr := do(t, uploadReq(t, "forged.xlsx", forged), testDeps(ownerUser(), rec))
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
