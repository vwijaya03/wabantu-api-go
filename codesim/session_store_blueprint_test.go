package codesim

import "testing"

func TestResolveExamBlueprintSlug_keepsExplicitSlug(t *testing.T) {
	if got := resolveExamBlueprintSlug("tendem-hard-07"); got != "tendem-hard-07" {
		t.Fatalf("got %q want tendem-hard-07", got)
	}
	if got := resolveExamBlueprintSlug("custom-blueprint"); got != "custom-blueprint" {
		t.Fatalf("got %q want custom-blueprint", got)
	}
}

func TestResolveExamBlueprintSlug_randomizesDefault(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		got := resolveExamBlueprintSlug(defaultExamBlueprintSlug)
		if got == defaultExamBlueprintSlug {
			t.Fatalf("default slug should resolve to tendem-hard-*")
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected multiple hard slugs over 40 rolls, got %d", len(seen))
	}
}
