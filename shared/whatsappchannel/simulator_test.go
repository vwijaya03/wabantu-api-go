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

func TestIsSimulatorPhone(t *testing.T) {
	for p, want := range map[string]bool{
		"620001234567890": true, "+620001234567890": true,
		"6281234567890": false, "62000123456789": false, "62000123456789a": false, "": false,
	} {
		if IsSimulatorPhone(p) != want {
			t.Errorf("%q", p)
		}
	}
}

func TestSimulatorHiddenFromListsAndMetaQueries(t *testing.T) {
	if ListableChannelSQL != "provider <> 'simulator'" {
		t.Fatal(ListableChannelSQL)
	}
	if !strings.Contains(ConnectedTokenQuerySuffix, "provider <> 'simulator'") {
		t.Fatal("Meta channel picker must exclude simulator")
	}
}
