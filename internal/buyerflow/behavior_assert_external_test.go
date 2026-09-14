package buyerflow_test

import (
	"testing"

	"encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/triageassert"
)

// Guards the GHA generated test shape: package buyerflow_test importing
// triageassert must compile (package buyerflow + triageassert is an import cycle).
func TestExternalPackage_behaviorAssertCompiles(t *testing.T) {
	sim := buyerflow.NewOmahSimulator()
	out := sim.Turn("halo")
	if err := triageassert.CheckTurn(out, triageassert.CommerceSpec{}); err != nil {
		t.Fatal(err)
	}
}
