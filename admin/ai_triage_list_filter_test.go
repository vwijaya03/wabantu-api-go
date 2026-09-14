package admin

import (
	"strings"
	"testing"
)

func TestParseTriageUUIDList(t *testing.T) {
	ids, err := parseTriageUUIDList([]string{
		"0f619251-d61c-40cf-ae67-3809c3e3f4d6",
		" 0f619251-d61c-40cf-ae67-3809c3e3f4d6 ",
	}, 50)
	if err != nil || len(ids) != 1 {
		t.Fatalf("dedupe: %v %v", ids, err)
	}
	if _, err := parseTriageUUIDList([]string{"undefined"}, 50); err == nil {
		t.Fatal("undefined must fail")
	}
	if _, err := parseTriageUUIDList(nil, 50); err == nil {
		t.Fatal("empty must fail")
	}
}

func TestAppendIncidentReviewFilter_defaultKeepsDismissed(t *testing.T) {
	q, args, n := appendIncidentReviewFilter("SELECT id FROM ai_triage_incident WHERE 1=1", nil, 1, "")
	if strings.Contains(q, "<> 'dismissed'") {
		t.Fatalf("default list must keep dismissed: %s", q)
	}
	if len(args) != 0 || n != 1 {
		t.Fatalf("args=%v n=%d", args, n)
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
