package retrieval

import "testing"

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
