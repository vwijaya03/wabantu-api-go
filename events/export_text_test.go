package events

import "testing"

func TestPdfSafeText_stripsProblematicUnicode(t *testing.T) {
	in := "Pasien — keluhan… emoji \U0001F600"
	out := pdfSafeText(in)
	if out == in {
		t.Fatal("expected normalization")
	}
	if containsRune(out, '\u2014') || containsRune(out, '\u2026') {
		t.Fatalf("dash/ellipsis not replaced: %q", out)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestPatientExportColumns_hidden(t *testing.T) {
	hidden := parsePatientHiddenColumns([]string{"complaint", "status", "invalid"})
	cols := patientExportColumns(hidden)
	keys := map[string]bool{}
	for _, c := range cols {
		keys[c.Key] = true
	}
	if !keys["name"] || !keys["no"] {
		t.Fatal("name/no must always be visible")
	}
	if keys["complaint"] || keys["status"] {
		t.Fatal("hidden columns should be omitted")
	}
	if keys["therapy"] == false {
		t.Fatal("therapy should remain visible")
	}
}
