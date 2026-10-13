package whatsappchannel

import (
	"strings"
	"testing"
)

func TestSimulatorHelpers(t *testing.T) {
	if !IsSimulator("simulator") || IsSimulator("meta_cloud") || IsSimulator("") {
		t.Fatal("IsSimulator")
	}
	a, b := SimulatorExternalID("ai"), SimulatorExternalID("ai")
	if a == b || !strings.HasPrefix(a, "sim-ai-") || len(a) > 128 {
		t.Fatalf("%s %s", a, b)
	}
}
