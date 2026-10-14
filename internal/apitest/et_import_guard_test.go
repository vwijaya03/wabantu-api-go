package apitest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const encoreEtImportPath = "encore.dev/et"

var encoreEtImportRE = regexp.MustCompile(`(?m)^\s*"` + regexp.QuoteMeta(encoreEtImportPath) + `"`)

func TestNoEncoreEtImportOutsideTestFiles(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		if strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "encore.gen.go") {
			return nil
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if encoreEtImportRE.Match(b) {
			offenders = append(offenders, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(offenders) == 0 {
		return
	}
	t.Fatalf(
		"non-test .go must not import %q (encore run panics: et cannot create manager in non-test environment).\n"+
			"Keep apitest helpers in *_test.go only (fixture_test.go, tenant_fixture_test.go, http_helpers_test.go).\n"+
			"Do not add fixture.go / tenant_fixture.go / http_helpers.go under internal/apitest — that package is an Encore service.\n"+
			"Offending files:\n  %s",
		encoreEtImportPath,
		strings.Join(offenders, "\n  "),
	)
}
