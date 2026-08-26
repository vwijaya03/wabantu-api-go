package apitest

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	encoreAuth "encore.dev/beta/auth"
	"encore.dev/et"
)

// RequireEncoreInfra skips when -short; smoke tests need encore test DB + Redis.
func RequireEncoreInfra(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("smoke tests require encore test databases and Redis")
	}
}

// BearerHeader returns Authorization header value for raw HTTP handler tests.
func BearerHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// WithOwnerAuth sets encore auth context for typed //encore:api handlers in the current test.
func WithOwnerAuth(fx *TenantFixture) {
	et.OverrideAuthInfo(encoreAuth.UID(fx.AccountID), fx.AuthUser())
}

// AssertJSONFields checks top-level JSON keys after marshaling a handler response.
func AssertJSONFields(t *testing.T, v any, fields ...string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\nbody: %s", err, string(b))
	}
	for _, f := range fields {
		if _, ok := m[f]; !ok {
			t.Fatalf("JSON missing field %q; keys=%v", f, jsonKeys(m))
		}
	}
}

// AssertJSONArrayField checks that a top-level field is a JSON array (empty OK).
func AssertJSONArrayField(t *testing.T, v any, field string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response is not a JSON object: %v", err)
	}
	raw, ok := m[field]
	if !ok {
		t.Fatalf("JSON missing array field %q", field)
	}
	if len(raw) == 0 || raw[0] != '[' {
		t.Fatalf("field %q is not a JSON array: %s", field, string(raw))
	}
}

func jsonKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// DecodeAuthEnvelope decodes a wrapped auth JSON response from a raw handler recorder.
func DecodeAuthEnvelope(t *testing.T, rr *httptest.ResponseRecorder) (token string, email string) {
	t.Helper()
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			AccessToken string `json:"accessToken"`
			User        struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"data"`
	}
	DecodeJSON(t, rr, &resp)
	if !resp.Success {
		t.Fatalf("auth success=false: %s", resp.Message)
	}
	if resp.Data.AccessToken == "" {
		t.Fatal("missing accessToken")
	}
	return resp.Data.AccessToken, resp.Data.User.Email
}
