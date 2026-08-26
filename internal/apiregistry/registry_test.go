package apiregistry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"encore.app/wabantu/internal/apiregistry"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "encore.app")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("encore.app not found")
		}
		dir = parent
	}
}

func TestDiscoverEndpointCount(t *testing.T) {
	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) < 300 {
		t.Fatalf("expected 300+ endpoints, got %d — apakah scan root salah?", len(eps))
	}
	t.Logf("discovered %d endpoints", len(eps))
}

func TestEndpointRoutesUnique(t *testing.T) {
	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]apiregistry.Endpoint{}
	for _, ep := range eps {
		key := apiregistry.RouteKey(ep.Method, ep.Path)
		if prev, ok := seen[key]; ok {
			t.Fatalf("duplicate route %s:\n  %s:%d\n  %s:%d", key, prev.File, prev.Line, ep.File, ep.Line)
		}
		seen[key] = ep
	}
}

func TestEndpointPathsWellFormed(t *testing.T) {
	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		if !strings.HasPrefix(ep.Path, "/api/") &&
			!strings.HasPrefix(ep.Path, "/webhook/") &&
			!strings.HasPrefix(ep.Path, "/whatsapp/") &&
			!strings.HasPrefix(ep.Path, "/internal/") {
			t.Errorf("%s:%d path %q should start with /api/, /webhook/, /whatsapp/, or /internal/", ep.File, ep.Line, ep.Path)
		}
		if ep.Access != "auth" && ep.Access != "public" && ep.Access != "private" {
			t.Errorf("%s:%d invalid access %q", ep.File, ep.Line, ep.Access)
		}
	}
}

// TestEndpointCatalogSnapshot — golden regression: tambah endpoint baru wajib update snapshot.
//
//	go run scripts/gen-api-catalog.go
func TestEndpointCatalogSnapshot(t *testing.T) {
	root := repoRoot(t)
	live, err := apiregistry.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	snapPath := filepath.Join(root, "internal/apiregistry/catalog_snapshot.json")
	b, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("read snapshot: %v (jalankan: go run scripts/gen-api-catalog.go)", err)
	}
	var snap struct {
		Count     int                   `json:"count"`
		Endpoints []apiregistry.Endpoint `json:"endpoints"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Count != len(live) {
		t.Fatalf("endpoint count drift: snapshot=%d live=%d — jalankan: go run scripts/gen-api-catalog.go", snap.Count, len(live))
	}
	if len(snap.Endpoints) != len(live) {
		t.Fatalf("snapshot endpoints len=%d live=%d", len(snap.Endpoints), len(live))
	}
	for i := range live {
		if snap.Endpoints[i].Path != live[i].Path ||
			snap.Endpoints[i].Method != live[i].Method ||
			snap.Endpoints[i].Access != live[i].Access ||
			snap.Endpoints[i].File != live[i].File {
			t.Fatalf("drift at index %d:\n  snapshot: %+v\n  live:     %+v\njalankan: go run scripts/gen-api-catalog.go",
				i, snap.Endpoints[i], live[i])
		}
	}
}

// TestPublicHealthEndpoints — smoke keys for uptime (no HTTP, structural only).
func TestPublicHealthEndpoints(t *testing.T) {
	eps, err := apiregistry.Discover(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"GET /api/v1/health":       false,
		"GET /api/v1/health/ready": false,
	}
	for _, ep := range eps {
		key := apiregistry.RouteKey(ep.Method, ep.Path)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Fatalf("missing public health route %s", k)
		}
	}
}
