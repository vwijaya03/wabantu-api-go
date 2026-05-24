package finance

import "strings"

// Satuan kepemilikan & harga per instrumen (praktik pasar Indonesia + umum).
// Saham IDX: lot + harga/lembar. Emas retail: gram. RD: unit. Kripto: per koin.

func defaultUnitNameForType(assetType string) string {
	switch assetType {
	case "stock":
		return "lot"
	case "crypto":
		return "coin"
	case "gold":
		return "gram"
	case "mutual_fund":
		return "unit"
	default:
		return "unit"
	}
}

func defaultUnitMultiplier(assetType, unitName string) float64 {
	u := strings.ToLower(strings.TrimSpace(unitName))
	switch assetType {
	case "stock":
		if u == "lot" {
			return 100
		}
		return 1
	default:
		return 1
	}
}

func defaultPriceUnitName(assetType, unitName string) string {
	u := strings.ToLower(strings.TrimSpace(unitName))
	switch assetType {
	case "stock":
		if u == "lot" {
			return "lembar"
		}
		if u == "lembar" {
			return "lembar"
		}
	case "crypto":
		if u == "coin" {
			return "coin"
		}
	case "gold":
		switch u {
		case "gram", "oz", "kg":
			return u
		}
	case "mutual_fund":
		if u == "unit" {
			return "unit"
		}
	}
	if u == "" {
		return defaultUnitNameForType(assetType)
	}
	return strings.TrimSpace(unitName)
}
