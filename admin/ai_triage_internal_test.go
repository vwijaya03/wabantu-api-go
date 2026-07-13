package admin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTriageJobIDFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/internal/ai-triage/jobs/1cd46c91-0d57-4ce1-acd8-3f1713fa7dad", "1cd46c91-0d57-4ce1-acd8-3f1713fa7dad"},
		{"/api/v1/internal/ai-triage/jobs/1cd46c91-0d57-4ce1-acd8-3f1713fa7dad/complete", "1cd46c91-0d57-4ce1-acd8-3f1713fa7dad"},
		{"/api/v1/internal/ai-triage/jobs/", ""},
		{"/api/v1/internal/ai-triage/jobs", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.SetPathValue("id", "")
		got := triageJobIDFromPath(req)
		if got != tc.want {
			t.Fatalf("path %q: got %q want %q", tc.path, got, tc.want)
		}
	}
}

func TestTriageJobIDFromPath_prefersPathValue(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/internal/ai-triage/jobs/wrong", nil)
	req.SetPathValue("id", "from-path-value")
	if got := triageJobIDFromPath(req); got != "from-path-value" {
		t.Fatalf("got %q want from-path-value", got)
	}
}

func TestTriageJobIDFromPath_stripsCompleteSuffix(t *testing.T) {
	req := httptest.NewRequest("POST", "/jobs/"+strings.Repeat("a", 36)+"/complete", nil)
	got := triageJobIDFromPath(req)
	if got != strings.Repeat("a", 36) {
		t.Fatalf("got %q", got)
	}
}
