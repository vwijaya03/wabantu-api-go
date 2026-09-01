package apiregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const genCatalogHint = "jalankan: go run scripts/gen-api-catalog.go"

type serviceCountsSnapshot struct {
	TotalServices  int            `json:"total_services"`
	TotalEndpoints int            `json:"total_endpoints"`
	Services       map[string]int `json:"services"`
}

// StructuralRegressionResult mirrors apiregistry go test suite.
type StructuralRegressionResult struct {
	Passed     bool   `json:"passed"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skipReason,omitempty"`
}

func loadServiceCounts(root string) (serviceCountsSnapshot, error) {
	path := filepath.Join(root, "internal/apiregistry/service_counts.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return serviceCountsSnapshot{}, fmt.Errorf("read service_counts.json: %w (%s)", err, genCatalogHint)
	}
	var snap serviceCountsSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return serviceCountsSnapshot{}, err
	}
	if len(snap.Services) == 0 {
		return serviceCountsSnapshot{}, fmt.Errorf("service_counts.json has no services (%s)", genCatalogHint)
	}
	return snap, nil
}

func liveServiceCounts(eps []Endpoint) map[string]int {
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

// FindRepoRoot walks up from cwd looking for encore.app marker.
func FindRepoRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "encore.app")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// RunStructuralRegression validates endpoint catalog drift (same as go test ./internal/apiregistry/).
func RunStructuralRegression() StructuralRegressionResult {
	start := time.Now()
	root, ok := FindRepoRoot()
	if !ok {
		return StructuralRegressionResult{
			Passed:     true,
			Skipped:    true,
			SkipReason: "source tree tidak tersedia di environment ini — jalankan di CI atau Encore local",
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	snap, err := loadServiceCounts(root)
	if err != nil {
		return StructuralRegressionResult{Passed: false, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if snap.TotalServices != len(snap.Services) {
		return StructuralRegressionResult{
			Passed: false,
			Error:  fmt.Sprintf("total_services=%d but services map has %d entries (%s)", snap.TotalServices, len(snap.Services), genCatalogHint),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	eps, err := Discover(root)
	if err != nil {
		return StructuralRegressionResult{Passed: false, Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
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
		return StructuralRegressionResult{
			Passed: false,
			Error:  fmt.Sprintf("%d catalog service(s) have no endpoints: %s", len(missing), strings.Join(missing, ", ")),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	if snap.TotalEndpoints != len(eps) {
		return StructuralRegressionResult{
			Passed: false,
			Error:  fmt.Sprintf("total endpoint drift: snapshot=%d live=%d — %s", snap.TotalEndpoints, len(eps), genCatalogHint),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	if snap.TotalServices != len(live) {
		return StructuralRegressionResult{
			Passed: false,
			Error: fmt.Sprintf("service count drift: snapshot=%d live=%d — %s\n%s",
				snap.TotalServices, len(live), genCatalogHint, formatServiceCountDrift(snap.Services, live)),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	if drift := formatServiceCountDrift(snap.Services, live); drift != "" {
		return StructuralRegressionResult{
			Passed: false,
			Error:  fmt.Sprintf("per-service endpoint count drift — %s:\n%s", genCatalogHint, drift),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	for _, ep := range eps {
		if ep.Raw {
			continue
		}
		if ep.Method == "" || ep.Method == "*" {
			return StructuralRegressionResult{
				Passed: false,
				Error: fmt.Sprintf("%s:%d non-raw endpoint missing explicit method (access=%s path=%s)",
					ep.File, ep.Line, ep.Access, ep.Path),
				DurationMs: time.Since(start).Milliseconds(),
			}
		}
	}
	want := map[string]bool{
		"GET /api/v1/health":                    false,
		"GET /api/v1/health/ready":              false,
		"POST /api/v1/auth/login":               false,
		"POST /api/v1/auth/register":            false,
		"* /api/v1/webhook/whatsapp":            false,
		"POST /api/v1/payment/webhook/midtrans": false,
	}
	for _, ep := range eps {
		key := RouteKey(ep.Method, ep.Path)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	var missingRoutes []string
	for route, found := range want {
		if !found {
			missingRoutes = append(missingRoutes, route)
		}
	}
	sort.Strings(missingRoutes)
	if len(missingRoutes) > 0 {
		return StructuralRegressionResult{
			Passed: false,
			Error:  fmt.Sprintf("missing critical public routes:\n  %s", strings.Join(missingRoutes, "\n  ")),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	return StructuralRegressionResult{Passed: true, DurationMs: time.Since(start).Milliseconds()}
}
