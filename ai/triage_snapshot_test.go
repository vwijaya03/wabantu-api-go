package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSimulatorSnapshot_roundTrip(t *testing.T) {
	src := newOmahSimulator()
	snap := SimulatorToSnapshot(src, "t_omah_apparel")
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if snap.TenantSchema != "t_omah_apparel" {
		t.Fatalf("tenantSchema = %q", snap.TenantSchema)
	}
	if len(snap.Catalog) == 0 {
		t.Fatal("expected catalog items in snapshot")
	}

	restored, err := SimulatorFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Profile.BusinessName != src.Profile.BusinessName {
		t.Fatalf("profile name = %q want %q", restored.Profile.BusinessName, src.Profile.BusinessName)
	}
	if len(restored.Catalog) != len(src.Catalog) {
		t.Fatalf("catalog len = %d want %d", len(restored.Catalog), len(src.Catalog))
	}

	lit, err := FormatSnapshotGoConst(snap)
	if err != nil {
		t.Fatal(err)
	}
	code := GenerateRegressionCases([]TriageMismatch{{
		InboundID:    "abc",
		UserText:     "halo",
		ExpectedPath: PathGreeting,
	}}, "t_omah_apparel", snap)
	if !strings.Contains(code, "const triageAutoGenSnapshotJSON = "+lit) {
		t.Fatalf("missing snapshot const in generated code:\n%s", code)
	}

	fromJSON, err := SimulatorFromSnapshotJSON(mustUnquoteGoString(t, lit))
	if err != nil {
		t.Fatal(err)
	}
	if fromJSON.Profile.BusinessName != src.Profile.BusinessName {
		t.Fatalf("json profile = %q", fromJSON.Profile.BusinessName)
	}
}

func TestGenerateRegressionCases_includesSnapshot(t *testing.T) {
	code := GenerateRegressionCases(nil, "t_demo", SimulatorToSnapshot(newOmahSimulator(), "t_demo"))
	if !strings.Contains(code, "triageAutoGenSnapshotJSON") {
		t.Fatalf("expected snapshot const: %s", code)
	}
}

func mustUnquoteGoString(t *testing.T, lit string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal([]byte(lit), &s); err != nil {
		t.Fatal(err)
	}
	return s
}
