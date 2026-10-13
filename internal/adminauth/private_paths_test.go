package adminauth

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every private endpoint outside /api/v1/internal/ must be exempt from the session-kind
// middleware, else admin requests that call it service-to-service would be rejected.
func TestPrivatePathsComplete(t *testing.T) {
	re := regexp.MustCompile(`//encore:api private\b.*\bpath=(\S+)`)
	root := filepath.Join("..", "..")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			path := m[1]
			if strings.HasPrefix(path, "/api/v1/internal/") {
				continue
			}
			if RequestAccess(path, false, KindAdmin) != nil {
				t.Errorf("%s: private endpoint %s not exempt (add to PrivatePaths)", p, path)
			}
		}
		return nil
	})
}

// Every endpoint whose handler checks for super_admin by role must be tagged so admin
// sessions are accepted (and client sessions rejected) there.
func TestSuperAdminOnlyEndpointsTagged(t *testing.T) {
	for _, f := range []string{"audit/audit.go", "flag/flag.go"} {
		b, err := os.ReadFile(filepath.Join("..", "..", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "//encore:api auth") && !strings.Contains(l, "/flags/check/") && !strings.Contains(l, "tag:super_admin") {
				t.Errorf("%s: %s must carry tag:super_admin", f, l)
			}
		}
	}
}
