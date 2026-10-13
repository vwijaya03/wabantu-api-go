package adminauth

import (
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
