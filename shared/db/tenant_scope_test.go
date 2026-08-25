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
