package buyerflow

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Explicit qty with whitespace before unit (avoids matching "1PCS" in catalog product titles).
	orderQtyWithUnitRe = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,4})\s+(pcs|pc|biji|buah|item|unit|piece|pieces|paket|pket)\b`)
	orderQtyLabelRe    = regexp.MustCompile(`(?i)\b(?:qty|jumlah)\s*[:\-]?\s*(\d{1,4})\b`)
	orderQtyBareLineRe = regexp.MustCompile(`(?i)^\s*(\d{1,4})\s*(?:biji|pcs|pc|buah|piece|pieces|paket)?\s*[!.?]*\s*$`)
	orderQtyLusinRe    = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,3})\s*lusin\b`)
	orderQtyOneLusinRe = regexp.MustCompile(`(?i)(?:^|\s)1\s*lusin\b|satu\s*lusin`)
	orderQtyIndoWordRe = regexp.MustCompile(`(?i)\b(satu|dua|tiga|empat|lima|enam|tujuh|delapan|sembilan|sepuluh)\s*(pcs|pc|biji|buah|piece|pieces|lusin)?\b`)
	gluedCatalogQtyRe  = regexp.MustCompile(`(?i)\d+pcs`)
	// Longest-first so XXL is not parsed as XL and XL is not parsed as L.
	orderSizeLineRe = regexp.MustCompile(`(?i)\b(xxxl|3xl|xxl|xl|4xl|5xl|xs|m|l|s|\d{2})\b`)
)

// orderFlowTemplates — default WA copy; overridden by knowledge_base_entry when matched.
type orderFlowTemplates struct {
	AskProduct     string
	AskVariant     string
	AskQty         string
	AskRecipient   string
	AskAddressFull string
	Complete       string
	RetryStep      string
	ClarifyQty     string
	ClarifyAddress string
}

func defaultOrderTemplates(formal bool) orderFlowTemplates {
	if formal {
		return orderFlowTemplates{
			AskProduct: "Siap kak. Sebutkan nama produk dari katalog kami (contoh: Jiniso Highwaist).",
			AskVariant: "Baik kak. Tulis ukuran (S/M/L/XL) dan warna yang diinginkan ya.",
			AskQty:     "Siap kak. Mau pesan berapa pcs?",
			AskRecipient: "Terima kasih kak. Tulis nama penerima dan nomor HP/WA aktif ya.\n" +
				"Contoh:\nNama: Budi Santoso\nHP: 081234567890",
			AskAddressFull: "Mohon kirim alamat pengiriman lengkap (format resmi Indonesia):\n" +
				"Jalan: ...\nRT/RW: ... (jika ada)\nKelurahan: ...\nKecamatan: ...\nKota/Kab: ...\nProvinsi: ...\nKode pos: 12345",
			ClarifyAddress: "Alamat belum lengkap kak. Pastikan ada jalan, kota, provinsi, dan kode pos 5 digit ya.",
			Complete:       "Sip kak, datanya sudah lengkap. Tim CS kami akan segera konfirmasi order kakak ya 🙏",
			RetryStep:      "Boleh kak lanjutkan data ordernya, nanti tim CS bantu proses sampai selesai ya.",
			ClarifyQty:     "Maaf kak, jumlahnya berapa pcs ya? (contoh: 1 pcs)",
		}
	}
	return orderFlowTemplates{
		AskProduct: "Siap kak. Mau order produk apa? Sebut nama produk dari katalog ya.",
		AskVariant: "Oke kak. Ukuran (S/M/L/XL) dan warnanya apa?",
		AskQty:     "Siap kak, mau pesan berapa pcs?",
		AskRecipient: "Makasih kak. Kirim nama penerima + no HP/WA ya.\n" +
			"Contoh:\nNama: Budi\nHP: 081234567890",
		AskAddressFull: "Kirim alamat lengkap ya (format Indonesia):\n" +
			"Jalan: ...\nRT/RW: ...\nKelurahan: ...\nKecamatan: ...\nKota/Kab: ...\nProvinsi: ...\nKode pos: 12345",
		ClarifyAddress: "Alamatnya belum lengkap kak — butuh jalan, kota, provinsi, dan kode pos 5 digit.",
		Complete:       "Sip kak, datanya sudah lengkap. Tim CS akan konfirmasi ordernya ya 🙏",
		RetryStep:      "Boleh kak lanjutin data ordernya, nanti tim CS bantu sampai selesai ya.",
		ClarifyQty:     "Maaf kak, jumlahnya berapa pcs? (misalnya: 1 pcs)",
	}
}

// orderTemplatesFromKB uses KB answers when question tags match order-flow steps.
func orderTemplatesFromKB(kb []KBEntry, formal bool) orderFlowTemplates {
	t := defaultOrderTemplates(formal)
	for _, e := range kb {
		a := strings.TrimSpace(e.Answer)
		if a == "" {
			continue
		}
		q := strings.ToLower(e.Question)
		cat := strings.ToLower(strings.TrimSpace(strOrEmpty(e.Category)))
		if cat != "order" && cat != "order_flow" && !strings.Contains(q, "order") {
			continue
		}
		switch {
		case strings.Contains(q, "produk") || strings.Contains(q, "barang"):
			t.AskProduct = a
		case strings.Contains(q, "varian") || strings.Contains(q, "ukuran") || strings.Contains(q, "size"):
			t.AskVariant = a
		case strings.Contains(q, "pcs") || strings.Contains(q, "jumlah") || strings.Contains(q, "qty"):
			t.AskQty = a
		case strings.Contains(q, "penerima") || strings.Contains(q, "nama") && strings.Contains(q, "hp"):
			t.AskRecipient = a
		case strings.Contains(q, "alamat") || strings.Contains(q, "pengiriman"):
			t.AskAddressFull = a
		case strings.Contains(q, "selesai") || strings.Contains(q, "konfirmasi"):
			t.Complete = a
		}
	}
	return t
}

type parsedOrderHints struct {
	Product string
	Variant string
	Qty     int
	HasQty  bool
	HasSize bool
}

var indoQtyWords = map[string]int{
	"satu": 1, "dua": 2, "tiga": 3, "empat": 4, "lima": 5,
	"enam": 6, "tujuh": 7, "delapan": 8, "sembilan": 9, "sepuluh": 10,
}

func parseQtyFromLine(line string) (int, bool) {
	lower := strings.ToLower(strings.TrimSpace(line))
	if orderQtyOneLusinRe.MatchString(lower) {
		return 12, true
	}
	if m := orderQtyLusinRe.FindStringSubmatch(line); len(m) > 1 {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		if n > 0 {
			return n * 12, true
		}
	}
	if m := orderQtyIndoWordRe.FindStringSubmatch(lower); len(m) > 2 {
		if n, ok := indoQtyWords[m[1]]; ok {
			unit := strings.ToLower(m[2])
			if unit == "lusin" {
				return n * 12, true
			}
			if unit != "" || n > 0 {
				return n, true
			}
		}
	}
	if m := orderQtyLabelRe.FindStringSubmatch(line); len(m) > 1 {
		var q int
		fmt.Sscanf(m[1], "%d", &q)
		return q, q > 0
	}
	if m := orderQtyWithUnitRe.FindStringSubmatch(line); len(m) > 1 {
		var q int
		fmt.Sscanf(m[1], "%d", &q)
		return q, q > 0
	}
	if m := orderQtyPackRe.FindStringSubmatch(line); len(m) > 1 {
		var q int
		fmt.Sscanf(m[1], "%d", &q)
		return q, q > 0
	}
	if orderQtyBareLineRe.MatchString(line) {
		if m := orderQtyBareLineRe.FindStringSubmatch(line); len(m) > 1 {
			var q int
			fmt.Sscanf(m[1], "%d", &q)
			return q, q > 0
		}
	}
	return 0, false
}

// parseOrderQty extracts customer-requested quantity, ignoring catalog prefixes like "1PCS" in product names.
func parseOrderQty(text string) (int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	best := 0
	found := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if gluedCatalogQtyRe.MatchString(line) && !orderQtyWithUnitRe.MatchString(line) && !orderQtyLabelRe.MatchString(line) {
			if len(line) > 25 {
				continue
			}
		}
		if q, ok := parseQtyFromLine(line); ok {
			best = q
			found = true
		}
	}
	if found {
		return best, true
	}
	if q, ok := parseQtyFromLine(text); ok {
		return q, true
	}
	if m := orderQtyLabelRe.FindStringSubmatch(text); len(m) > 1 {
		var q int
		fmt.Sscanf(m[1], "%d", &q)
		if q > 0 {
			return q, true
		}
	}
	if m := orderQtyWithUnitRe.FindStringSubmatch(text); len(m) > 1 {
		var q int
		fmt.Sscanf(m[1], "%d", &q)
		if q > 0 {
			return q, true
		}
	}
	return 0, false
}

func mentionsOrderQty(text string) bool {
	_, ok := parseOrderQty(text)
	return ok
}

func parseOrderHints(userText string) parsedOrderHints {
	text := strings.TrimSpace(userText)
	lower := strings.ToLower(text)
	var out parsedOrderHints

	if m := orderSizeLineRe.FindString(text); m != "" {
		out.Variant = m
		out.HasSize = true
	}
	if qty, ok := parseOrderQty(text); ok {
		out.Qty = qty
		out.HasQty = true
	}
	out.Product = strings.TrimSpace(text)
	if len(out.Product) > 200 {
		out.Product = out.Product[:200]
	}
	_ = lower
	return out
}

// IsOrderContinuationMessage is true for qty/size/address lines during an active order (e.g. "1 pcs saja").
func IsOrderContinuationMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if mentionsOrderQty(text) {
		return true
	}
	if orderSizeLineRe.MatchString(text) {
		return true
	}
	for _, kw := range []string{
		"pcs", "pc", "biji", "buah", "piece", "pieces", "qty", "jumlah", "unit",
		"alamat", "jalan", "jl.", "rt", "rw", "kel.", "kec.", "kota", "kab.", "kode pos",
		"kodepos", "penerima", "provinsi", "kelurahan", "kecamatan",
	} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// OrderIntentKeywords — purchase / checkout phrases (incl. common Indonesian variants).
var OrderIntentKeywords = []string{
	"order", "pesan", "pesen", "pesin", "beli", "checkout",
	"jadi ambil", "jadi beli", "nyesan",
}

var orderIntentWordRe = regexp.MustCompile(`(?i)\b(order|pesan|pesanan|pesen|pesin|beli|checkout|nyesan)\b`)

func hasOrderIntentText(userText string) bool {
	text := strings.ToLower(userText)
	if orderIntentWordRe.MatchString(text) {
		return true
	}
	for _, kw := range OrderIntentKeywords {
		if strings.Contains(kw, " ") && strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// HasPurchaseIntent — CART_READY: checkout eksplisit tanpa "?", bukan pertanyaan konsultasi.
func HasPurchaseIntent(userText string) bool {
	return hasPurchaseIntent(userText, nil)
}

func hasPurchaseIntent(userText string, catalog []CatalogItem) bool {
	if IsConsultingPurchaseQuestion(userText, catalog) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if strings.Contains(text, "cari") && !hasOrderIntentText(userText) && !hasExplicitCartReadyPhrase(text) &&
		!mentionsOrderQty(text) {
		return false
	}
	if text == "" || IsQuestionLike(userText) {
		return false
	}
	if hasExplicitCartReadyPhrase(text) {
		return true
	}
	hasWant := strings.Contains(text, "mau") || strings.Contains(text, "pengen") ||
		strings.Contains(text, "pengin") || strings.Contains(text, "ingin")
	if hasWant {
		if mentionsOrderQty(text) || orderSizeLineRe.MatchString(text) {
			return true
		}
		for _, kw := range apparelProductKeywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	if mentionsOrderQty(text) {
		for _, kw := range apparelProductKeywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	if hasOrderIntentText(userText) {
		if mentionsOrderQty(text) || orderSizeLineRe.MatchString(text) {
			return true
		}
		for _, kw := range apparelProductKeywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	if len(catalog) > 0 {
		if match := matchCatalogItem(userText, catalog); match != nil {
			if _, ok := parseOrderQty(userText); ok {
				return true
			}
			if hasWant || hasOrderIntentText(userText) {
				return true
			}
		}
	}
	return false
}

// IsStoreLocationQuestion — "tokonya di kota mana", "alamat toko dimana".
func IsStoreLocationQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if strings.Contains(text, "alamat toko") || strings.Contains(text, "lokasi toko") {
		return true
	}
	hasStore := strings.Contains(text, "toko") || strings.Contains(text, "belanja") || strings.Contains(text, "offline")
	hasWhere := strings.Contains(text, "mana") || strings.Contains(text, "dimana") ||
		strings.Contains(text, "dimananya") || strings.Contains(text, "lokasi") ||
		strings.Contains(text, "kota") || strings.Contains(text, "daerah")
	if hasStore && hasWhere {
		return true
	}
	if strings.Contains(text, "tokonya") || strings.Contains(text, "toko nya") || strings.Contains(text, "toko kamu") {
		return hasWhere || strings.Contains(text, "?")
	}
	return false
}

// IsShippingQuoteQuestion — minta hitung ongkir, bukan jawaban langkah order.
func IsShippingQuoteQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if !strings.Contains(text, "ongkir") && !strings.Contains(text, "ongkos kirim") {
		return false
	}
	return strings.Contains(text, "?") || strings.Contains(text, "berapa") ||
		strings.Contains(text, "hitung") || strings.Contains(text, "minta tolong") ||
		strings.Contains(text, "tanya") || strings.Contains(text, "kena")
}

// IsAcknowledgmentLike — ucapan terima kasih / oke singkat setelah checkout.
func IsAcknowledgmentLike(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || len(strings.Fields(text)) > 8 {
		return false
	}
	phrases := []string{
		"terima kasih", "makasih", "thanks", "thank you", "thx",
		"oke terima", "ok terima", "siap terima", "baik terima",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return text == "oke" || text == "ok" || text == "siap" || text == "sip"
}

// IsOrderFollowUpFromHistory — qty/size reply after bot asked about order (no Redis state yet).
func IsOrderFollowUpFromHistory(history []Message, userText string) bool {
	if IsStoreLocationQuestion(userText) || IsShippingQuoteQuestion(userText) {
		return false
	}
	if !IsOrderContinuationMessage(userText) && !HasPurchaseIntent(userText) {
		return false
	}
	if len(strings.Fields(strings.TrimSpace(userText))) > 14 {
		return false
	}
	var lastOut []string
	for i := len(history) - 1; i >= 0 && len(lastOut) < 3; i-- {
		if history[i].Direction != "out" {
			continue
		}
		lastOut = append(lastOut, strings.ToLower(strings.TrimSpace(history[i].Body)))
	}
	for _, out := range lastOut {
		if out == "" {
			continue
		}
		askedQty := (strings.Contains(out, "berapa") || strings.Contains(out, "jumlah")) &&
			(strings.Contains(out, "pcs") || strings.Contains(out, "banyak") || strings.Contains(out, "unit"))
		askedVariant := strings.Contains(out, "varian") || strings.Contains(out, "ukuran") ||
			strings.Contains(out, "size") || strings.Contains(out, "warna")
		askedOrder := strings.Contains(out, "proses pesanan") || strings.Contains(out, "jenis jeans") ||
			strings.Contains(out, "mau order") || strings.Contains(out, "sebutkan pilihan")
		inOrderFlow := strings.Contains(out, "data order") || strings.Contains(out, "alamat pengiriman") ||
			strings.Contains(out, "kode pos") || strings.Contains(out, "nama penerima")
		if askedQty || askedVariant || askedOrder {
			return true
		}
		if inOrderFlow {
			return IsOrderContinuationMessage(userText) || mentionsOrderQty(userText)
		}
	}
	return false
}

// IsActiveCheckoutFromHistory — payment/total follow-up after a recent order or checkout reply.
func IsActiveCheckoutFromHistory(history []Message, userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	paymentHint := strings.Contains(text, "transfer") || strings.Contains(text, "trf") ||
		strings.Contains(text, "bayar") || strings.Contains(text, "pembayaran") ||
		strings.Contains(text, "cod") || strings.Contains(text, "qris") ||
		strings.Contains(text, "rekening") || strings.Contains(text, "bukti")
	if IsShippingQuoteQuestion(userText) || IsStoreLocationQuestion(userText) {
		return false
	}
	totalHint := strings.Contains(text, "total") ||
		(strings.Contains(text, "berapa") && strings.Contains(text, "semua"))
	if !paymentHint && !totalHint && !IsQuestionLike(userText) && !IsAcknowledgmentLike(userText) {
		return false
	}
	var lastOut []string
	for i := len(history) - 1; i >= 0 && len(lastOut) < 4; i-- {
		if history[i].Direction != "out" {
			continue
		}
		lastOut = append(lastOut, strings.ToLower(strings.TrimSpace(history[i].Body)))
	}
	for _, out := range lastOut {
		if out == "" {
			continue
		}
		if strings.Contains(out, "order") || strings.Contains(out, "pesan") ||
			strings.Contains(out, "ongkir") || strings.Contains(out, "konfirmasi") ||
			strings.Contains(out, "datanya sudah lengkap") || strings.Contains(out, "alamat pengiriman") ||
			strings.Contains(out, "total harga") || strings.Contains(out, "biaya pengiriman") {
			return true
		}
	}
	return false
}

var orderAddrHintRe = regexp.MustCompile(`(?i)(jalan|\bjl\.?\b|rt|rw|kel\.|kec\.|kota|kab\.|kode pos|taman|setiabudi)`)

// ShouldBreakOrderFlow — new intent (greeting, harga, tanya produk) while Redis order state is active.
func ShouldBreakOrderFlow(userText, step string, catalog []CatalogItem) bool {
	if IsOrderRevisionMessage(userText) {
		return false
	}
	if IsUserSalesCorrection(userText) {
		return true
	}
	if IsStructuredOrderList(userText) || IsExplicitNewOrderStart(userText) {
		return true
	}
	if isOrderProductContinuationStep(step) && messageNamesCatalogProduct(userText, catalog) {
		return false
	}
	if IsConsultingPurchaseQuestion(userText, catalog) {
		return true
	}
	if IsCatalogBrowsingIntent(userText) || isGeneralStoreCatalogQuestion(userText) {
		return true
	}
	if IsOrderFlowCancelled(userText) {
		return true
	}
	if IsGreetingLike(userText) {
		return true
	}
	if IsStoreLocationQuestion(userText) || IsShippingQuoteQuestion(userText) {
		return true
	}
	if IsAcknowledgmentLike(userText) {
		return true
	}

	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}

	// Price / catalog questions are not order-step answers.
	if (strings.Contains(text, "harga") || strings.Contains(text, "berapa") ||
		strings.Contains(text, "stok") || strings.Contains(text, "ongkir") ||
		strings.Contains(text, "ready")) &&
		(IsQuestionLike(userText) || strings.Contains(text, "?") || strings.Contains(text, "tanya")) {
		if messageNamesCatalogProduct(userText, catalog) {
			return false
		}
		if !mentionsOrderQty(text) || strings.Contains(text, "berapa") || strings.Contains(text, "harga") {
			return true
		}
	}

	// "mau tanya jeans" / info produk — bukan melanjutkan form order.
	if strings.Contains(text, "tanya") && !strings.Contains(text, "pesan") &&
		step != "ask_address_full" && step != "ask_address" && step != "ask_recipient" {
		return true
	}

	// Stuck on address/recipient steps but user sends unrelated chat.
	if step == "ask_address" || step == "ask_address_full" || step == "ask_recipient" {
		if orderAddrHintRe.MatchString(text) || postalCodeIDRe.MatchString(text) {
			return false
		}
		if phoneIDRe.MatchString(text) {
			return false
		}
		if name, _ := parseRecipientLine(userText); name != "" {
			return false
		}
		if strings.Contains(text, "nama:") || strings.Contains(text, "nama :") ||
			strings.Contains(text, "hp:") || strings.Contains(text, "hp :") {
			return false
		}
		if IsOrderContinuationMessage(userText) && !strings.Contains(text, "berapa") {
			return false
		}
		return true
	}

	return false
}

func normalizeOrderState(st OrderState) OrderState {
	if st.ProductName == "" && strings.TrimSpace(st.Product) != "" {
		st.ProductName = strings.TrimSpace(st.Product)
	}
	if st.Variant != "" && st.Size == "" && st.Color == "" {
		sz, cl := parseSizeAndColor(st.Variant)
		if sz != "" {
			st.Size = sz
		}
		if cl != "" {
			st.Color = cl
		}
	}
	switch st.Step {
	case "ask_address":
		st.Step = "ask_address_full"
	}
	inferVariantFromProductName(&st)
	if len(st.Items) > 0 && strings.TrimSpace(st.ProductName) == "" {
		applyLineToOrderState(&st, st.Items[0])
	}
	return st
}

func orderFlowCancelReply(tone string) string {
	if tone == "formal" {
		return "Baik kak, ordernya kami batalkan dulu ya. Silakan tanya produk, harga, atau stok jika ada yang dibutuhkan."
	}
	return "Oke kak, ordernya dibatalkan dulu ya 😊 Mau tanya produk atau harga, langsung chat aja."
}
