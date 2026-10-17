package buyerflow

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"encore.app/wabantu/shared/retrieval"
)

func warehouseBuyerLabel(customerLabel, warehouseName string) string {
	if s := strings.TrimSpace(customerLabel); s != "" {
		return s
	}
	return strings.TrimSpace(warehouseName)
}

var (
	postalCodeIDRe = regexp.MustCompile(`\b(\d{5})\b`)
	phoneIDRe      = regexp.MustCompile(`(?:\+62|62|0)8[0-9]{8,11}`)
	colorHintRe    = regexp.MustCompile(`(?i)(warna|color|colour)\s*[:\-]?\s*([a-z]+)`)
)

var genericCatalogMatchTokens = map[string]bool{
	"steak": true, "portion": true, "gram": true, "australian": true,
	"yellow": true, "tail": true, "beef": true, "pcs": true, "order": true,
	"please": true, "would": true, "like": true, "okay": true, "about": true,
}

// uniqueSingleProductTokenPick — e.g. "tuna" maps to one SKU; shared "steak" must not win.
func uniqueSingleProductTokenPick(text string, catalog []CatalogItem) *CatalogItem {
	tokens := tokenize(strings.ToLower(strings.TrimSpace(text)))
	if len(tokens) == 0 {
		return nil
	}
	exclude := append(catalogExcludeHints(text), catalogSpeciesExcludeHints(text)...)
	var picked *CatalogItem
	for _, tok := range tokens {
		if len(tok) < 3 || genericCatalogMatchTokens[tok] || isFuzzyStopToken(tok) {
			continue
		}
		var hits []*CatalogItem
		for i := range catalog {
			nameLower := strings.ToLower(catalog[i].Name)
			if catalogItemExcluded(nameLower, exclude) {
				continue
			}
			if strings.Contains(nameLower, tok) {
				hits = append(hits, &catalog[i])
			}
		}
		if len(hits) != 1 {
			continue
		}
		if picked == nil {
			picked = hits[0]
		} else if picked.ID != hits[0].ID {
			return nil
		}
	}
	return picked
}

func catalogSpeciesExcludeHints(text string) []string {
	lower := strings.ToLower(strings.TrimSpace(text))
	var out []string
	if strings.Contains(lower, "tuna") {
		out = append(out, "salmon")
	}
	if strings.Contains(lower, "salmon") {
		out = append(out, "tuna")
	}
	return out
}

func matchCatalogItem(userText string, catalog []CatalogItem) *CatalogItem {
	if len(catalog) == 0 {
		return nil
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	if pick := uniqueSingleProductTokenPick(text, catalog); pick != nil {
		return pick
	}

	exclude := append(catalogExcludeHints(text), catalogSpeciesExcludeHints(text)...)
	preferPria := strings.Contains(text, "pria") || strings.Contains(text, "cowok")
	preferAnak := strings.Contains(text, "anak") || strings.Contains(text, "perempuan")

	var best *CatalogItem
	var bestScore float64
	for i := range catalog {
		it := &catalog[i]
		nameLower := strings.ToLower(it.Name)
		if catalogItemExcluded(nameLower, exclude) {
			continue
		}
		score := overlapScore(tokens, tokenize(nameLower))
		if nameLower != "" && strings.Contains(text, nameLower) {
			score += 0.35
		}
		for _, tok := range tokens {
			if len(tok) >= 4 && strings.Contains(nameLower, tok) {
				score += 0.08
			}
		}
		score += catalogPhraseBoost(text, nameLower)
		if preferPria {
			if strings.Contains(nameLower, "pria") || strings.Contains(nameLower, "cowok") {
				score += 0.25
			}
			if strings.Contains(nameLower, "perempuan") || strings.Contains(nameLower, "anak perempuan") {
				score -= 0.35
			}
		}
		if preferAnak && strings.Contains(nameLower, "anak") {
			score += 0.15
		}
		if score > bestScore {
			bestScore = score
			best = it
		}
	}
	if bestScore < 0.12 {
		return matchCatalogItemFuzzy(userText, catalog)
	}
	return best
}

// matchCatalogItemFuzzy — typo singkat (mis. "cadburi" → Cadbury) bila lexical miss.
func matchCatalogItemFuzzy(userText string, catalog []CatalogItem) *CatalogItem {
	text := strings.ToLower(strings.TrimSpace(userText))
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	exclude := catalogExcludeHints(text)
	var best *CatalogItem
	var bestScore float64
	for i := range catalog {
		it := &catalog[i]
		nameLower := strings.ToLower(it.Name)
		if catalogItemExcluded(nameLower, exclude) {
			continue
		}
		score := fuzzyNameTokenScore(tokens, tokenize(nameLower))
		if score > bestScore {
			bestScore = score
			best = it
		}
	}
	if bestScore < 0.18 {
		return nil
	}
	return best
}

func fuzzyNameTokenScore(userTokens, nameTokens []string) float64 {
	var score float64
	for _, ut := range userTokens {
		if len(ut) < 4 || isFuzzyStopToken(ut) {
			continue
		}
		for _, nt := range nameTokens {
			if len(nt) < 4 {
				continue
			}
			if fuzzyTokenPrefixMatch(ut, nt) {
				score += 0.22
			}
		}
	}
	return score
}

func isFuzzyStopToken(tok string) bool {
	switch tok {
	case "mau", "pesen", "pesan", "beli", "order", "pcs", "paket", "bukan", "woi", "dong", "kak":
		return true
	}
	return false
}

func fuzzyTokenPrefixMatch(a, b string) bool {
	if a == b {
		return true
	}
	n := 4
	if len(a) < n || len(b) < n {
		return false
	}
	return strings.HasPrefix(a, b[:n]) || strings.HasPrefix(b, a[:n])
}

func catalogNameAppearsInText(name, body string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	body = strings.ToLower(body)
	if name == "" || body == "" {
		return false
	}
	if strings.Contains(body, name) {
		return true
	}
	compact := strings.Join(strings.Fields(strings.NewReplacer("(", " ", ")", " ", ",", " ").Replace(body)), " ")
	fields := strings.Fields(name)
	if len(fields) >= 3 {
		prefix := strings.Join(fields[:3], " ")
		return strings.Contains(compact, prefix)
	}
	return strings.Contains(compact, name)
}

// matchSingleProductFromRecentOutbound — pronoun buy / history-backed order; ignore token noise like "gram".
func matchSingleProductFromRecentOutbound(history []Message, catalog []CatalogItem) *CatalogItem {
	if len(history) == 0 || len(catalog) == 0 {
		return nil
	}
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < 4; i-- {
		if history[i].Direction != "out" {
			continue
		}
		seen++
		body := history[i].Body
		if isCatalogListOutbound(body) {
			continue
		}
		var matched []*CatalogItem
		for j := range catalog {
			it := &catalog[j]
			if catalogNameAppearsInText(it.Name, body) {
				matched = append(matched, it)
			}
		}
		switch len(matched) {
		case 1:
			return matched[0]
		case 0:
			if m := uniqueSingleProductTokenPick(body, catalog); m != nil {
				return m
			}
		}
	}
	return nil
}

func resolveOrderProductMatch(userText string, history []Message, catalog []CatalogItem, vctx *CatalogVectorContext) *CatalogItem {
	if isPronounPurchaseIntent(userText) {
		if m := matchSingleProductFromRecentOutbound(history, catalog); m != nil {
			return m
		}
	}
	if unique := uniqueBrandSKUFromText(userText, catalog); unique != nil {
		return unique
	}
	if unique := uniqueSizedSKUFromText(userText, catalog); unique != nil {
		return unique
	}
	if lexicalBrandAmbiguous(userText, catalog) {
		return nil
	}
	if m := matchCatalogItem(userText, catalog); m != nil {
		return m
	}
	if vctx != nil && len(vctx.Hits) > 0 {
		if m := MatchCatalogItemSemantic(userText, catalog, vctx.Hits); m != nil {
			return m
		}
		if orderSemanticAmbiguous(vctx) {
			return nil
		}
	}
	return matchCatalogFromRecentOutbound(history, catalog)
}

func orderSemanticAmbiguous(vctx *CatalogVectorContext) bool {
	return vctx != nil && len(vctx.Hits) >= 2 && CatalogSemanticAmbiguous(vctx.Hits)
}

func matchCatalogLine(raw string, catalog []CatalogItem, vctx *CatalogVectorContext) *CatalogItem {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	if m := resolveOrderProductMatch(text, nil, catalog, vctx); m != nil {
		return m
	}
	cleaned := StripOrderSizeTokens(text)
	cleaned = strings.TrimSpace(strings.TrimRight(cleaned, "ya"))
	return resolveOrderProductMatch(cleaned, nil, catalog, vctx)
}

// orderVectorVariantPickerReply — klarifikasi varian saat vector hits ambiguous di order flow.
func orderVectorVariantPickerReply(formal bool, userText string, catalog []CatalogItem, vctx *CatalogVectorContext) (string, bool) {
	if vctx == nil || len(vctx.Hits) == 0 {
		return "", false
	}
	brand := brandTokenFromText(userText, catalog)
	if brand != "" {
		ids := catalogVectorHitsEntryIDs(vctx.Hits)
		if reply := buildBrandVariantListFromVectorHits(formal, brand, catalog, ids, 10, userText); reply != "" {
			return reply, true
		}
	}
	byID := map[string]CatalogItem{}
	for _, it := range catalog {
		byID[it.ID] = it
	}
	var items []CatalogItem
	seen := map[string]struct{}{}
	for _, h := range sortedHitsByScore(vctx.Hits) {
		id := retrieval.EntryIDFromHit(h)
		it, ok := byID[id]
		if !ok {
			continue
		}
		if _, dup := seen[it.ID]; dup {
			continue
		}
		seen[it.ID] = struct{}{}
		items = append(items, it)
	}
	if len(items) < 2 {
		return "", false
	}
	token := brand
	if token == "" {
		token = "produk"
	}
	return formatBrandVariantListBody(formal, token, items, 6, userText), true
}

func catalogExcludeHints(text string) []string {
	lower := strings.ToLower(strings.TrimSpace(text))
	var out []string
	if strings.Contains(lower, "bukan") {
		for _, hint := range []string{"hello kitty", "mono spot", "de wasa", "abon", "boxer"} {
			if strings.Contains(lower, "bukan "+hint) || strings.Contains(lower, "bukan "+strings.ReplaceAll(hint, " ", "")) {
				out = append(out, hint)
			}
			if hint == "hello kitty" && strings.Contains(lower, "bukan") && strings.Contains(lower, "hello") {
				out = append(out, hint)
			}
		}
		if strings.Contains(lower, "bukan hello") || strings.Contains(lower, "bukan hellokitty") {
			out = append(out, "hello kitty")
		}
		for _, prefix := range []string{"bukan "} {
			idx := strings.Index(lower, prefix)
			if idx < 0 {
				continue
			}
			rest := strings.TrimSpace(lower[idx+len(prefix):])
			for _, stop := range []string{" wo", " ya", " kak", " dong", " woi", ",", "?", "!", " tapi", " yang"} {
				if j := strings.Index(rest, stop); j > 0 {
					rest = strings.TrimSpace(rest[:j])
				}
			}
			if rest != "" && len(rest) >= 3 {
				out = append(out, rest)
			}
		}
	}
	for _, prefix := range []string{"selain ", "kecuali "} {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(lower[idx+len(prefix):])
		for _, stop := range []string{" ada ", " list", " produk", " barang", "?", ",", " yang"} {
			if j := strings.Index(rest, stop); j > 0 {
				rest = strings.TrimSpace(rest[:j])
			}
		}
		if rest != "" {
			out = append(out, rest)
		}
	}
	return out
}

func catalogItemExcluded(nameLower string, exclude []string) bool {
	for _, ex := range exclude {
		if strings.Contains(nameLower, ex) {
			return true
		}
	}
	return false
}

func catalogPhraseBoost(text, nameLower string) float64 {
	phrases := []string{"mono spot", "hello kitty", "de wasa", "abon sapi"}
	var boost float64
	for _, p := range phrases {
		if strings.Contains(text, p) && strings.Contains(nameLower, p) {
			boost += 0.4
		}
	}
	return boost
}

// tryApplyProductRevision — ganti produk saat checkout ("bukan hello kitty", "mono spot bukan ...").
func tryApplyProductRevision(st *OrderState, userText string, catalog []CatalogItem, vctx *CatalogVectorContext) bool {
	if st == nil || len(catalog) == 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	hasSignal := strings.Contains(text, "bukan") || strings.Contains(text, "not the ") ||
		strings.Contains(text, " instead of ") ||
		strings.Contains(text, "ubah produk") ||
		strings.Contains(text, "ganti jadi") || strings.Contains(text, "ganti produk") ||
		strings.Contains(text, "mau boxer") || strings.Contains(text, "mau mono") ||
		(strings.Contains(text, "mono spot") && strings.Contains(text, "bukan")) ||
		(strings.Contains(text, "hello kitty") && strings.Contains(text, "bukan")) ||
		(strings.Contains(text, "de wasa") && !strings.Contains(text, "ganti jadi"))
	if !hasSignal {
		return false
	}
	match := resolveOrderProductMatch(userText, nil, catalog, vctx)
	if match == nil {
		return false
	}
	prevID := st.CatalogItemID
	applyCatalogMatch(st, match)
	inferVariantFromProductName(st)
	sz, color := parseSizeAndColor(userText)
	if sz != "" {
		st.Size = sz
	}
	if color != "" {
		st.Color = color
	}
	return st.CatalogItemID != prevID || st.ProductName != match.Name
}

func formatCatalogPicker(catalog []CatalogItem, max int) string {
	if len(catalog) == 0 {
		return ""
	}
	if max < 1 || max > 8 {
		max = 6
	}
	var lines []string
	for i := 0; i < len(catalog) && i < max; i++ {
		it := catalog[i]
		price := ""
		if it.SellPrice > 0 {
			price = fmt.Sprintf(" — Rp%.0f", it.SellPrice)
		}
		lines = append(lines, fmt.Sprintf("• %s%s", it.Name, price))
	}
	return strings.Join(lines, "\n")
}

func parseSizeAndColor(userText string) (size, color string) {
	text := strings.TrimSpace(userText)
	if m := orderSizeLineRe.FindString(text); m != "" {
		size = strings.ToUpper(m)
	}
	lower := strings.ToLower(text)
	if m := colorHintRe.FindStringSubmatch(lower); len(m) > 2 {
		color = strings.TrimSpace(m[2])
	}
	for _, c := range []string{"hitam", "putih", "biru", "merah", "pink", "cream", "navy", "abu", "coklat", "hijau", "kuning"} {
		if strings.Contains(lower, c) {
			if color == "" {
				color = c
			}
		}
	}
	return size, color
}

func buildVariantLabel(size, color string) string {
	var parts []string
	if size != "" {
		parts = append(parts, "Ukuran: "+size)
	}
	if color != "" {
		parts = append(parts, "Warna: "+color)
	}
	return strings.Join(parts, " | ")
}

var buyerQuestionStarts = map[string]bool{
	"what": true, "when": true, "where": true, "why": true, "how": true, "is": true, "are": true,
	"can": true, "could": true, "do": true, "does": true, "will": true, "would": true, "which": true,
	"apa": true, "apakah": true, "kapan": true, "dimana": true, "kenapa": true, "mengapa": true,
	"gimana": true, "bagaimana": true, "berapa": true, "bisakah": true, "bolehkah": true,
}

var nonNameWords = map[string]bool{
	"today": true, "tomorrow": true, "delivered": true, "deliver": true, "delivery": true,
	"ship": true, "shipping": true, "order": true, "cancel": true, "price": true, "frozen": true,
	"fresh": true, "ask": true, "want": true, "product": true, "kirim": true, "dikirim": true,
	"besok": true, "hari": true, "pesanan": true, "batal": true, "harga": true, "ongkir": true,
	"tanya": true, "mau": true, "woi": true, "pay": true, "bayar": true, "transfer": true,
}

// buyerAskStatementRe — an inquiry worded without '?' or a leading question word
// ("I want to ask about…", "mau tanya soal…"). Word-bounded so a name like "Tanya" is not matched.
var buyerAskStatementRe = regexp.MustCompile(`(?i)\b(?:(?:want|wanna|like|need)\s+to\s+ask|ask\s+(?:about|you)|have\s+a\s+question|(?:mau|ingin|pengen|boleh)\s+(?:tanya|nanya|bertanya)|nanya)\b`)

// looksLikeBuyerQuestion — '?' anywhere, a question word first, an "X or Y" day choice,
// or an ask-statement ("I want to ask about…").
func looksLikeBuyerQuestion(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(t, "?") {
		return true
	}
	f := strings.Fields(t)
	if len(f) > 0 && buyerQuestionStarts[f[0]] {
		return true
	}
	return strings.Contains(t, "today or tomorrow") || strings.Contains(t, "hari ini atau besok") ||
		buyerAskStatementRe.MatchString(t)
}

// plausibleNameLine — 1–4 words, letters and . ' - only, no common non-name word.
func plausibleNameLine(line string) bool {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 0 || len(f) > 4 {
		return false
	}
	for _, w := range f {
		lw := strings.ToLower(strings.Trim(w, ".,'-"))
		if nonNameWords[lw] {
			return false
		}
		for _, r := range w {
			if !(unicode.IsLetter(r) || r == '.' || r == '\'' || r == '-') {
				return false
			}
		}
	}
	return true
}

// hasRecipientNameLabel — a `Name:` / `Nama:` style labelled line is present.
func hasRecipientNameLabel(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if kind, _ := englishRecipientLabel(line); kind == recipientLabelName {
			return true
		}
		if strings.HasPrefix(strings.ToLower(line), "nama") && strings.Contains(line, ":") {
			return true
		}
	}
	return false
}

func parseRecipientLine(userText string) (name, phone string) {
	text := strings.TrimSpace(userText)
	// A question mid-checkout is never a recipient name (phone, if any, is still kept).
	if looksLikeBuyerQuestion(text) && !hasRecipientNameLabel(text) {
		if m := phoneIDRe.FindString(text); m != "" {
			phone = normalizePhoneID(m)
		}
		return "", phone
	}
	if m := phoneIDRe.FindString(text); m != "" {
		phone = normalizePhoneID(m)
		name = strings.TrimSpace(strings.ReplaceAll(text, m, ""))
		name = strings.TrimSpace(strings.Trim(name, ",;-"))
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if p := phoneIDRe.FindString(line); p != "" && phone == "" {
			phone = normalizePhoneID(p)
			continue
		}
		lower := strings.ToLower(line)
		if kind, val := englishRecipientLabel(line); kind != "" {
			switch kind {
			case recipientLabelName:
				if val != "" {
					name = val
				}
			case recipientLabelPhone:
				if p := extractRecipientPhone(val); p != "" {
					phone = p
				}
			}
			continue
		}
		if strings.HasPrefix(lower, "nama") {
			name = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		} else if strings.HasPrefix(lower, "hp") || strings.HasPrefix(lower, "no") || strings.HasPrefix(lower, "telp") {
			if p := phoneIDRe.FindString(line); p != "" {
				phone = normalizePhoneID(p)
			}
		} else if phone == "" && phoneIDRe.MatchString(line) {
			phone = normalizePhoneID(phoneIDRe.FindString(line))
		} else if name == "" && len(line) > 2 && plausibleNameLine(line) {
			name = line
		}
	}
	return name, phone
}

const (
	recipientLabelName  = "name"
	recipientLabelPhone = "phone"

	// maxRecipientFieldLen caps label values parsed from buyer text (defensive).
	maxRecipientFieldLen = 120
	// maxRecipientPhoneDigits caps digits kept when normalising a bare phone value.
	maxRecipientPhoneDigits = 15
)

// englishRecipientLabel detects `Name:` / `Phone:` style labels (EN) on a single line.
// Returns ("", "") for any other line so ID labels keep their existing handling.
func englishRecipientLabel(line string) (kind, val string) {
	i := strings.Index(line, ":")
	if i <= 0 || i > 32 {
		return "", ""
	}
	key := strings.ToLower(strings.TrimSpace(line[:i]))
	switch key {
	case "name", "full name", "recipient", "recipient name", "receiver", "receiver name":
		kind = recipientLabelName
	case "phone", "phone number", "phone no", "phone no.", "mobile", "mobile number", "mobile phone", "tel", "contact", "contact number", "whatsapp":
		kind = recipientLabelPhone
	default:
		return "", ""
	}
	val = strings.TrimSpace(line[i+1:])
	if len(val) > maxRecipientFieldLen {
		val = ""
	}
	return kind, val
}

// extractRecipientPhone pulls a phone from a labelled value. phoneIDRe wins; otherwise a
// bare 8–12 digit mobile starting with 8 or 9 (leading 0 / +62 omitted) is normalised to +62.
func extractRecipientPhone(val string) string {
	val = strings.TrimSpace(val)
	if val == "" || len(val) > maxRecipientFieldLen {
		return ""
	}
	total := 0
	for i := 0; i < len(val); i++ {
		if val[i] >= '0' && val[i] <= '9' {
			total++
		}
	}
	if total > maxRecipientPhoneDigits {
		return ""
	}
	if m := phoneIDRe.FindString(val); m != "" {
		return normalizePhoneID(m)
	}
	var digits strings.Builder
	for _, r := range val {
		switch {
		case r >= '0' && r <= '9':
			if digits.Len() >= maxRecipientPhoneDigits {
				return ""
			}
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '.' || r == '(' || r == ')' || r == '+':
		default:
			return ""
		}
	}
	d := digits.String()
	switch {
	case strings.HasPrefix(d, "628") && len(d) >= 11 && len(d) <= 14:
		return "+" + d
	case strings.HasPrefix(d, "08") && len(d) >= 10 && len(d) <= 13:
		return "+62" + d[1:]
	}
	if len(d) < 8 || len(d) > 12 || (d[0] != '8' && d[0] != '9') {
		return ""
	}
	return "+62" + d
}

func normalizePhoneID(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	if strings.HasPrefix(p, "62") {
		return "+" + p
	}
	if strings.HasPrefix(p, "0") {
		return "+62" + strings.TrimPrefix(p, "0")
	}
	return p
}

// clearShippingAddress wipes street/city/postal fields so a buyer amend does not merge into stale data.
func clearShippingAddress(st *OrderState) {
	if st == nil {
		return
	}
	st.Street, st.RT, st.RW = "", "", ""
	st.Kelurahan, st.Kecamatan = "", ""
	st.City, st.Province, st.PostalCode = "", "", ""
}

func mergeShippingText(st *OrderState, userText string) {
	if st == nil {
		return
	}
	text := strings.TrimSpace(userText)
	if text == "" {
		return
	}
	lower := strings.ToLower(text)

	if st.PostalCode == "" {
		if m := postalCodeIDRe.FindStringSubmatch(text); len(m) > 1 {
			st.PostalCode = m[1]
		}
	}

	name, phone := parseRecipientLine(text)
	if name != "" && st.RecipientName == "" {
		st.RecipientName = name
	}
	if phone != "" && st.RecipientPhone == "" {
		st.RecipientPhone = phone
	}

	// Labelled lines (format resmi Indonesia).
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		val := labelValue(line)
		if kind, ev := englishRecipientLabel(line); kind != "" {
			switch kind {
			case recipientLabelName:
				if ev != "" {
					st.RecipientName = ev
				}
			case recipientLabelPhone:
				if p := extractRecipientPhone(ev); p != "" {
					st.RecipientPhone = p
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(low, "nama penerima"), strings.HasPrefix(low, "nama"):
			if val != "" {
				st.RecipientName = val
			}
		case strings.HasPrefix(low, "no hp"), strings.HasPrefix(low, "hp"), strings.HasPrefix(low, "telepon"), strings.HasPrefix(low, "wa"):
			if p := extractRecipientPhone(val); p != "" {
				st.RecipientPhone = p
			}
		case val != "" && strings.HasPrefix(low, "street"):
			st.Street = val
		case val != "" && strings.HasPrefix(low, "city"):
			st.City = val
		case val != "" && (strings.HasPrefix(low, "province") || strings.HasPrefix(low, "state")):
			st.Province = val
		case val != "" && (strings.HasPrefix(low, "postal") || strings.HasPrefix(low, "postcode") || strings.HasPrefix(low, "zip")):
			if m := postalCodeIDRe.FindString(val); m != "" {
				st.PostalCode = m
			}
		case val != "" && (strings.HasPrefix(low, "district") || strings.HasPrefix(low, "subdistrict")):
			st.Kecamatan = val
		case strings.HasPrefix(low, "jalan"), strings.HasPrefix(low, "alamat"), strings.HasPrefix(low, "address"), strings.HasPrefix(low, "jl"):
			if val != "" {
				st.Street = val
			}
		case strings.HasPrefix(low, "rt/rw"), strings.Contains(low, "rt/rw"):
			parts := strings.SplitN(val, "/", 2)
			if len(parts) == 2 {
				st.RT = strings.TrimSpace(parts[0])
				st.RW = strings.TrimSpace(parts[1])
			} else {
				st.RT = val
			}
		case strings.HasPrefix(low, "rt"):
			st.RT = val
		case strings.HasPrefix(low, "rw"):
			st.RW = val
		case strings.HasPrefix(low, "kel"), strings.HasPrefix(low, "kelurahan"):
			st.Kelurahan = val
		case strings.HasPrefix(low, "kec"), strings.HasPrefix(low, "kecamatan"):
			st.Kecamatan = val
		case strings.HasPrefix(low, "kota"), strings.HasPrefix(low, "kab"):
			st.City = val
		case strings.HasPrefix(low, "prov"), strings.HasPrefix(low, "provinsi"):
			st.Province = val
		case strings.HasPrefix(low, "kode pos"), strings.HasPrefix(low, "pos"):
			if m := postalCodeIDRe.FindString(val); m != "" {
				st.PostalCode = m
			}
		case strings.HasPrefix(low, "negara"):
			st.Country = val
		}
	}

	if st.Street == "" && !looksLikeBuyerQuestion(text) && orderAddrHintRe.MatchString(text) {
		st.Street = strings.TrimSpace(text)
	}
	parseUnstructuredAddress(st, lower, text)
	parseCommaAddress(st, text)
	if st.Country == "" {
		st.Country = "Indonesia"
	}
}

var idCityHints = []struct {
	needle   string
	city     string
	province string
}{
	{"jakarta selatan", "Jakarta Selatan", "DKI Jakarta"},
	{"south jakarta", "Jakarta Selatan", "DKI Jakarta"},
	{"central jakarta", "Jakarta Pusat", "DKI Jakarta"},
	{"east jakarta", "Jakarta Timur", "DKI Jakarta"},
	{"west jakarta", "Jakarta Barat", "DKI Jakarta"},
	{"north jakarta", "Jakarta Utara", "DKI Jakarta"},
	{"jaksel", "Jakarta Selatan", "DKI Jakarta"},
	{"jakarta pusat", "Jakarta Pusat", "DKI Jakarta"},
	{"jakarta timur", "Jakarta Timur", "DKI Jakarta"},
	{"jakarta barat", "Jakarta Barat", "DKI Jakarta"},
	{"jakarta utara", "Jakarta Utara", "DKI Jakarta"},
	{"surabaya", "Surabaya", "Jawa Timur"},
	{"malang", "Malang", "Jawa Timur"},
	{"bandung", "Bandung", "Jawa Barat"},
	{"yogyakarta", "Yogyakarta", "DI Yogyakarta"},
	{"semarang", "Semarang", "Jawa Tengah"},
	{"medan", "Medan", "Sumatera Utara"},
	{"bekasi", "Bekasi", "Jawa Barat"},
	{"tangerang selatan", "Tangerang Selatan", "Banten"},
	{"tangerang", "Tangerang", "Banten"},
	{"depok", "Depok", "Jawa Barat"},
}

func parseUnstructuredAddress(st *OrderState, lower, raw string) {
	if st == nil {
		return
	}
	for _, h := range idCityHints {
		if strings.Contains(lower, h.needle) {
			if st.City == "" {
				st.City = h.city
			}
			if st.Province == "" {
				st.Province = h.province
			}
			break
		}
	}
	// "Jl X, Jakarta Selatan" — keep street before comma when whole text was dumped to Street.
	if st.Street != "" && strings.Contains(st.Street, ",") {
		parts := strings.SplitN(st.Street, ",", 2)
		if len(parts) == 2 && orderAddrHintRe.MatchString(parts[0]) {
			st.Street = strings.TrimSpace(parts[0])
			tail := strings.TrimSpace(strings.ToLower(parts[1]))
			for _, h := range idCityHints {
				if strings.Contains(tail, h.needle) {
					if st.City == "" {
						st.City = h.city
					}
					if st.Province == "" {
						st.Province = h.province
					}
					break
				}
			}
		}
	}
	if st.Street == "" && st.City != "" {
		idx := strings.Index(lower, strings.ToLower(st.City))
		if idx > 3 {
			street := strings.Trim(raw[:idx], " ,\n")
			lowStreet := strings.ToLower(street)
			for _, label := range []string{"address:", "alamat:"} {
				if i := strings.LastIndex(lowStreet, label); i >= 0 {
					street = strings.TrimSpace(street[i+len(label):])
				}
			}
			street = strings.Trim(street, " ,")
			if len(street) > 3 {
				st.Street = street
			}
		}
	}
}

func labelValue(line string) string {
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return strings.TrimSpace(line)
}

func (st OrderState) ShippingComplete() bool {
	if st.RecipientName == "" || st.RecipientPhone == "" {
		return false
	}
	if st.Street == "" || st.City == "" || st.Province == "" {
		return false
	}
	if len(st.PostalCode) != 5 {
		return false
	}
	return postalCodeIDRe.MatchString(st.PostalCode)
}

func (st OrderState) ProductComplete() bool {
	if st.HasMultiItems() {
		return st.StructuredLinesReady()
	}
	return strings.TrimSpace(st.ProductName) != "" || strings.TrimSpace(st.CatalogItemID) != ""
}

// catalogItemNeedsVariant — apparel/ukuran S-M-L. Makanan, gadget, kosmetik, dan
// SKU tanpa suffix ukuran tidak masuk ask_variant.
func catalogItemNeedsVariant(it *CatalogItem) bool {
	if it == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(it.Name))
	if name == "" {
		return false
	}
	if extractSizeFromProductName(it.Name) != "" {
		return true
	}
	for _, kw := range []string{
		"celana", "boxer", "jeans", "baju", "kaos", "dress", "rok", "kemeja",
		"jaket", "hoodie", "highwaist", "hotpants", "skinny",
	} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	if strings.Contains(name, "celana dalam") || strings.Contains(name, "boxer anak") {
		return true
	}
	return false
}

func (st OrderState) VariantComplete() bool {
	it := &CatalogItem{Name: st.ProductName, ExternalCode: st.ExternalCode}
	if !catalogItemNeedsVariant(it) {
		return true
	}
	return st.Size != "" || st.Color != ""
}

func applyCatalogMatch(st *OrderState, it *CatalogItem) {
	if st == nil || it == nil {
		return
	}
	st.CatalogItemID = it.ID
	st.ExternalCode = it.ExternalCode
	st.ProductName = it.Name
	st.UnitPrice = it.SellPrice
	st.SellUnit = it.SellUnit
	if st.ProductName == "" {
		st.ProductName = it.Name
	}
	inferVariantFromProductName(st)
}

func catalogConfirmLine(st OrderState) string {
	summary := formatOrderSummary(st)
	if summary != "" {
		return summary
	}
	if st.ProductName == "" {
		return ""
	}
	it := &CatalogItem{
		Name:         st.ProductName,
		SellPrice:    st.UnitPrice,
		SellUnit:     st.SellUnit,
		ExternalCode: st.ExternalCode,
	}
	return strings.TrimSpace("Produk:\n" + st.ProductName + "\n\nHarga:\n" + formatCatalogPrice(it))
}

func missingOrderDataPrompt(st OrderState, tmpl orderFlowTemplates) string {
	st = normalizeOrderState(st)
	if st.HasMultiItems() {
		if !st.StructuredLinesReady() {
			return tmpl.AskVariant
		}
	} else {
		if !st.ProductComplete() {
			return tmpl.AskProduct
		}
		if !st.VariantComplete() {
			return tmpl.AskVariant
		}
		if st.Qty < 1 {
			return tmpl.AskQty
		}
	}
	if strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "" {
		return tmpl.AskRecipient
	}
	if !st.ShippingComplete() {
		return addressMissingReply(st, tmpl)
	}
	return ""
}

// provinceAliases maps English and short province names to the Indonesian form.
var provinceAliases = map[string]string{
	"dki jakarta": "DKI Jakarta", "jakarta": "DKI Jakarta",
	"east java": "Jawa Timur", "jawa timur": "Jawa Timur", "jatim": "Jawa Timur",
	"west java": "Jawa Barat", "jawa barat": "Jawa Barat", "jabar": "Jawa Barat",
	"central java": "Jawa Tengah", "jawa tengah": "Jawa Tengah", "jateng": "Jawa Tengah",
	"banten": "Banten", "bali": "Bali", "yogyakarta": "DI Yogyakarta", "di yogyakarta": "DI Yogyakarta",
	"north sumatra": "Sumatera Utara", "sumatera utara": "Sumatera Utara",
	"west sumatra": "Sumatera Barat", "sumatera barat": "Sumatera Barat",
	"south sumatra": "Sumatera Selatan", "sumatera selatan": "Sumatera Selatan",
	"riau": "Riau", "lampung": "Lampung", "aceh": "Aceh",
	"east kalimantan": "Kalimantan Timur", "kalimantan timur": "Kalimantan Timur",
	"west kalimantan": "Kalimantan Barat", "kalimantan barat": "Kalimantan Barat",
	"south kalimantan": "Kalimantan Selatan", "kalimantan selatan": "Kalimantan Selatan",
	"south sulawesi": "Sulawesi Selatan", "sulawesi selatan": "Sulawesi Selatan",
	"north sulawesi": "Sulawesi Utara", "sulawesi utara": "Sulawesi Utara",
	"west nusa tenggara": "Nusa Tenggara Barat", "ntb": "Nusa Tenggara Barat",
	"east nusa tenggara": "Nusa Tenggara Timur", "ntt": "Nusa Tenggara Timur",
	"papua": "Papua", "maluku": "Maluku",
}

var postalSuffixRe = regexp.MustCompile(`\s*\b\d{5}\b\s*$`)

// parseCommaAddress handles one-line addresses written as "street, city, province, postal"
// (English or Indonesian) for any city, not only the idCityHints table. Only empty fields
// are filled, except a Street that still holds the whole comma-separated line.
func parseCommaAddress(st *OrderState, raw string) {
	if st == nil {
		return
	}
	var line string
	for _, l := range strings.Split(raw, "\n") {
		if strings.Count(l, ",") >= 2 {
			line = l
			break
		}
	}
	if line == "" {
		return
	}
	var parts []string
	for _, p := range strings.Split(line, ",") {
		p = strings.TrimSpace(postalSuffixRe.ReplaceAllString(strings.TrimSpace(p), ""))
		lp := strings.ToLower(p)
		if p == "" || lp == "indonesia" {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) < 3 {
		return
	}
	street, city, prov := parts[0], parts[len(parts)-2], parts[len(parts)-1]
	if st.Street == "" || strings.Count(st.Street, ",") >= 2 {
		st.Street = street
	}
	if st.City == "" {
		st.City = city
	}
	if st.Province == "" {
		if canon, ok := provinceAliases[strings.ToLower(prov)]; ok {
			st.Province = canon
		} else {
			st.Province = prov
		}
	}
}

// addressMissingReply names only the address parts that are still missing, in the language of
// the active templates, instead of repeating the full "need street, city, province…" list.
func addressMissingReply(st OrderState, tmpl orderFlowTemplates) string {
	en := strings.HasPrefix(tmpl.ClarifyAddress, "Address") || strings.HasPrefix(tmpl.ClarifyAddress, "The address")
	var missing []string
	add := func(id, eng string) {
		if en {
			missing = append(missing, eng)
		} else {
			missing = append(missing, id)
		}
	}
	if strings.TrimSpace(st.Street) == "" {
		add("nama jalan dan nomor", "street name and number")
	}
	if strings.TrimSpace(st.City) == "" {
		add("kota", "city")
	}
	if strings.TrimSpace(st.Province) == "" {
		add("provinsi", "province")
	}
	if len(st.PostalCode) != 5 {
		add("kode pos 5 digit", "5-digit postal code")
	}
	if len(missing) == 0 {
		return tmpl.ClarifyAddress
	}
	list := strings.Join(missing, ", ")
	if en {
		return "Thanks! To complete the address I still need the " + list + "."
	}
	return "Makasih kak, alamatnya tinggal kurang " + list + " ya."
}
