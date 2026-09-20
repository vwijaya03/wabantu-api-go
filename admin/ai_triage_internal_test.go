package admin

import (
	"errors"
	"net/http/httptest"
	"testing"

	"encore.dev/beta/errs"
)

func TestTriageJobIDFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/internal/ai-triage/behavior-jobs/f7168625-1823-4dc2-8eb2-d6d7e248b2b4", "f7168625-1823-4dc2-8eb2-d6d7e248b2b4"},
		{"/api/v1/internal/ai-triage/behavior-jobs/f7168625-1823-4dc2-8eb2-d6d7e248b2b4/complete", "f7168625-1823-4dc2-8eb2-d6d7e248b2b4"},
		{"/api/v1/internal/ai-triage/behavior-jobs/", ""},
		{"/api/v1/internal/ai-triage/jobs/1cd46c91-0d57-4ce1-acd8-3f1713fa7dad", ""},
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
	const id = "1cd46c91-0d57-4ce1-acd8-3f1713fa7dad"
	req := httptest.NewRequest("GET", "/api/v1/internal/ai-triage/behavior-jobs/wrong", nil)
	req.SetPathValue("id", id)
	if got := triageJobIDFromPath(req); got != id {
		t.Fatalf("got %q want %s", got, id)
	}
}

func TestTriageJobIDFromPath_ignoresPathValueComplete(t *testing.T) {
	const id = "f7168625-1823-4dc2-8eb2-d6d7e248b2b4"
	req := httptest.NewRequest("POST", "/api/v1/internal/ai-triage/behavior-jobs/"+id+"/complete", nil)
	req.SetPathValue("id", "complete")
	if got := triageJobIDFromPath(req); got != id {
		t.Fatalf("PathValue complete must fall back to UUID in URL, got %q", got)
	}
}

func TestTriageJobIDFromPath_stripsCompleteSuffix(t *testing.T) {
	const id = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	req := httptest.NewRequest("POST", "/behavior-jobs/"+id+"/complete", nil)
	got := triageJobIDFromPath(req)
	if got != id {
		t.Fatalf("got %q", got)
	}
}

func TestRequireTriageUUID(t *testing.T) {
	const id = "f7168625-1823-4dc2-8eb2-d6d7e248b2b4"
	got, err := requireTriageUUID(id)
	if err != nil || got != id {
		t.Fatalf("valid uuid: got %q err %v", got, err)
	}
	for _, s := range []string{"", "undefined", "null", "complete", "not-a-uuid"} {
		got, err := requireTriageUUID(s)
		if got != "" {
			t.Fatalf("%q: want empty id, got %q", s, got)
		}
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.InvalidArgument {
			t.Fatalf("%q: want InvalidArgument, got %v", s, err)
		}
	}
}
