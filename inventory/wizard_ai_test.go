package inventory

import "testing"

func TestParseAIWizardRecommendation(t *testing.T) {
	raw := `{"method":"fifo","reason":"Salmon frozen perlu keluar dulu.","owner_summary":"Stok lama dijual lebih dulu."}`
	got, err := parseAIWizardRecommendation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != CostingFIFO || got.Reason == "" || got.OwnerSummary == "" {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestParseAIWizardRecommendationMarkdownWrapped(t *testing.T) {
	raw := "```json\n{\"method\":\"average\",\"reason\":\"UMKM seragam\",\"owner_summary\":\"Rata-rata\"}\n```"
	got, err := parseAIWizardRecommendation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != CostingAverage {
		t.Fatalf("method=%q", got.Method)
	}
}
