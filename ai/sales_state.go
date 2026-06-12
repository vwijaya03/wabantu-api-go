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

// IsConsultingPurchaseQuestion — "boleh beli 1 pcs?", "kalau order satu bisa?" (CONSULTING, bukan CART_READY).
func IsConsultingPurchaseQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, s := range retailPolicySignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	hasBuy := hasOrderIntentText(userText) || strings.Contains(text, "mau")
	for _, p := range consultingPurchasePrefixes {
		if strings.HasPrefix(text, p+" ") || strings.HasPrefix(text, p) ||
			strings.Contains(text, " "+p+" ") || strings.Contains(text, " "+p) {
			if hasBuy || mentionsOrderQty(text) {
				return true
			}
		}
	}
	if IsQuestionLike(userText) && (hasBuy || mentionsOrderQty(text)) {
		if !hasExplicitCartReadyPhrase(text) {
			return true
		}
	}
	return false
}

// IsUserSalesCorrection — user menolak checkout / koreksi AI ("masih tanya", "jangan checkout dulu").
func IsUserSalesCorrection(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, p := range salesCorrectionPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	if isConfusionOnly(userText) {
		return true
	}
	return false
}

func isConfusionOnly(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	text = strings.TrimRight(text, "?!. ")
	switch text {
	case "ha", "hah", "hei", "eh", "loh", "lah", "kok":
		return true
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
