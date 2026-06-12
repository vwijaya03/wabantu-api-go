package ai

import (
	"strings"
)

// Variasi bahasa WA Indonesia untuk generator test.
func stylePhrase(base, style string) string {
	base = strings.TrimSpace(base)
	switch style {
	case "formal":
		if !strings.HasPrefix(strings.ToLower(base), "selamat") {
			return "Selamat siang kak, " + base
		}
		return base
	case "informal":
		return strings.ToLower(base) + " dong"
	case "typo":
		return applyTypoMutations(base)
	case "slang":
		return applySlangMutations(base)
	case "indo_english":
		return applyIndoEnglishMutations(base)
	case "regional":
		return applyRegionalMutations(base)
	default:
		return base
	}
}

var waLanguageStyles = []string{"neutral", "formal", "informal", "typo", "slang", "indo_english", "regional"}

func applyTypoMutations(s string) string {
	r := strings.NewReplacer(
		"boxer", "boxr",
		"paket", "pket",
		"berapa", "brp",
		"harga", "harg",
		"abon", "abonn",
		"mono spot", "mono spott",
		"ukuran", "ukran",
		"transfer", "trnsfer",
	)
	return r.Replace(strings.ToLower(s))
}

func applySlangMutations(s string) string {
	low := strings.ToLower(s)
	low = strings.ReplaceAll(low, "saya", "gw")
	low = strings.ReplaceAll(low, "mau", "pengen")
	low = strings.ReplaceAll(low, "tidak", "ga")
	low = strings.ReplaceAll(low, "bang", "bang")
	if !strings.Contains(low, "bang") && !strings.Contains(low, "?") {
		low += " bang"
	}
	return low
}

func applyIndoEnglishMutations(s string) string {
	low := strings.ToLower(s)
	repl := []struct{ from, to string }{
		{"harga", "price"},
		{"berapa", "how much"},
		{"ukuran", "size"},
		{"pesan", "order"},
		{"beli", "buy"},
		{"katalog", "catalog"},
		{"rekomendasi", "recommendation"},
	}
	for _, r := range repl {
		if strings.Contains(low, r.from) {
			return r.to + " " + low
		}
	}
	return "hi kak, " + low
}

func applyRegionalMutations(s string) string {
	low := strings.ToLower(s)
	if strings.Contains(low, "?") {
		return "punten mas, " + low
	}
	if strings.Contains(low, "ada") {
		return strings.ReplaceAll(low, "ada", "ado")
	}
	if strings.Contains(low, "tidak") {
		return strings.ReplaceAll(low, "tidak", "dak")
	}
	return "monggo mas, " + low
}

func baseOrderAbon(qty int, step string) *orderState {
	return &orderState{
		Step:          step,
		CatalogItemID: "abon-500g",
		ProductName:   "Abon Sapi 500G",
		Qty:           qty,
		UnitPrice:     35000,
		SellUnit:      "pcs",
	}
}

func baseOrderBoxer(qty int, step string) *orderState {
	return &orderState{
		Step:          step,
		CatalogItemID: "boxer-mono-l",
		ProductName:   "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L",
		Qty:           qty,
		UnitPrice:     56900,
		SellUnit:      "pcs",
		Size:          "L",
	}
}
