package usage

import (
	"testing"

	"encore.app/wabantu/shared/llm"
)

// Every model the admin API can save must have an explicit price. Missing rows used to
// fall through to the Sonnet default and mis-state the estimate.
func TestModelAllowlistHasPricing(t *testing.T) {
	if len(llm.ModelAllowlist) == 0 {
		t.Fatal("ModelAllowlist is empty")
	}
	for provider, models := range llm.ModelAllowlist {
		if len(models) == 0 {
			t.Errorf("provider %s has no models", provider)
		}
		for _, model := range models {
			if _, ok := pricingTable[model]; !ok {
				t.Errorf("model %q (provider %s) has no pricingTable entry", model, provider)
			}
		}
	}
}
