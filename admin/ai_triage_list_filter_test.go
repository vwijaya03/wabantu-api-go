package admin

import (
	"strings"
	"testing"
)

func TestAppendIncidentReviewFilter_excludesDismissedByDefault(t *testing.T) {
	q, args, n := appendIncidentReviewFilter("SELECT id FROM ai_triage_incident WHERE 1=1", nil, 1, "")
	if !strings.Contains(q, "review_status <> 'dismissed'") {
		t.Fatalf("default list must hide dismissed: %s", q)
	}
	if len(args) != 0 || n != 1 {
		t.Fatalf("no extra args, got args=%v n=%d", args, n)
	}
}

func TestAppendIncidentReviewFilter_exactStatus(t *testing.T) {
	q, args, n := appendIncidentReviewFilter("SELECT id FROM ai_triage_incident WHERE 1=1", nil, 1, "dismissed")
	if !strings.Contains(q, "review_status = $1") {
		t.Fatalf("got %s", q)
	}
	if len(args) != 1 || args[0] != "dismissed" || n != 2 {
		t.Fatalf("args=%v n=%d", args, n)
	}
}
