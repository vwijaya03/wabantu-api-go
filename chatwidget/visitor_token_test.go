package chatwidget

import "testing"

func TestVisitorTokenRoundTrip(t *testing.T) {
	raw, hash, err := mintVisitorToken("test-secret", "omah-apparel", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || raw == "" {
		t.Fatal("empty token")
	}
	if !verifyVisitorToken("test-secret", "omah-apparel", "sess-1", raw) {
		t.Fatal("token should verify")
	}
	if verifyVisitorToken("test-secret", "omah-apparel", "sess-2", raw) {
		t.Fatal("wrong session must fail")
	}
}
