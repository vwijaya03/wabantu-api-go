package tenant

import (
	"strings"
	"testing"
)

func TestEventsSchemaPatchSQLGuardsOptionalContact(t *testing.T) {
	if !strings.Contains(eventsSchemaPatchSQL, "$evt_contact$") {
		t.Fatal("events patch must guard optional contact table")
	}
	if strings.Contains(eventsSchemaPatchSQL, "contact_id UUID REFERENCES contact(id)") {
		t.Fatal("inline REFERENCES on ADD COLUMN breaks tenants without inbox module")
	}
}

func TestEventsSchemaPatchSQLMealNotesLenConstraint(t *testing.T) {
	const name = "evt_event_person_meal_notes_len_chk"
	if n := strings.Count(eventsSchemaPatchSQL, "CONSTRAINT "+name+" CHECK (char_length(meal_notes) <= 200)"); n != 1 {
		t.Fatalf("CREATE TABLE evt_event_person must declare %s once, got %d", name, n)
	}
	for _, want := range []string{
		"$evt_meal_notes_len$",
		"conrelid = to_regclass('evt_event_person')",
		"ADD CONSTRAINT " + name,
		"CHECK (char_length(meal_notes) <= 200) NOT VALID",
		"EXCEPTION WHEN duplicate_object",
	} {
		if !strings.Contains(eventsSchemaPatchSQL, want) {
			t.Fatalf("events patch missing %q (idempotent NOT VALID constraint for old tenants)", want)
		}
	}
}
