package ai

import "testing"

func TestIsRecommendationRequest_productNameFalsePositive(t *testing.T) {
	cases := []string{
		"LOL Best Seller 1 lusin ya ukuran L",
		`mau buat pesanan baru
1. LOL Best Seller 1 lusin ya ukuran L`,
		"beli LOL Best Seller 2 pcs",
	}
	for _, msg := range cases {
		if IsRecommendationRequest(msg) {
			t.Fatalf("IsRecommendationRequest(%q) should be false", msg)
		}
	}
	positive := []string{
		"minta rekomendasi best seller dong",
		"ada produk best seller nggak",
		"yang best seller apa ya",
	}
	for _, msg := range positive {
		if !IsRecommendationRequest(msg) {
			t.Fatalf("IsRecommendationRequest(%q) should be true", msg)
		}
	}
}
