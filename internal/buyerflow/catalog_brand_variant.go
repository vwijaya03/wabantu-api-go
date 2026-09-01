package buyerflow

import (
	"fmt"
	"sort"
	"strings"

	"encore.app/wabantu/shared/retrieval"
)

var brandVariantSignals = []string{
	"varian", "variant", "berapa jenis", "berapa macam", "berapa varian",
	"ada berapa varian", "ada berapa jenis", "ada berapa macam",
	"variannya apa", "varian apa", "jenis apa saja", "macam apa saja",
	"tipe apa", "berapa tipe",
}

// IsBrandVariantInquiry — user asks how many variants a brand/product line has.
func IsBrandVariantInquiry(userText string, history []Message, catalog []CatalogItem) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || len(catalog) == 0 {
		return false
	}
	if isGeneralStoreCatalogQuestion(userText) || IsCatalogListQuestion(userText) {
		return false
	}
	if !hasBrandVariantSignal(text) {
		return false
	}
	return extractBrandToken(userText, history, catalog) != ""
}

func hasBrandVariantSignal(text string) bool {
	for _, s := range brandVariantSignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	if strings.Contains(text, "berapa") &&
		(strings.Contains(text, "varian") || strings.Contains(text, "jenis") ||
			strings.Contains(text, "macam") || strings.Contains(text, " ada")) {
		return true
	}
	return false
}

func normalizeBrandToken(tok string) string {
	tok = strings.ToLower(strings.TrimSpace(tok))
	switch tok {
	case "magi", "magie":
		return "maggi"
	default:
		return tok
	}
}

func extractBrandToken(userText string, history []Message, catalog []CatalogItem) string {
	if b := brandTokenFromText(userText, catalog); b != "" {
		return b
	}
	start := 0
	if len(history) > 6 {
		start = len(history) - 6
	}
	for i := len(history) - 1; i >= start; i-- {
		if b := brandTokenFromText(history[i].Body, catalog); b != "" {
			return b
		}
	}
	return ""
}

func brandTokenFromText(text string, catalog []CatalogItem) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return ""
	}
	noise := []string{
		"varian", "variant", "berapa", "jenis", "macam", "tipe", "ada", "apa", "saja",
		"yang", "nya", "kak", "dong", "deh", "nih", "itu", "ini", "?", "!", ".", ",",
	}
	for _, n := range noise {
		lower = strings.ReplaceAll(lower, n, " ")
	}
	for _, tok := range tokenize(lower) {
		if brand := fuzzyBrandTokenName(tok, catalog); brand != "" {
			return brand
		}
	}
	return ""
}

func fuzzyBrandTokenName(tok string, catalog []CatalogItem) string {
	tok = normalizeBrandToken(tok)
	if len(tok) < 3 {
		return ""
	}
	for _, it := range catalog {
		if catalogItemMatchesBrand(it, tok) {
			return tok
		}
	}
	return ""
}

func catalogItemMatchesBrand(it CatalogItem, brandToken string) bool {
	token := normalizeBrandToken(brandToken)
	if token == "" {
		return false
	}
	name := strings.ToLower(it.Name)
	if strings.Contains(name, token) {
		return true
	}
	code := strings.ToLower(strings.TrimSpace(it.ExternalCode))
	if code != "" && strings.Contains(code, token) {
		return true
	}
	family := strings.ToLower(inferProductFamily(it))
	return family != "" && strings.Contains(family, token)
}

func catalogItemsForBrand(brandToken string, catalog []CatalogItem) []CatalogItem {
	token := normalizeBrandToken(brandToken)
	var out []CatalogItem
	for _, it := range catalog {
		if catalogItemMatchesBrand(it, token) {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildBrandVariantListReply(formal bool, brandToken string, catalog []CatalogItem, max int) string {
	if max <= 0 || max > 10 {
		max = 10
	}
	items := catalogItemsForBrand(brandToken, catalog)
	if len(items) == 0 {
		return ""
	}
	return formatBrandVariantListBody(formal, brandToken, items, max)
}

func buildBrandVariantListFromVectorHits(formal bool, brandToken string, catalog []CatalogItem, hits []retrievalHit, max int) string {
	if max <= 0 || max > 10 {
		max = 10
	}
	token := normalizeBrandToken(brandToken)
	seen := map[string]struct{}{}
	var items []CatalogItem
	byID := map[string]CatalogItem{}
	for _, it := range catalog {
		byID[it.ID] = it
	}
	for _, h := range hits {
		id := h.entryID
		if id == "" {
			continue
		}
		it, ok := byID[id]
		if !ok || !catalogItemMatchesBrand(it, token) {
			continue
		}
		if _, dup := seen[it.ID]; dup {
			continue
		}
		seen[it.ID] = struct{}{}
		items = append(items, it)
	}
	if len(items) == 0 {
		return ""
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return formatBrandVariantListBody(formal, brandToken, items, max)
}

type retrievalHit struct {
	entryID string
}

func catalogVectorHitsEntryIDs(hits []retrieval.Hit) []retrievalHit {
	out := make([]retrievalHit, 0, len(hits))
	for _, h := range hits {
		id := retrieval.EntryIDFromHit(h)
		if id != "" {
			out = append(out, retrievalHit{entryID: id})
		}
	}
	return out
}

func formatBrandVariantListBody(formal bool, brandToken string, items []CatalogItem, max int) string {
	if len(items) == 0 {
		return ""
	}
	displayBrand := normalizeBrandToken(brandToken)
	if displayBrand != "" {
		displayBrand = strings.ToUpper(displayBrand[:1]) + displayBrand[1:]
	} else {
		displayBrand = "produk ini"
	}
	shown := items
	extra := 0
	if len(shown) > max {
		extra = len(shown) - max
		shown = shown[:max]
	}
	var head string
	if formal {
		head = fmt.Sprintf("Berikut varian %s yang tersedia:\n", displayBrand)
	} else {
		head = fmt.Sprintf("Ini varian %s yang ada ya kak:\n", displayBrand)
	}
	var lines []string
	for i, it := range shown {
		lines = append(lines, fmt.Sprintf("%d. %s\n%s", i+1, shortDisplayName(it.Name), formatCatalogPrice(&it)))
	}
	body := strings.Join(lines, "\n\n")
	if extra > 0 {
		body += fmt.Sprintf("\n\n…dan %d varian lainnya. Sebut nama varian yang kakak mau ya.", extra)
	} else {
		cta := "Mau varian yang mana? Sebut nama lengkapnya ya kak."
		if formal {
			cta = "Silakan sebut varian yang diinginkan."
		}
		body += "\n\n" + cta
	}
	return strings.TrimSpace(head + "\n" + body)
}

// tryBrandVariantReply handles "berapa varian Maggi?" style questions.
func tryBrandVariantReply(
	formal bool,
	userText string,
	history []Message,
	catalog []CatalogItem,
	vctx *CatalogVectorContext,
) (string, bool) {
	if !IsBrandVariantInquiry(userText, history, catalog) {
		return "", false
	}
	brand := extractBrandToken(userText, history, catalog)
	if brand == "" {
		return "", false
	}
	if reply := buildBrandVariantListReply(formal, brand, catalog, 10); reply != "" {
		return reply, true
	}
	if vctx != nil && len(vctx.Hits) > 0 {
		ids := catalogVectorHitsEntryIDs(vctx.Hits)
		if reply := buildBrandVariantListFromVectorHits(formal, brand, catalog, ids, 10); reply != "" {
			return reply, true
		}
	}
	return "", false
}
