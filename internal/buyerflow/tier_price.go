package buyerflow

// ApplyTierPrices overrides SellPrice with the buyer's tier price (catalog item id → price)
// and stamps PriceTier on every item. Items without a tier price keep sell_price (the
// "umum" fallback). Returns the number of overridden items. Prices ≤ 0 are ignored.
func ApplyTierPrices(items []CatalogItem, prices map[string]float64, tier string) int {
	n := 0
	for i := range items {
		items[i].PriceTier = tier
		if p, ok := prices[items[i].ID]; ok && p > 0 {
			items[i].SellPrice = p
			n++
		}
	}
	return n
}
