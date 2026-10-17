package usage

import (
	"testing"

	"encore.app/wabantu/shared/llm"
)

// Every model the admin API can save must have an explicit price. A model without a row is
// reported as unknown (cost 0), never priced as Sonnet.
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

func TestUnknownModelIsNotPricedAsSonnet(t *testing.T) {
	cost, known := EstimateTokenCostKnown("gemini-9-imaginary", 1_000_000, 1_000_000)
	if known || cost != 0 {
		t.Fatalf("unknown model: cost=%v known=%v, want 0/false", cost, known)
	}
	if got := EstimateTokenCost("gemini-9-imaginary", 1_000_000, 1_000_000); got != 0 {
		t.Fatalf("EstimateTokenCost of an unknown model = %v, want 0 (never the Sonnet price)", got)
	}
	if got := EstimateTokenCost("", 1000, 1000); got != 0 {
		t.Fatalf("empty model = %v, want 0", got)
	}
	if ModelPriced("gemini-9-imaginary") || !ModelPriced("claude-sonnet-4-6") {
		t.Fatal("ModelPriced must follow pricingTable")
	}
}

// Prices from https://ai.google.dev/gemini-api/docs/pricing, Standard paid tier, text.
func TestGeminiStandardPrices(t *testing.T) {
	cases := []struct {
		model   string
		in, out float64
	}{
		{"gemini-3.6-flash", 0.75, 3.75},
		{"gemini-3.8-flash", 0.75, 3.75},
		{"gemini-3.5-flash-lite", 0.30, 2.50},
		{"gemini-3.1-flash-lite", 0.25, 1.50},
		{"gemini-3.1-pro-preview", 2.00, 12.00},
	}
	for _, c := range cases {
		p, ok := pricingTable[c.model]
		if !ok || p.InputPer1M != c.in || p.OutputPer1M != c.out {
			t.Errorf("%s: got %+v (present=%v), want in=%v out=%v", c.model, p, ok, c.in, c.out)
		}
	}
	// 1M in + 1M out on gemini-3.6-flash = 0.75 + 3.75.
	if got, known := EstimateTokenCostKnown("gemini-3.6-flash", 1_000_000, 1_000_000); !known || got != 4.50 {
		t.Fatalf("3.6 flash 1M/1M = %v (known=%v), want 4.50", got, known)
	}
}
