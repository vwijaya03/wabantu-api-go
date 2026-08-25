package db

import "testing"

func TestQualify(t *testing.T) {
	got := Qualify("t_demo", "contact")
	want := `"t_demo"."contact"`
	if got != want {
		t.Fatalf("Qualify() = %q, want %q", got, want)
	}
}

func TestQualifySQL(t *testing.T) {
	sch := SchemaSQL{Schema: "t_demo"}
	in := `SELECT id FROM contact c JOIN message m ON m.conversation_id = c.id`
	out := QualifySQL(sch, in)
	if out == in {
		t.Fatal("expected table names to be qualified")
	}
	if want := `"t_demo"."contact"`; !containsAll(out, want, `"t_demo"."message"`) {
		t.Fatalf("QualifySQL() = %q", out)
	}
}

func TestQualifySQLOrderTableQuoted(t *testing.T) {
	sch := SchemaSQL{Schema: "t_omah_apparel"}
	in := `(SELECT o.id::text FROM "order" o WHERE o.payment_proof_message_id = m.id AND o.deleted_at IS NULL LIMIT 1)`
	out := QualifySQL(sch, in)
	want := `(SELECT o.id::text FROM "t_omah_apparel"."order" o WHERE o.payment_proof_message_id = m.id AND o.deleted_at IS NULL LIMIT 1)`
	if out != want {
		t.Fatalf("QualifySQL() = %q, want %q", out, want)
	}
	if contains(out, `"order""`) {
		t.Fatalf("QualifySQL() must not produce double-quoted order table: %q", out)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
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
