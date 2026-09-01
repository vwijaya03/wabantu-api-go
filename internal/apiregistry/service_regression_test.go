package apiregistry_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"encore.app/wabantu/internal/apiregistry"
)

const genCatalogHint = "jalankan: go run scripts/gen-api-catalog.go"

type serviceCountsSnapshot struct {
	TotalServices  int            `json:"total_services"`
	TotalEndpoints int            `json:"total_endpoints"`
	Services       map[string]int `json:"services"`
}

func loadServiceCounts(t *testing.T) serviceCountsSnapshot {
	t.Helper()
	root := repoRoot(t)
	path := filepath.Join(root, "internal/apiregistry/service_counts.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read service_counts.json: %v (%s)", err, genCatalogHint)
	}
	var snap serviceCountsSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Services) == 0 {
		t.Fatalf("service_counts.json has no services (%s)", genCatalogHint)
	}
	return snap
}

func liveServiceCounts(eps []apiregistry.Endpoint) map[string]int {
	counts := make(map[string]int)
	for _, ep := range eps {
		counts[ep.Service]++
	}
	return counts
}

func formatServiceCountDrift(expected, live map[string]int) string {
	var keys []string
	seen := make(map[string]struct{}, len(expected)+len(live))
	for k := range expected {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range live {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, svc := range keys {
		exp, hasExp := expected[svc]
		got, hasLive := live[svc]
		if hasExp && hasLive && exp == got {
			continue
		}
		switch {
		case !hasExp:
			fmt.Fprintf(&b, "  + %s: snapshot missing, live=%d\n", svc, got)
		case !hasLive:
			fmt.Fprintf(&b, "  - %s: snapshot=%d, live=missing\n", svc, exp)
		default:
			fmt.Fprintf(&b, "  ~ %s: snapshot=%d, live=%d\n", svc, exp, got)
		}
	}
	return b.String()
}

// TestEachServiceHasEndpoints — setiap service di katalog wajib punya >=1 endpoint.
func TestEachServiceHasEndpoints(t *testing.T) {
	snap := loadServiceCounts(t)
	if snap.TotalServices != len(snap.Services) {
		t.Fatalf("total_services=%d but services map has %d entries (%s)",
			snap.TotalServices, len(snap.Services), genCatalogHint)
	}

	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	live := liveServiceCounts(eps)

	var missing []string
	for svc := range snap.Services {
		if live[svc] < 1 {
			missing = append(missing, svc)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%d catalog service(s) have no endpoints: %s", len(missing), strings.Join(missing, ", "))
	}
}

// TestServiceEndpointCounts — golden per-service counts; drift wajib update via gen script.
func TestServiceEndpointCounts(t *testing.T) {
	snap := loadServiceCounts(t)

	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	live := liveServiceCounts(eps)

	if snap.TotalEndpoints != len(eps) {
		t.Fatalf("total endpoint drift: snapshot=%d live=%d — %s", snap.TotalEndpoints, len(eps), genCatalogHint)
	}
	if snap.TotalServices != len(live) {
		t.Fatalf("service count drift: snapshot=%d live=%d — %s\n%s",
			snap.TotalServices, len(live), genCatalogHint, formatServiceCountDrift(snap.Services, live))
	}

	drift := formatServiceCountDrift(snap.Services, live)
	if drift != "" {
		t.Fatalf("per-service endpoint count drift — %s:\n%s", genCatalogHint, drift)
	}
}

// TestAuthEndpointsHaveMethod — endpoint non-raw wajib punya HTTP method eksplisit.
func TestAuthEndpointsHaveMethod(t *testing.T) {
	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		if ep.Raw {
			continue
		}
		if ep.Method == "" || ep.Method == "*" {
			t.Errorf("%s:%d non-raw endpoint missing explicit method (access=%s path=%s)",
				ep.File, ep.Line, ep.Access, ep.Path)
		}
	}
}

// TestCriticalPublicRoutes — smoke keys untuk health, auth publik, dan webhook ingress.
func TestCriticalPublicRoutes(t *testing.T) {
	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"GET /api/v1/health":                        false,
		"GET /api/v1/health/ready":                  false,
		"POST /api/v1/auth/login":                   false,
		"POST /api/v1/auth/register":              false,
		"* /api/v1/webhook/whatsapp":              false,
		"POST /api/v1/payment/webhook/midtrans":     false,
	}

	for _, ep := range eps {
		key := apiregistry.RouteKey(ep.Method, ep.Path)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}

	var missing []string
	for route, found := range want {
		if !found {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("missing critical public routes:\n  %s", strings.Join(missing, "\n  "))
	}
}
