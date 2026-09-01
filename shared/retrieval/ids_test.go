package retrieval

import (
	"strings"
	"testing"
)

func TestKBVectorIDDeterministic(t *testing.T) {
	id := KBVectorID("abc", 3, 0)
	if id != "kb:abc:v3:c0" {
		t.Fatalf("unexpected id: %s", id)
	}
}

func TestContentHashStable(t *testing.T) {
	h1 := ContentHash("Q", "A")
	h2 := ContentHash("Q", "A")
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("unexpected hash: %s", h1)
	}
}

func TestNamespaceValidation(t *testing.T) {
	_, err := Namespace(TenantIdentity{TenantSchema: "bad"})
	if err == nil {
		t.Fatal("expected error for invalid namespace")
	}
	ns, err := Namespace(TenantIdentity{TenantSchema: "t_acme"})
	if err != nil || ns != "t_acme" {
		t.Fatalf("unexpected: %s %v", ns, err)
	}
}

func TestNamespaceRejectsInjectionChars(t *testing.T) {
	cases := []string{
		"t_a; DROP TABLE",
		"t_../other",
		"t_",
		"t_" + strings.Repeat("a", 61),
		"public",
	}
	for _, tc := range cases {
		if _, err := Namespace(TenantIdentity{TenantSchema: tc}); err == nil {
			t.Fatalf("expected error for namespace %q", tc)
		}
	}
}
