package inbox

import (
	"testing"

	appdb "encore.app/wabantu/shared/db"
)

func TestMessageListSelectSQLQualifySQLOrderJoin(t *testing.T) {
	sch := appdb.SchemaSQL{Schema: "t_omah_apparel"}
	out := appdb.QualifySQL(sch, messageListSelectSQL)
	want := `LEFT JOIN "t_omah_apparel"."order" o ON o.payment_proof_message_id = m.id AND o.deleted_at IS NULL`
	if !containsSubstring(out, want) {
		t.Fatalf("QualifySQL() missing qualified order join:\n%s", out)
	}
	if !containsSubstring(out, `"t_omah_apparel"."message"`) {
		t.Fatalf("QualifySQL() missing qualified message table:\n%s", out)
	}
	if containsSubstring(out, `"order""`) {
		t.Fatalf("QualifySQL() must not double-quote order table: %q", out)
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfSubstring(s, sub) >= 0)
}

func indexOfSubstring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
