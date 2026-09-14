package admin

import (
	"os"
	"strings"
	"testing"
)

func TestCompleteBehaviorJobSQL_castsRepeatedStatusParam(t *testing.T) {
	b, err := os.ReadFile("ai_triage_behavior_job.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "$2::varchar IN") {
		t.Fatal("completeBehaviorJob must cast $2; untyped $2 IN (...) is PostgreSQL 42P08")
	}
}
