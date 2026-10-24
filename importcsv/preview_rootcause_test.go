package importcsv

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildCurlLikeUpload mirrors `curl -F file=@x.csv -F targetTable=business_catalog_item`
// (and FE lib/api/import.ts): multipart with fields "file" + "targetTable".
func buildCurlLikeUpload(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("targetTable", "business_catalog_item"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestRootCause_TypedAPIFileHeaderNeverOpens documents why the endpoint must
// be raw. On master (3f66543) the typed endpoint
// `Preview(ctx, file *multipart.FileHeader)` was called from `encore test` with a
// FileHeader produced by ParseMultipartForm from this exact curl-style request,
// and still failed with invalid_argument "cannot open uploaded file" (log:
// audit/logs-import-preview/01-red-rootcause-master.log): a typed Encore API
// marshals its request param across the API boundary, and FileHeader keeps the
// uploaded bytes only in unexported fields. This test pins that mechanism.
func TestRootCause_TypedAPIFileHeaderNeverOpens(t *testing.T) {
	req := buildCurlLikeUpload(t, "x.csv", []byte("nama,kode\nItem A,SKU1\n"))
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	fh := req.MultipartForm.File["file"][0]
	if f, err := fh.Open(); err != nil {
		t.Fatalf("sanity: parsed FileHeader must open locally: %v", err)
	} else {
		_ = f.Close()
	}

	b, err := json.Marshal(fh)
	if err != nil {
		t.Fatal(err)
	}
	var crossed multipart.FileHeader
	if err := json.Unmarshal(b, &crossed); err != nil {
		t.Fatal(err)
	}
	if crossed.Filename != "x.csv" {
		t.Fatalf("filename should survive the boundary, got %q", crossed.Filename)
	}
	if f, err := crossed.Open(); err == nil {
		_ = f.Close()
		t.Fatal("FileHeader after API-boundary serialization unexpectedly opened; typed endpoint might work")
	}
}
