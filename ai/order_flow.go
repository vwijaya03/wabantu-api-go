package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"encore.dev/rlog"
)

var (
	orderQtyLineRe = regexp.MustCompile(`(?i)\b(\d{1,4})\s*(pcs|pc|biji|buah|item|unit)?\b`)
	orderSizeLineRe = regexp.MustCompile(`(?i)\b(xs|s|m|l|xl|xxl|xxxl|3xl|4xl|5xl|\d{2})\b`)
)

// orderFlowTemplates — default WA copy; overridden by knowledge_base_entry when matched.
type orderFlowTemplates struct {
	AskProduct  string
	AskVariant  string
	AskQty      string
	AskAddress  string
	Complete    string
	RetryStep   string
	ClarifyQty  string
}

func defaultOrderTemplates(formal bool) orderFlowTemplates {
	if formal {
		return orderFlowTemplates{
			AskProduct: "Siap kak, mau order produk yang mana ya? Sekalian tulis varian/size kalau ada.",
			AskVariant: "Baik kak. Untuk variannya apa ya (mis. size/warna)?",
			AskQty:     "Siap kak. Mau pesan berapa pcs?",
			AskAddress: "Terima kasih kak. Boleh kirim alamat pengiriman lengkapnya ya.",
			Complete:   "Sip kak, datanya sudah lengkap. Tim CS kami akan segera konfirmasi order kakak ya 🙏",
			RetryStep:  "Boleh kak lanjutkan data ordernya, nanti tim CS bantu proses sampai selesai ya.",
			ClarifyQty: "Maaf kak, jumlahnya berapa pcs ya? (contoh: 1 pcs)",
		}
	}
	return orderFlowTemplates{
		AskProduct: "Siap kak, mau order produk yang mana? Tulis juga varian/size kalau ada ya.",
		AskVariant: "Oke kak, varian/size-nya apa ya?",
		AskQty:     "Siap kak, mau pesan berapa pcs?",
		AskAddress: "Makasih kak. Boleh kirim alamat pengiriman lengkapnya ya.",
		Complete:   "Sip kak, datanya sudah lengkap. Tim CS akan konfirmasi ordernya ya 🙏",
		RetryStep:  "Boleh kak lanjutin data ordernya, nanti tim CS bantu sampai selesai ya.",
		ClarifyQty: "Maaf kak, jumlahnya berapa pcs? (misalnya: 1 pcs)",
	}
}

// orderTemplatesFromKB uses KB answers when question tags match order-flow steps.
func orderTemplatesFromKB(kb []dbKBEntry, formal bool) orderFlowTemplates {
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
		case strings.Contains(q, "alamat") || strings.Contains(q, "pengiriman"):
			t.AskAddress = a
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

func parseOrderHints(userText string) parsedOrderHints {
	text := strings.TrimSpace(userText)
	lower := strings.ToLower(text)
	var out parsedOrderHints

	if m := orderSizeLineRe.FindString(text); m != "" {
		out.Variant = m
		out.HasSize = true
	}
	if m := orderQtyLineRe.FindStringSubmatch(text); len(m) > 1 {
		fmt.Sscanf(m[1], "%d", &out.Qty)
		if out.Qty > 0 {
			out.HasQty = true
		}
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
	if orderQtyLineRe.MatchString(text) {
		return true
	}
	if orderSizeLineRe.MatchString(text) {
		return true
	}
	for _, kw := range []string{
		"pcs", "pc", "biji", "buah", "qty", "jumlah", "unit",
		"alamat", "jalan", "jl.", "rt", "rw", "kel.", "kec.", "kota", "kab.", "kode pos",
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

func hasOrderIntentText(userText string) bool {
	text := strings.ToLower(userText)
	for _, kw := range OrderIntentKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// HasPurchaseIntent — in-scope checkout lines without "?" (e.g. "mau 1 pcs jiniso highwaist XL").
func HasPurchaseIntent(userText string) bool {
	if hasOrderIntentText(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	hasWant := strings.Contains(text, "mau") || strings.Contains(text, "pengen") ||
		strings.Contains(text, "pengin") || strings.Contains(text, "ingin")
	if hasWant {
		if orderQtyLineRe.MatchString(text) || orderSizeLineRe.MatchString(text) {
			return true
		}
		for _, kw := range apparelProductKeywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	if orderQtyLineRe.MatchString(text) {
		for _, kw := range apparelProductKeywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	return false
}

// IsOrderFollowUpFromHistory — qty/size reply after bot asked about order (no Redis state yet).
func IsOrderFollowUpFromHistory(history []dbMessage, userText string) bool {
	if !IsOrderContinuationMessage(userText) && !HasPurchaseIntent(userText) {
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
		inOrderFlow := strings.Contains(out, "data order") || strings.Contains(out, "alamat pengiriman")
		if askedQty || askedVariant || askedOrder || inOrderFlow {
			return true
		}
	}
	return false
}

// IsActiveCheckoutFromHistory — payment/total follow-up after a recent order or checkout reply.
func IsActiveCheckoutFromHistory(history []dbMessage, userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	paymentHint := strings.Contains(text, "transfer") || strings.Contains(text, "trf") ||
		strings.Contains(text, "bayar") || strings.Contains(text, "pembayaran") ||
		strings.Contains(text, "cod") || strings.Contains(text, "qris") ||
		strings.Contains(text, "rekening") || strings.Contains(text, "bukti")
	totalHint := strings.Contains(text, "total") || strings.Contains(text, "ongkir") ||
		(strings.Contains(text, "berapa") && (strings.Contains(text, "semua") || strings.Contains(text, "ongkir")))
	if !paymentHint && !totalHint && !IsQuestionLike(userText) {
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

var orderCancelPhrases = []string{
	"tidak jadi", "gak jadi", "ga jadi", "nggak jadi", "tidak order", "gak order",
	"batal order", "batal pesan", "cancel order", "batalkan order", "dibatalkan",
}

// IsOrderFlowCancelled — customer exits the order state machine.
func IsOrderFlowCancelled(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	for _, p := range orderCancelPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

var orderAddrHintRe = regexp.MustCompile(`(?i)(jalan|jl\.|rt|rw|kel\.|kec\.|kota|kab\.|kode pos)`)

// ShouldBreakOrderFlow — new intent (greeting, harga, tanya produk) while Redis order state is active.
func ShouldBreakOrderFlow(userText, step string) bool {
	if IsOrderFlowCancelled(userText) {
		return true
	}
	if IsGreetingLike(userText) {
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
		if !orderQtyLineRe.MatchString(text) || strings.Contains(text, "berapa") || strings.Contains(text, "harga") {
			return true
		}
	}

	// "mau tanya jeans" / info produk — bukan melanjutkan form order.
	if strings.Contains(text, "tanya") && !strings.Contains(text, "pesan") && step != "ask_address" {
		if strings.Contains(text, "jeans") || strings.Contains(text, "produk") ||
			strings.Contains(text, "harga") || strings.Contains(text, "ukuran") {
			return true
		}
	}

	// Stuck on ask_address but user sends chat biasa.
	if step == "ask_address" {
		if orderAddrHintRe.MatchString(text) {
			return false
		}
		if IsOrderContinuationMessage(userText) && !strings.Contains(text, "berapa") {
			return false
		}
		return true
	}

	return false
}

func orderFlowCancelReply(tone string) string {
	if tone == "formal" {
		return "Baik kak, ordernya kami batalkan dulu ya. Silakan tanya produk, harga, atau stok jika ada yang dibutuhkan."
	}
	return "Oke kak, ordernya dibatalkan dulu ya 😊 Mau tanya produk atau harga, langsung chat aja."
}

// persistDraftOrder inserts a draft row into tenant.order after the flow collects enough data.
func persistDraftOrder(
	ctx context.Context,
	db *sql.DB,
	tenantSchema string,
	convoID, contactID string,
	st orderState,
	addressLine string,
) (orderID string, err error) {
	qty := st.Qty
	if qty < 1 {
		qty = 1
	}
	item := map[string]any{
		"name":      st.Product,
		"variant":   st.Variant,
		"qty":       qty,
		"unitPrice": 0,
	}
	itemsJSON, _ := json.Marshal([]map[string]any{item})
	notes := strings.TrimSpace(addressLine)
	if st.Qty < 1 {
		if notes != "" {
			notes = "Qty estimasi 1 (belum eksplisit dari chat). " + notes
		} else {
			notes = "Qty estimasi 1 (belum eksplisit dari chat)."
		}
	}

	var convArg, contactArg any
	if strings.TrimSpace(convoID) != "" {
		convArg = convoID
	}
	if strings.TrimSpace(contactID) != "" {
		contactArg = contactID
	}

	q := fmt.Sprintf(`
		INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, notes, status, subtotal, shipping_cost, total)
		VALUES ($1, $2, $3, $4, 'draft', 0, 0, 0)
		RETURNING id::text`, tenantSchema)

	err = db.QueryRowContext(ctx, q, convArg, contactArg, itemsJSON, notes).Scan(&orderID)
	if err != nil {
		return "", err
	}
	rlog.Info("AI order: draft persisted",
		"orderId", orderID,
		"convoId", convoID,
		"product", previewText(st.Product, 60),
		"qty", qty,
	)
	return orderID, nil
}
