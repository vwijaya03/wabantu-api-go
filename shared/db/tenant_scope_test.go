package db

import (
	"context"
	"database/sql"
	"testing"
)

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

func TestQualifySQLPreservesStringLiterals(t *testing.T) {
	sch := SchemaSQL{Schema: "t_omah_apparel"}
	in := `INSERT INTO message (conversation_id, external_id, direction, author, type, body, metadata, status)
		 VALUES ($1, $2, 'in', 'contact', $3, $4, $5::jsonb, 'delivered')`
	out := QualifySQL(sch, in)
	if contains(out, `"t_omah_apparel"."contact"`) && contains(out, `'contact'`) == false {
		t.Fatalf("QualifySQL() must not rewrite string literal 'contact': %q", out)
	}
	if !contains(out, `'contact'`) {
		t.Fatalf("QualifySQL() = %q, want author literal 'contact' preserved", out)
	}
	if !contains(out, `"t_omah_apparel"."message"`) {
		t.Fatalf("QualifySQL() = %q, want message table qualified", out)
	}
}

func TestQualifySQLOrderTableLeftJoin(t *testing.T) {
	sch := SchemaSQL{Schema: "t_omah_apparel"}
	in := `LEFT JOIN "order" o ON o.payment_proof_message_id = m.id AND o.deleted_at IS NULL`
	out := QualifySQL(sch, in)
	want := `LEFT JOIN "t_omah_apparel"."order" o ON o.payment_proof_message_id = m.id AND o.deleted_at IS NULL`
	if out != want {
		t.Fatalf("QualifySQL() = %q, want %q", out, want)
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

func TestTenantScope_BeginTxWithoutPoolFails(t *testing.T) {
	ts := TenantScope{
		Q:   stdQuerier{q: &sql.DB{}},
		Sch: SchemaSQL{Schema: "t_demo"},
	}
	_, err := ts.BeginTx(context.Background(), nil)
	if err == nil {
		t.Fatal("expected BeginTx error when pool is not set")
	}
}
