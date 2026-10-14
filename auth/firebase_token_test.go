package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/internal/realtime"
	"encore.app/wabantu/shared/types"
)

// Handler-level kind gate for POST /auth/firebase-token: explicit JSON errors, never 200 empty.
func TestFirebaseTokenKindGate(t *testing.T) {
	cases := []struct {
		kind   string
		ok     bool
		status int
		code   string
	}{
		{adminauth.KindClient, true, 0, ""},
		{adminauth.KindAdmin, false, http.StatusUnauthorized, "unauthenticated"},
		{adminauth.KindImpersonation, false, http.StatusForbidden, "permission_denied"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		if got := firebaseTokenKindOK(rec, c.kind); got != c.ok {
			t.Fatalf("%s: ok=%v want %v", c.kind, got, c.ok)
		}
		if c.ok {
			if rec.Body.Len() != 0 {
				t.Errorf("%s: gate wrote a body on pass", c.kind)
			}
			continue
		}
		if rec.Code != c.status {
			t.Errorf("%s: status %d want %d", c.kind, rec.Code, c.status)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["code"] != c.code || body["message"] == "" {
			t.Errorf("%s: body %q (err %v)", c.kind, rec.Body.String(), err)
		}
	}
}

// Ordering: the kind gate runs before the Firebase config check. Admin/impersonation must get
// 401/403 with a JSON body even when Firebase is unconfigured; cfgFn must not be reached.
func TestFirebaseTokenKindCheckedBeforeConfig(t *testing.T) {
	for _, c := range []struct {
		kind    string
		status  int
		cfgUsed bool
	}{
		{adminauth.KindAdmin, http.StatusUnauthorized, false},
		{adminauth.KindImpersonation, http.StatusForbidden, false},
		{adminauth.KindClient, http.StatusServiceUnavailable, true}, // unconfigured → 503
	} {
		called := false
		cfgFn := func() (*realtime.Config, error) { called = true; return nil, errors.New("unconfigured") }
		u := &types.AuthUser{AccountID: "acc-1", TenantID: "tenant-1", Role: "owner", SessionKind: c.kind}
		if c.kind != adminauth.KindClient {
			u.Role = "super_admin"
		}
		rec := httptest.NewRecorder()
		serveFirebaseTokenFor(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/firebase-token", nil), u, cfgFn)
		if rec.Code != c.status || called != c.cfgUsed {
			t.Errorf("%s: status %d cfgCalled %v, want %d %v", c.kind, rec.Code, called, c.status, c.cfgUsed)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["code"] == "" || body["message"] == "" {
			t.Errorf("%s: not a JSON error body: %q", c.kind, rec.Body.String())
		}
	}
	// No session → 401 JSON from the realtime handler (before config too).
	rec := httptest.NewRecorder()
	serveFirebaseTokenFor(rec, httptest.NewRequest(http.MethodPost, "/", nil), nil,
		func() (*realtime.Config, error) { t.Error("cfg reached without session"); return nil, nil })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
}
