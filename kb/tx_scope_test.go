package kb

import (
	"context"
	"testing"

	appdb "encore.app/wabantu/shared/db"
)

func TestTxnQualifiesKnowledgeBaseInsert(t *testing.T) {
	sch := appdb.SchemaSQL{Schema: "t_demo"}
	raw := `INSERT INTO knowledge_base_entry (question) VALUES ('x')`
	got := appdb.QualifySQL(sch, raw)
	want := `"t_demo"."knowledge_base_entry"`
	if got == raw || !contains(got, want) {
		t.Fatalf("QualifySQL() = %q want qualified %q", got, want)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Avoid unused import if test file only uses QualifySQL via db package.
var _ = context.Background
