package buyerflow

import (
	"regexp"
	"strings"
)

var orderQtyPackRe = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,4})\s*paket`)

// orderRevisionSignals — inti deteksi revisi qty (tanpa memanggil IsPricingUnitClarification).
func orderRevisionSignals(text, userText string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, "bukan") &&
		(strings.Contains(text, "paket") || strings.Contains(text, "pcs") || strings.Contains(text, "biji")) {
		return true
	}
	hasRevisionVerb := strings.Contains(text, "ubah") || strings.Contains(text, "rubah") ||
		strings.Contains(text, "revisi") || strings.Contains(text, "ganti") ||
		strings.Contains(text, "gantiin") || strings.Contains(text, "make it") ||
		strings.Contains(text, "change it to") || strings.Contains(text, "change to")
	if hasRevisionVerb {
		if _, ok := parseOrderQty(userText); ok {
			return true
		}
		if strings.Contains(text, "paket") || strings.Contains(text, "jadi") {
			return true
		}
	}
	return false
}

// IsOrderRevisionMessage — koreksi/revisi jumlah pesanan (bukan pesanan baru / tanya harga).
func IsOrderRevisionMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	return orderRevisionSignals(text, userText)
}

// qtyNumberWord — Indonesian number words parseOrderQty also understands.
const qtyNumberWord = `satu|dua|tiga|empat|lima|enam|tujuh|delapan|sembilan|sepuluh`

var (
	// explicitQtyUnitRe — a unit word attached to a number ("2 pcs", "3pcs", "dua buah", "pcs: 3").
	// A bare unit word does not count, so "Jl. Buah Batu No 12" is not a quantity statement.
	explicitQtyUnitRe = regexp.MustCompile(
		`(?:\d|\b(?:` + qtyNumberWord + `))\s*(?:pcs|pc|buah|biji|porsi|pack|paket)\b` +
			`|\b(?:pcs|pc|buah|biji|porsi|pack|paket)\s*[:=]?\s*(?:\d|\b(?:` + qtyNumberWord + `)\b)`)
	explicitQtyTimesRe = regexp.MustCompile(`\b\d+\s*x\b`)
)

// qtyRevisionCueWords — verbs/labels with which a buyer revises the quantity.
var qtyRevisionCueWords = []string{
	"qty", "jumlah", "jadi ", "ubah", "revisi", "ganti", "make it", "change to", "change it to",
}

// hasExplicitQtyCue — the (lowercased) text states a quantity, not just digits.
func hasExplicitQtyCue(lowerText string) bool {
	for _, cue := range qtyRevisionCueWords {
		if strings.Contains(lowerText, cue) {
			return true
		}
	}
	return explicitQtyUnitRe.MatchString(lowerText) || explicitQtyTimesRe.MatchString(lowerText)
}

func isRecipientOrAddressStep(step string) bool {
	switch step {
	case "ask_recipient", "ask_address", "ask_address_full":
		return true
	}
	return false
}

// tryApplyQtyRevision updates qty when customer revises count mid-checkout.
func tryApplyQtyRevision(st *OrderState, userText string) bool {
	if st == nil {
		return false
	}
	// At the recipient/address steps digits are a phone number, house number or postal code
	// unless the buyer states a quantity explicitly.
	if isRecipientOrAddressStep(st.Step) && !hasExplicitQtyCue(strings.ToLower(userText)) {
		return false
	}
	q, ok := parseOrderQty(userText)
	if !ok || q < 1 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	isRevision := IsOrderRevisionMessage(userText) ||
		strings.Contains(text, "ubah") || strings.Contains(text, "revisi") ||
		strings.Contains(text, "ganti") || (st.Qty > 0 && q != st.Qty)
	if !isRevision {
		return false
	}
	st.Qty = q
	if len(st.Items) == 1 {
		st.Items[0].Qty = q
	} else if st.CatalogItemID != "" {
		for i := range st.Items {
			if st.Items[i].CatalogItemID == st.CatalogItemID {
				st.Items[i].Qty = q
				break
			}
		}
	}
	return true
}

var addOneMoreRe = regexp.MustCompile(`(?i)^(?:tambah|nambah)\s*1\s+lagi[.!?]*$|^1\s+lagi[.!?]*$`)

// tryApplyAddOneMore increments qty by one when the buyer says "tambah 1 lagi" with a single-line cart.
func tryApplyAddOneMore(st *OrderState, userText string) bool {
	if st == nil || !addOneMoreRe.MatchString(strings.TrimSpace(userText)) {
		return false
	}
	ensureMultiItemsFromSingle(st)
	if len(st.Items) == 1 {
		st.Items[0].Qty++
		st.Qty = st.Items[0].Qty
		return true
	}
	if len(st.Items) == 0 && st.Qty > 0 {
		st.Qty++
		return true
	}
	return false
}

// IsCasualPraiseLike — pujian singkat ke bot, tetap in-scope.
func IsCasualPraiseLike(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || len(strings.Fields(text)) > 10 {
		return false
	}
	signals := []string{
		"pinter", "pandai", "mantap", "keren", "bagus", "top", "jos", "joss",
		"oke udah", "nah oke", "sip udah", "lu udah", "kamu udah",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func casualPraiseReply(formal bool, userText string) string {
	idFormal := "Terima kasih kak 😊 Ada yang masih ingin ditanyakan atau mau lanjut pesan?"
	idCasual := "Makasih kak 😊 Ada yang masih mau ditanyain atau mau lanjut pesan?"
	enFormal := "Thank you 😊 Anything else you'd like to ask, or ready to continue your order?"
	enCasual := "Thanks 😊 Anything else you want to ask, or want to keep ordering?"
	if formal {
		return PickBuyerLanguageReply(userText, true, idFormal, enFormal)
	}
	return PickBuyerLanguageReply(userText, false, idCasual, enCasual)
}

func inferVariantFromProductName(st *OrderState) {
	if st == nil || strings.TrimSpace(st.Size) != "" {
		return
	}
	if sz := extractSizeFromProductName(st.ProductName); sz != "" {
		st.Size = sz
	}
}
