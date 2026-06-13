package ai

import (
	"regexp"
	"strings"
)

var promptInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+instructions?`),
	regexp.MustCompile(`(?i)system\s+prompt`),
	regexp.MustCompile(`(?i)developer\s+message`),
	regexp.MustCompile(`(?i)act\s+as\s+(an?\s+)?(admin|developer|root)`),
	regexp.MustCompile(`(?i)(show|reveal|leak).*(secret|token|password|key)`),
	regexp.MustCompile(`(?i)(drop|truncate|delete|alter)\s+(table|database|schema)`),
}

var idStopwords = map[string]struct{}{
	"yang": {}, "dan": {}, "atau": {}, "untuk": {}, "dengan": {},
	"dari": {}, "kami": {}, "kamu": {}, "saya": {}, "anda": {},
	"ini": {}, "itu": {}, "ada": {}, "mau": {}, "juga": {},
	"agar": {}, "supaya": {}, "bisa": {}, "lebih": {}, "sudah": {},
	"belum": {}, "dalam": {}, "pada": {}, "di": {}, "ke": {},
	"the": {}, "and": {}, "for": {}, "with": {},
}

var questionKeywords = []string{
	"apa", "apakah", "berapa", "gimana", "bagaimana",
	"kapan", "bisa", "stok", "size", "ukuran",
	"harga", "ongkir", "dimana", "lokasi", "mana",
}

var greetingPrefixes = []string{
	"selamat pagi", "selamat siang", "selamat sore", "selamat malam",
	"halo", "hai", "assalamualaikum", "salam", "permisi",
}

// Short Indonesian time-of-day / chat openers (not only "selamat malam").
var standaloneGreetingWords = []string{
	"pagi", "siang", "sore", "malam", "halo", "hai", "salam", "permisi",
}

// Shopping intent — in-scope for apparel/retail even if profile keywords (jeans, etc.) are absent from the message.
var retailIntentKeywords = []string{
	"mau", "tanya", "nanya", "tny", "beli", "pesan", "order", "checkout",
	"pcs", "pc", "biji", "buah", "qty", "jumlah", "unit", "saja",
	"harga", "stok", "stock", "ready", "tersedia", "ada", "jual", "jualan",
	"catalog", "menu", "rekomend", "minimum", "minimal", "min order",
	"ukuran", "size", "warna", "model", "varian", "katalog", "produk", "barang",
	"kirim", "ongkir", "pengiriman", "berapa", "apakah", "bisa", "cari", "butuh", "minat",
	"toko", "tokonya", "dimana", "lokasi", "alamat", "terima kasih", "makasih",
	"bayar", "pembayaran", "transfer", "trf", "tf", "cod", "qris", "rekening", "total",
	"invoice", "nota", "bukti", "lunasi",
}

// Customer engagement — replies about the bot/conversation stay in scope (not out_of_scope).
var customerEngagementKeywords = []string{
	"balas", "balasan", "kok", "salah", "keliru", "bilang", "kata", "maksud", "tadi",
	"pinter", "pandai", "mantap", "keren", "bagus", "jos", "joss",
}

// Apparel / fashion product terms (Omah-style tenants).
var apparelProductKeywords = []string{
	"celana", "jeans", "baju", "kaos", "pakaian", "fashion", "apparel", "busana",
	"hotpants", "skinny", "highwaist", "high waist", "dalam", "boxer", "kemeja",
	"dress", "rok", "hoodie", "jaket", "cardigan", "legging", "short", "pants",
	"denim", "warna", "size", "ukuran",
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9\s]`)
var digitOnly = regexp.MustCompile(`^\d+$`)

func SanitizeForPrompt(raw string) string {
	if raw == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(raw, "\x00", "")
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > 2000 {
		cleaned = cleaned[:2000]
	}
	return cleaned
}

func IsPromptInjectionLikely(raw string) bool {
	if raw == "" {
		return false
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return false
	}
	for _, p := range promptInjectionPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

func IsQuestionLike(raw string) bool {
	if raw == "" {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false
	}
	if strings.Contains(text, "?") {
		return true
	}
	for _, kw := range questionKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func IsGreetingLike(raw string) bool {
	if raw == "" {
		return false
	}
	if IsDraftOrderCancelRequest(raw) || IsSoftCancelRegret(raw) {
		return false
	}
	if IsMinimumOrderQuestion(raw) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false
	}
	if idx := strings.Index(text, ","); idx >= 0 {
		tail := strings.TrimSpace(text[idx+1:])
		if tail != "" && (isCommerceDominant(tail) || !isPureGreetingCore(tail)) {
			return false
		}
	}
	if isCommerceDominant(text) {
		return false
	}
	core := stripWaPoliteLeadIn(text)
	if isPureGreetingCore(text) || isPureGreetingCore(core) {
		return true
	}
	return false
}

func ExtractScopeKeywords(scopeText string) []string {
	lower := strings.ToLower(scopeText)
	cleaned := nonAlphaNum.ReplaceAllString(lower, " ")
	words := strings.Fields(cleaned)

	seen := make(map[string]struct{})
	var result []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len(w) < 4 {
			continue
		}
		if _, stop := idStopwords[w]; stop {
			continue
		}
		if digitOnly.MatchString(w) {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		result = append(result, w)
	}
	return result
}

func IsWithinBusinessScope(userText string, scopeKW, fallbackKW []string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}

	// e.g. "pesan nasi goreng" at Omah Apparel — commerce words alone must not count as in-scope.
	if IsOffBusinessProductRequest(userText, scopeKW) {
		return false
	}

	// Product / fashion vocabulary (e.g. "celana dalam" even when profile only mentions jeans).
	for _, kw := range apparelProductKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}

	// General commerce intent (price, stock, order, "mau tanya", etc.).
	for _, kw := range retailIntentKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}

	for _, kw := range customerEngagementKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}

	// Keyword from business profile / KB scope appears in the customer message.
	lookup := scopeKW
	if len(lookup) == 0 {
		lookup = fallbackKW
	}
	if len(lookup) == 0 {
		return true
	}
	for _, kw := range lookup {
		if len(kw) >= 3 && strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
