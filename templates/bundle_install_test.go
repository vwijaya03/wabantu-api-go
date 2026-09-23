package templates

import "testing"

func TestSurfacesForKindBundle(t *testing.T) {
	s := surfacesForKind("bundle")
	if len(s) != 2 || s[0] != "chatbot" || s[1] != "storefront" {
		t.Fatalf("unexpected surfaces: %v", s)
	}
}
