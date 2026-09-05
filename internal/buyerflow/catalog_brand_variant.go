package buyerflow

import (
	"fmt"
	"regexp"
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
	case "abo", "abonn":
		return "abon"
	case "magi", "magie":
		return "maggi"
	case "cadburry", "cadburi", "cadbure", "cadbery", "cadburri", "cadburie":
		return "cadbury"
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
	if len(tok) < 5 {
		return ""
	}
	best := ""
	bestDist := 99
	seen := map[string]struct{}{}
	for _, it := range catalog {
		nameToks := tokenize(it.Name)
		if len(nameToks) == 0 {
			continue
		}
		cand := normalizeBrandToken(nameToks[0])
		if len(cand) < 4 {
			continue
		}
		if _, dup := seen[cand]; dup {
			continue
		}
		seen[cand] = struct{}{}
		if !brandTypoMatch(tok, cand) {
			continue
		}
		d := editDistance(tok, cand)
		if d < bestDist {
			bestDist = d
			best = cand
		}
	}
	return best
}

func brandTypoMatch(user, brand string) bool {
	user = normalizeBrandToken(user)
	brand = normalizeBrandToken(brand)
	if user == brand {
		return true
	}
	if len(user) < 5 || len(brand) < 4 {
		return false
	}
	if fuzzyTokenPrefixMatch(user, brand) && absInt(len(user)-len(brand)) <= 2 {
		return true
	}
	if minInt(len(user), len(brand)) >= 6 && editDistance(user, brand) <= 2 {
		return true
	}
	return false
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = del
			if ins < curr[j] {
				curr[j] = ins
			}
			if sub < curr[j] {
				curr[j] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
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

var brandDistinctiveStop = map[string]bool{
	"bumbu": true, "ayam": true, "goreng": true, "the": true, "and": true,
	"pcs": true, "gram": true, "sapi": true, "biskuit": true,
}

func distinctiveNameTokens(name, brand string) []string {
	brand = normalizeBrandToken(brand)
	var out []string
	for _, tok := range tokenize(strings.ToLower(name)) {
		tok = strings.Trim(tok, "-()")
		if tok == brand || brandDistinctiveStop[tok] {
			continue
		}
		if len(tok) < 3 && !isNumericSizeToken(tok) && !isApparelSizeToken(tok) && !isMultiCharSizeToken(tok) && !isCapacityOrVolumeToken(tok) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func isCapacityOrVolumeToken(tok string) bool {
	tok = strings.ToLower(strings.TrimSpace(tok))
	if tok == "" {
		return false
	}
	return capacityOrVolumeRe.MatchString(tok)
}

func isMultiCharSizeToken(tok string) bool {
	switch strings.ToLower(strings.TrimSpace(tok)) {
	case "xxxl", "3xl", "xxl", "xl", "xs", "4xl", "5xl":
		return true
	default:
		return false
	}
}

func isNumericSizeToken(tok string) bool {
	if len(tok) < 2 || len(tok) > 4 {
		return false
	}
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func uniqueBrandSKUFromText(userText string, catalog []CatalogItem) *CatalogItem {
	brand := brandTokenFromText(userText, catalog)
	if brand == "" {
		return nil
	}
	items := catalogItemsForBrand(brand, catalog)
	if len(items) < 2 {
		return nil
	}
	if hit := uniqueItemByDistinctiveUserTokens(userText, brand, items); hit != nil {
		return hit
	}
	if sz, _ := parseSizeAndColor(userText); sz != "" {
		var sized []CatalogItem
		for _, it := range items {
			if catalogItemHasSize(it, sz) {
				sized = append(sized, it)
			}
		}
		if len(sized) == 1 {
			hit := sized[0]
			return &hit
		}
	}
	return nil
}

// uniqueSizedSKUFromText picks a catalog row when the buyer named a size that
// uniquely identifies one high-overlap SKU (Hello Kitty L vs boxer L).
func uniqueSizedSKUFromText(userText string, catalog []CatalogItem) *CatalogItem {
	sz, _ := parseSizeAndColor(userText)
	if sz == "" || len(catalog) == 0 {
		return nil
	}
	tokens := tokenize(userText)
	if len(tokens) == 0 {
		return nil
	}
	var best *CatalogItem
	var bestScore float64
	tied := false
	for i := range catalog {
		it := &catalog[i]
		if !catalogItemHasSize(*it, sz) {
			continue
		}
		score := overlapScore(tokens, tokenize(it.Name))
		if score < 0.12 {
			continue
		}
		if best == nil || score > bestScore {
			best = it
			bestScore = score
			tied = false
			continue
		}
		if score == bestScore {
			tied = true
		}
	}
	if best == nil || tied {
		return nil
	}
	return best
}

func itemsHitByUniqueDistinctiveTokens(userText, brand string, items []CatalogItem) []CatalogItem {
	userToks := map[string]struct{}{}
	for _, tok := range tokenize(strings.ToLower(userText)) {
		userToks[tok] = struct{}{}
	}
	owners := map[string][]int{}
	for i, it := range items {
		seen := map[string]struct{}{}
		for _, tok := range distinctiveNameTokens(it.Name, brand) {
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			owners[tok] = append(owners[tok], i)
		}
	}
	hitIDs := map[string]struct{}{}
	var unique []CatalogItem
	for tok := range userToks {
		idxs := owners[tok]
		if len(idxs) != 1 {
			continue
		}
		it := items[idxs[0]]
		if _, ok := hitIDs[it.ID]; ok {
			continue
		}
		hitIDs[it.ID] = struct{}{}
		unique = append(unique, it)
	}
	return unique
}

func uniqueItemByDistinctiveUserTokens(userText, brand string, items []CatalogItem) *CatalogItem {
	unique := itemsHitByUniqueDistinctiveTokens(userText, brand, items)
	if len(unique) != 1 {
		return nil
	}
	hit := unique[0]
	return &hit
}

func catalogItemHasSize(it CatalogItem, size string) bool {
	sz := strings.ToUpper(strings.TrimSpace(size))
	if sz == "" {
		return false
	}
	if got := extractSizeFromProductName(it.Name); got != "" {
		return got == sz
	}
	for _, tok := range tokenize(it.Name) {
		if strings.EqualFold(tok, sz) {
			return true
		}
	}
	return false
}

func lexicalBrandAmbiguous(userText string, catalog []CatalogItem) bool {
	if uniqueBrandSKUFromText(userText, catalog) != nil {
		return false
	}
	brand := brandTokenFromText(userText, catalog)
	if brand == "" {
		return false
	}
	return len(catalogItemsForBrand(brand, catalog)) >= 2
}

func orderLexicalBrandPickerReply(formal bool, userText string, catalog []CatalogItem) (string, bool) {
	if !lexicalBrandAmbiguous(userText, catalog) {
		return "", false
	}
	brand := brandTokenFromText(userText, catalog)
	if brand == "" {
		return "", false
	}
	reply := buildBrandVariantListReply(formal, brand, catalog, 10)
	if reply == "" {
		return "", false
	}
	return reply, true
}

func shouldReviseToSiblingSKU(st OrderState, userText string, catalog []CatalogItem) bool {
	if strings.TrimSpace(st.CatalogItemID) == "" {
		return false
	}
	if st.HasMultiItems() && len(st.Items) > 1 {
		return false
	}
	match := resolveOrderProductMatch(userText, nil, catalog, nil)
	if match == nil || match.ID == st.CatalogItemID {
		return false
	}
	var current *CatalogItem
	for i := range catalog {
		if catalog[i].ID == st.CatalogItemID {
			current = &catalog[i]
			break
		}
	}
	if current == nil {
		return false
	}
	return sameProductLine(*current, *match)
}

// capacityOrVolumeRe strips gadget/beauty variant suffixes (128GB, 30ml, 25W)
// so product-line comparison is domain-agnostic — not F&B-only.
var capacityOrVolumeRe = regexp.MustCompile(`(?i)\s*\d+(\.\d+)?\s*(gb|tb|mb|ml|mah|w|watt)\b`)

var trailingShadeColorRe = regexp.MustCompile(`(?i)\s+(nude|pink|red|hitam|putih|beige|coral|brown|merah|biru|cream|matte|glossy|rose)$`)

var trailingShadeCodeRe = regexp.MustCompile(`(?i)\s+\d{1,3}$`)

func productLineCore(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = catalogQtyPrefixRe.ReplaceAllString(n, "")
	n = catalogLeadingPackRe.ReplaceAllString(n, "")
	n = bracketPackPrefixRe.ReplaceAllString(n, "")
	n = productSizeSuffixRe.ReplaceAllString(n, "")
	n = weightSuffixRe.ReplaceAllString(n, "")
	n = capacityOrVolumeRe.ReplaceAllString(n, " ")
	n = strings.TrimSpace(n)
	if i := strings.LastIndex(n, " - "); i >= 8 {
		left := strings.TrimSpace(n[:i])
		if len(strings.Fields(left)) >= 3 {
			n = left
		}
	}
	n = trailingShadeColorRe.ReplaceAllString(n, "")
	n = trailingShadeCodeRe.ReplaceAllString(n, "")
	return strings.Join(strings.Fields(n), " ")
}

func sameProductLine(a, b CatalogItem) bool {
	ca := productLineCore(a.Name)
	cb := productLineCore(b.Name)
	if ca == "" || cb == "" || len(ca) < 6 {
		return false
	}
	return ca == cb
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
