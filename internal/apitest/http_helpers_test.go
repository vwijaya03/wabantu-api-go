package apitest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

var nonSlugChar = regexp.MustCompile(`[^a-z0-9]+`)

// UniqueSlug returns a tenant slug safe for register and t_<slug> schema names.
func UniqueSlug(t *testing.T) string {
	t.Helper()
	base := strings.ToLower(t.Name())
	base = strings.TrimPrefix(base, "testauthsmoke_")
	base = strings.TrimPrefix(base, "testorderlistsmoke_")
	base = strings.TrimPrefix(base, "testordercreatesmoke_")
	base = strings.TrimPrefix(base, "testinboxlistconversationssmoke_")
	base = nonSlugChar.ReplaceAllString(base, "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "auth"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	return base + "_" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
}

// NewJSONPostRequest builds a POST request with a JSON body for raw Encore handlers.
func NewJSONPostRequest(t *testing.T, path string, body any) *http.Request {
	t.Helper()
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		payload = bytes.NewReader(b)
	}
	req := httptest.NewRequest(http.MethodPost, path, payload)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// NewGetRequest builds a GET request for raw Encore handlers.
func NewGetRequest(path string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// DecodeJSON unmarshals the response body into dst.
func DecodeJSON(t *testing.T, rr *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(dst); err != nil {
		t.Fatalf("decode response (status %d): %v\nbody: %s", rr.Code, err, rr.Body.String())
	}
}
