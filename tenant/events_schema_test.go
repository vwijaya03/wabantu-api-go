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
