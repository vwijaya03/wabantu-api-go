package adminauth

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every endpoint under /api/v1/platform-admin/ must be listed by its Encore route identity.
func TestPlatformAdminEndpointsComplete(t *testing.T) {
	re := regexp.MustCompile(`//encore:api [^\n]*\bpath=(/api/v1/platform-admin/\S*)\s*\nfunc (\w+)`)
	root := filepath.Join("..", "..")
	found := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		svc := filepath.Base(filepath.Dir(p))
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			found++
			if id := svc + "." + m[2]; !PlatformAdminEndpoints[id] {
				t.Errorf("%s: %s (%s) missing from PlatformAdminEndpoints", p, m[1], id)
			}
		}
		return nil
	})
	if found != len(PlatformAdminEndpoints) {
		t.Errorf("found %d platform-admin endpoints in source, map has %d", found, len(PlatformAdminEndpoints))
	}
}

// HandlerEnforcedRaw bypasses the middleware (security boundary): every entry must be a raw
// endpoint in source, and either a platform-admin route (handler uses MeAccess/portal check) or
// have an explicit RawHandlerAccess policy.
func TestHandlerEnforcedRawIsBounded(t *testing.T) {
	re := regexp.MustCompile(`//encore:api [^\n]*\braw\b[^\n]*\n(?://[^\n]*\n)*func (\w+)`)
	raw := map[string]bool{}
	root := filepath.Join("..", "..")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			raw[filepath.Base(filepath.Dir(p))+"."+m[1]] = true
		}
		return nil
	})
	for id := range HandlerEnforcedRaw {
		if !raw[id] {
			t.Errorf("HandlerEnforcedRaw %s is not a raw endpoint in source", id)
		}
		if PlatformAdminEndpoints[id] {
			continue
		}
		allowed := false
		for _, k := range []string{KindClient, KindAdmin, KindImpersonation} {
			if RawHandlerAccess(id, k) == nil {
				allowed = true
			}
		}
		if !allowed {
			t.Errorf("HandlerEnforcedRaw %s has no RawHandlerAccess policy", id)
		}
	}
}

// Kind matrix for the client-portal raw auth handlers. Kind "" = legacy session (NormalizeKind →
// client). No token at all: the handler's own
// AuthenticateHTTP returns 401 before this policy (covered live).
func TestRawHandlerAccessMatrix(t *testing.T) {
	want := map[string]map[string]bool{ // endpoint → kind → allowed
		"auth.Me":            {KindClient: true, KindImpersonation: true, KindAdmin: false, "": true},
		"auth.Reauth":        {KindClient: true, KindImpersonation: false, KindAdmin: false, "": true},
		"auth.Logout":        {KindClient: true, KindImpersonation: true, KindAdmin: true, "": true},
		"auth.FirebaseToken": {KindClient: true, KindImpersonation: true, KindAdmin: false, "": true},
		"auth.Other":         {KindClient: false, KindImpersonation: false, KindAdmin: false},
	}
	for ep, kinds := range want {
		for k, ok := range kinds {
			if got := RawHandlerAccess(ep, k) == nil; got != ok {
				t.Errorf("RawHandlerAccess(%s, %q) allowed=%v, want %v", ep, k, got, ok)
			}
		}
	}
	if RawHandlerAccess("auth.FirebaseToken", KindImpersonation) != nil {
		t.Error("firebase-token impersonation must be allowed (Pantau realtime)")
	}
	if !errors.Is(RawHandlerAccess("auth.FirebaseToken", KindAdmin), ErrWrongTokenKind) {
		t.Error("firebase-token admin must be ErrWrongTokenKind (401)")
	}
	for _, ep := range []string{"auth.Me", "auth.Reauth", "auth.Logout", "auth.FirebaseToken"} {
		if !HandlerEnforcedRaw[ep] {
			t.Errorf("%s must be handler-enforced (middleware errors are dropped on raw endpoints)", ep)
		}
	}
}

// Kind × endpoint matrix. "" kind = no token: the middleware is not reached (auth.Data nil),
// the handler's own auth applies; covered live (401) in the report.
func TestPlatformAdminAccessMatrix(t *testing.T) {
	type ep struct{ id, path string }
	me := ep{"auth.PlatformAdminMe", "/api/v1/platform-admin/me"}
	admin := ep{"admin.ListTenants", "/api/v1/admin/tenants"}
	cases := []struct {
		e    ep
		kind string
		ok   bool
	}{
		{me, KindAdmin, true}, {me, KindClient, false}, {me, KindImpersonation, false},
		{admin, KindAdmin, true}, {admin, KindClient, false}, {admin, KindImpersonation, false},
	}
	for _, c := range cases {
		err := RequestAccessEndpoint(c.e.id, c.e.path, false, c.kind)
		if (err == nil) != c.ok {
			t.Errorf("%s as %s: err=%v want ok=%v", c.e.path, c.kind, err, c.ok)
		}
	}
}

// Raw-path tricks cannot change the decision: classification uses the matched endpoint.
// For an endpoint that is NOT platform-admin, path variants pointing "into" platform-admin
// never grant an admin token access, and never let client/impersonation tokens through on
// the real platform-admin endpoint.
func TestPlatformAdminPathVariants(t *testing.T) {
	variants := []string{
		"/api/v1/platform-admin/me/", "/api/v1/PLATFORM-ADMIN/me", "/API/V1/platform-admin/me",
		"/api/v1/orders/../platform-admin/me", "/api/v1/platform-admin%2Fme", "/api/v1%2Fplatform-admin/me",
		"//api/v1/platform-admin/me", "/api/v1/./platform-admin/me",
	}
	for _, v := range variants {
		for _, k := range []string{KindClient, KindImpersonation} {
			if RequestAccessEndpoint("auth.PlatformAdminMe", v, false, k) == nil {
				t.Errorf("%s token passed platform-admin/me via %q", k, v)
			}
		}
		// a client endpoint reached via a weird path stays a client endpoint
		if RequestAccessEndpoint("order.ListOrders", v, false, KindAdmin) == nil {
			t.Errorf("admin token passed client endpoint via %q", v)
		}
	}
}

func TestSessionlessExactOnly(t *testing.T) {
	for _, k := range []string{KindClient, KindImpersonation, KindAdmin} {
		if RequestAccessEndpoint("auth.PlatformAdminLogin", "/api/v1/platform-admin/login", false, k) != nil {
			t.Errorf("admin login blocked for %s", k)
		}
	}
	for _, id := range []string{"auth.PlatformAdminLoginX", "auth.platformadminlogin", "PlatformAdminLogin", "auth.PlatformAdminMe"} {
		if SessionlessEndpoints[id] {
			t.Errorf("%q must not be sessionless", id)
		}
	}
}

// The handler of every HandlerEnforcedRaw "me" endpoint must reject non-admin kinds itself.
func TestHandlerEnforcedRawMeRejectsNonAdmin(t *testing.T) {
	if !HandlerEnforcedRaw["auth.PlatformAdminMe"] {
		t.Fatal("PlatformAdminMe expected handler-enforced")
	}
	for _, k := range []string{KindClient, KindImpersonation, ""} {
		if MeAccess(MeAdmin, k) == nil {
			t.Errorf("MeAccess(MeAdmin, %q) allowed", k)
		}
	}
	if MeAccess(MeAdmin, KindAdmin) != nil {
		t.Error("admin rejected by MeAccess")
	}
	// Non-platform-admin entries are bounded by TestHandlerEnforcedRawIsBounded.
}
