package realtime

import (
	"os"
	"strings"
	"testing"
)

// Static guard on firestore.rules (no emulator available): tenant-scoped reads, no client writes.
func TestFirestoreRulesShape(t *testing.T) {
	b, err := os.ReadFile("../../firestore.rules")
	if err != nil {
		t.Fatal(err)
	}
	r := string(b)
	for _, want := range []string{
		"match /tenants/{tenantId}/{document=**}",
		"request.auth.token.tenantId == tenantId",
		"allow write: if false;",
		"allow read, write: if false;",
	} {
		if !strings.Contains(r, want) {
			t.Fatalf("firestore.rules missing %q", want)
		}
	}
	if strings.Contains(r, "allow write: if true") || strings.Contains(r, "allow read: if true") {
		t.Fatal("open rule")
	}
}
