package ai

import (
	"strings"
)

// Sales assistant states (BROWSING → CONSULTING → PRODUCT_SELECTED → CART_READY → CHECKOUT).
// Order FSM maps CHECKOUT steps; catalog paths cover BROWSING/CONSULTING/PRODUCT_SELECTED.

var explicitCartReadyPhrases = []string{
	"saya jadi beli", "saya pesan", "saya order", "saya ambil",
	"lanjut checkout", "checkout aja", "mau yang ini",
	"jadi pesan", "jadi order", "jadi beli", "jadi ambil",
	"siap pesan", "siap order", "fix order", "lanjut pesan",
}

var consultingPurchasePrefixes = []string{
	"boleh", "bisa", "bisa ga", "bisa gak", "bisa nggak", "boleh ga", "boleh gak", "boleh nggak",
	"kalau", "kalo", "misal", "misalnya", "andai",
	"gimana kalau", "gimana kalo", "apa bisa", "apakah bisa", "apakah boleh",
}

var retailPolicySignals = []string{
	"eceran", "ecer", "per biji", "bijian", "biji aja", "biji doang",
	"beli 1 pcs", "beli satu pcs", "1 biji", "satu biji", "satuan",
}

var salesCorrectionPhrases = []string{
	"masih tanya", "belum jadi beli", "belum mau beli", "belum order", "belum pesan",
	"jangan checkout", "jangan di checkout", "jangan checkoutkan", "ga mau checkout",
	"gak mau checkout", "nggak mau checkout", "tidak mau checkout",
	"cuma nanya", "hanya nanya", "cuma nanya-nanya",
	"bukan mau beli", "bukan mau order", "bukan itu", "salah paham",
	"loh saya masih", "belum siap", "nanti aja dulu", "jangan di checkoutkan",
}

func hasExplicitCartReadyPhrase(text string) bool {
	for _, p := range explicitCartReadyPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// isHistoryBackedPurchaseIntent — "mau beli 2 lusin" tanpa nama produk, produk jelas dari outbound terakhir.
func isHistoryBackedPurchaseIntent(userText string, history []dbMessage, catalog []dbCatalogItem) bool {
	if !mentionsOrderQty(userText) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if !(strings.Contains(text, "mau beli") || hasOrderIntentText(userText)) {
		return false
	}
	if matchCatalogItem(userText, catalog) != nil {
		return false
	}
	return matchCatalogFromFocusedHistory(history, catalog) != nil
}

// IsConsultingPurchaseQuestion — "boleh beli 1 pcs?", "kalau order satu bisa?" (CONSULTING, bukan CART_READY).
func IsConsultingPurchaseQuestion(userText string, catalog []dbCatalogItem) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsPaymentStatusInquiry(userText) {
		return false
	}
	if IsRecipientPolicyQuestion(userText) {
		return false
	}
	if isNamedProductPurchaseIntent(userText, catalog) {
		return false
	}
	for _, p := range orderStatusInquiryPhrases {
		if strings.Contains(text, p) {
			return false
		}
	}
	for _, s := range retailPolicySignals {
		if strings.Contains(text, s) {
			// "mau 1 biji abon" = pesanan eksplisit, bukan tanya kebijakan eceran.
			if !IsQuestionLike(userText) && mentionsOrderQty(text) &&
				(strings.Contains(text, "mau") || strings.Contains(text, "pengen") ||
					strings.Contains(text, "pengin") || hasOrderIntentText(userText)) {
				return false
			}
			return true
		}
	}
	hasBuy := hasOrderIntentText(userText) || strings.Contains(text, "mau")
	if hasConsultingPurchasePrefix(text) && (hasBuy || mentionsOrderQty(text)) {
		return true
	}
	if (strings.Contains(text, "?") || hasConsultingPurchasePrefix(text)) &&
		(hasBuy || mentionsOrderQty(text)) {
		if !hasExplicitCartReadyPhrase(text) {
			return true
		}
	}
	return false
}

// isNamedProductPurchaseIntent — "mau beli abon sapi ..." dengan produk disebut eksplisit di pesan.
func isNamedProductPurchaseIntent(userText string, catalog []dbCatalogItem) bool {
	match := matchCatalogItem(userText, catalog)
	if match == nil || !catalogProductExplicitlyNamed(userText, match) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if strings.Contains(text, "mau beli") {
		return true
	}
	if hasOrderIntentText(userText) && !hasConsultingPurchasePrefix(text) {
		return true
	}
	return false
}

func catalogProductExplicitlyNamed(userText string, it *dbCatalogItem) bool {
	if it == nil {
		return false
	}
	text := strings.ToLower(userText)
	stop := map[string]bool{"pcs": true, "paket": true, "pc": true, "lusin": true}
	named := 0
	for _, tok := range tokenize(it.Name) {
		t := strings.ToLower(tok)
		if len(t) < 3 || stop[t] {
			continue
		}
		if strings.Contains(text, t) {
			named++
		}
	}
	return named >= 1
}

func isOrderProductContinuationStep(step string) bool {
	return step == "ask_product" || step == "ask_variant" || step == "ask_qty"
}

func messageNamesCatalogProduct(userText string, catalog []dbCatalogItem) bool {
	return len(catalog) > 0 && matchCatalogItem(userText, catalog) != nil
}

func hasConsultingPurchasePrefix(text string) bool {
	for _, p := range consultingPurchasePrefixes {
		if strings.HasPrefix(text, p+" ") || strings.HasPrefix(text, p) ||
			strings.Contains(text, " "+p+" ") || strings.Contains(text, " "+p) {
			return true
		}
	}
	return false
}

func normalizeBuyerTextForRules(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	repl := strings.NewReplacer("pengen", "mau", "pengin", "mau", "gw ", "saya ", "gue ", "saya ")
	return repl.Replace(text)
}

// IsUserSalesCorrection — user menolak checkout / koreksi AI ("masih tanya", "jangan checkout dulu").
func IsUserSalesCorrection(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	if idx := strings.Index(text, ","); idx >= 0 {
		tail := strings.TrimSpace(text[idx+1:])
		if tail != "" && (matchesSalesCorrectionPhrases(tail) || isConfusionOnly(tail) || isProductDenialCorrection(tail)) {
			return true
		}
	}
	if matchesSalesCorrectionPhrases(text) || isConfusionOnly(text) || isProductDenialCorrection(text) {
		return true
	}
	return false
}

func isProductDenialCorrection(text string) bool {
	if !strings.Contains(text, "bukan") {
		return false
	}
	for _, p := range []string{
		"bukan abon", "bukan api", "bukan sapi", "bukan itu produk", "bukan produk",
		"salah produk", "bukan yang ini", "bukan barang",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func matchesSalesCorrectionPhrases(text string) bool {
	for _, p := range salesCorrectionPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func isConfusionOnly(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	text = strings.TrimRight(text, "?!. ")
	if text == "" {
		return false
	}
	fields := strings.Fields(strings.NewReplacer("?", " ", "!", " ").Replace(text))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "ha", "hah", "hei", "eh", "loh", "lah", "kok":
		return len(fields) <= 2
	}
	return false
}

func salesCorrectionReply(formal bool) string {
	if formal {
		return "Mohon maaf kak, sepertinya ada miss. Saya bantu jawab pertanyaannya dulu ya."
	}
	return "Maaf kak, saya kira kakak sudah mau pesan 😊\n\nSaya bantu jawab dulu ya."
}

func prependSalesCorrection(formal bool, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return salesCorrectionReply(formal)
	}
	return strings.TrimSpace(salesCorrectionReply(formal) + "\n\n" + body)
}

func orderFlowLoopBreakReply(formal bool) string {
	if formal {
		return "Mohon maaf kak, sepertinya ada miss komunikasi.\n\nKakak mau tanya produk atau sudah siap pesan? Sebut saja ya."
	}
	return "Maaf kak, saya bingung maksudnya 😅\n\nKakak mau tanya produk atau lanjut pesan? Sebut aja ya."
}

// wouldRepeatOutbound — anti-loop: jangan kirim pertanyaan yang sama 2× berturut-turut.
func wouldRepeatOutbound(history []dbMessage, outbound string) bool {
	want := normalizeOutboundCompare(outbound)
	if want == "" {
		return false
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Direction != "out" {
			continue
		}
		prev := normalizeOutboundCompare(history[i].Body)
		if prev == "" {
			return false
		}
		return prev == want || strings.Contains(prev, want) || strings.Contains(want, prev)
	}
	return false
}

func normalizeOutboundCompare(body string) string {
	s := strings.ToLower(strings.TrimSpace(body))
	if i := strings.Index(s, "💡 rekomendasi"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, "🛒 ringkasan"); i >= 0 {
		s = strings.TrimSpace(s[i:])
	}
	return strings.TrimSpace(s)
}
