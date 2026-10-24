package importcsv

import (
	"net/url"
	"regexp"
	"testing"
)

var importJobIDPattern = regexp.MustCompile(`^imp_[0-9a-f]{32}$`)

func TestNewImportJobID_FormatAndURLSafe(t *testing.T) {
	id, err := newImportJobID()
	if err != nil {
		t.Fatal(err)
	}
	if !importJobIDPattern.MatchString(id) {
		t.Fatalf("jobId %q does not match %s", id, importJobIDPattern)
	}
	if url.PathEscape(id) != id {
		t.Fatalf("jobId %q is not URL-path safe", id)
	}
}

func TestNewImportJobID_Unique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id, err := newImportJobID()
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate jobId after %d ids: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}
