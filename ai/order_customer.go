package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/finance"
	"encore.app/wabantu/order"
)

// FormatOrderNumber — nomor pesanan singkat untuk pembeli (tanpa kolom DB baru).
// Contoh: WB-A1B2C3D4 dari UUID order.
func FormatOrderNumber(orderID string) string {
	id := strings.ReplaceAll(strings.TrimSpace(orderID), "-", "")
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return "WB-" + strings.ToUpper(id)
}

var orderCancelPhrases = []string{
	"tidak jadi", "gak jadi", "ga jadi", "nggak jadi", "tidak order", "gak order",
	"batal order", "batal pesan", "cancel order", "batalkan order", "dibatalkan",
	"mau batalkan", "mau saya batalkan", "saya batalkan", "batalkan ya", "batalin",
	"batalkan pesanan", "batal pesanan", "cancel pesanan",
}

var softCancelRegretPhrases = []string{
	"tidak jadi", "gak jadi", "ga jadi", "nggak jadi", "tidak order", "gak order",
}

var explicitPersistedCancelPhrases = []string{
	"batalkan pesanan", "batal pesanan", "cancel order", "batalkan order",
	"cancel pesanan", "batal order", "batal pesan", "batalkan ya", "batalin",
	"mau batalkan", "mau saya batalkan", "saya batalkan", "dibatalkan",
}

// batal | batalkan | cancel (standalone atau dalam frasa pendek).
var orderCancelWordRe = regexp.MustCompile(`(?i)\b(batal(?:kan)?|cancel)\b`)

// Nomor pesanan singkat dari chat pembeli (WB-A1B2C3D4).
var orderRefInMessageRe = regexp.MustCompile(`(?i)\bWB-([A-F0-9]{6,8})\b`)

func isRevisionNotCancel(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	return orderRevisionSignals(text, userText)
}

// IsCancelClarificationQuestion — "order mana yang dibatalkan?" (status, bukan perintah batal).
func IsCancelClarificationQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	hasCancel := orderCancelWordRe.MatchString(text) ||
		strings.Contains(text, "dibatalkan") || strings.Contains(text, "dibatalin") ||
		strings.Contains(text, "di cancel") || strings.Contains(text, "di-cancel")
	if !hasCancel {
		return false
	}
	clarify := []string{
		"mana", "which", "kok", "kenapa", "mengapa",
		"yang kamu", "yang lu", "yang lo", "yang elu",
		"order apa", "pesanan apa", "order mana", "pesanan mana",
		"yang dibatalkan", "yang dibatalin", "barusan", "tadi",
	}
	for _, s := range clarify {
		if strings.Contains(text, s) {
			return true
		}
	}
	return strings.Contains(text, "?") &&
		(strings.Contains(text, "order") || strings.Contains(text, "pesanan"))
}

// IsNewPurchaseIntentQuestion — "mau order X bisa?" bukan tanya status pesanan lama.
func IsNewPurchaseIntentQuestion(userText string) bool {
	if IsCancelClarificationQuestion(userText) {
		return false
	}
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	for _, p := range orderStatusInquiryPhrases {
		if strings.Contains(text, p) {
			return false
		}
	}
	if IsConsultingPurchaseQuestion(userText) {
		return true
	}
	wants := strings.Contains(text, "mau") || hasOrderIntentText(userText)
	if !wants {
		return false
	}
	if strings.Contains(text, "order baru") || strings.Contains(text, "pesan baru") ||
		strings.Contains(text, "order barang") || strings.Contains(text, "pesan barang") ||
		strings.Contains(text, "beli barang") {
		return true
	}
	if mentionsOrderQty(text) {
		return true
	}
	for _, kw := range apparelProductKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return hasExplicitCartReadyPhrase(text)
}

// IsDraftOrderCancelRequest — batalkan draft Redis (termasuk "batal" / "cancel" saja).
func IsDraftOrderCancelRequest(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || isRevisionNotCancel(userText) || IsCancelClarificationQuestion(userText) {
		return false
	}
	if orderCancelWordRe.MatchString(text) {
		return true
	}
	for _, p := range explicitPersistedCancelPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsSoftCancelRegret — "ga jadi" tanpa kata batal eksplisit.
func IsSoftCancelRegret(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || isRevisionNotCancel(userText) {
		return false
	}
	for _, p := range softCancelRegretPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsExplicitPersistedOrderCancel — batalkan order DB yang sudah tersimpan.
func IsExplicitPersistedOrderCancel(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || isRevisionNotCancel(userText) || IsCancelClarificationQuestion(userText) {
		return false
	}
	for _, p := range explicitPersistedCancelPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	// "batal", "cancel", "batalkan" saja → eksplisit.
	fields := strings.Fields(strings.TrimRight(text, "?!. "))
	if len(fields) <= 2 && orderCancelWordRe.MatchString(text) {
		return true
	}
	return false
}

// ShouldCancelPersistedOrder — jangan batalkan order lama jika user cuma menyesal + tanya status.
func ShouldCancelPersistedOrder(userText string) bool {
	if IsExplicitPersistedOrderCancel(userText) {
		return true
	}
	if IsSoftCancelRegret(userText) && !IsOrderStatusInquiry(userText) &&
		!strings.Contains(strings.ToLower(userText), "?") {
		return true
	}
	return false
}

// IsOrderCancelRequest — sinyal batal (draft atau persisted routing di autoreply).
func IsOrderCancelRequest(userText string) bool {
	return IsDraftOrderCancelRequest(userText) || IsSoftCancelRegret(userText)
}

// IsOrderFlowCancelled kept for callers; same as cancel request.
func IsOrderFlowCancelled(userText string) bool {
	return IsOrderCancelRequest(userText)
}

var orderStatusInquiryPhrases = []string{
	"pesanan saya", "pesanan ku", "order saya", "ada pesanan", "punya pesanan",
	"status pesanan", "nomor pesanan", "no pesanan", "cek pesanan", "lihat pesanan",
	"pesanan yang", "pesanan atas nama", "orderan saya", "pesanan mana",
}

// IsOrderStatusInquiry — customer asks about their existing order.
func IsOrderStatusInquiry(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsMinimumOrderQuestion(userText) {
		return false
	}
	for _, p := range orderStatusInquiryPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	if IsCancelClarificationQuestion(userText) {
		return true
	}
	if IsNewPurchaseIntentQuestion(userText) {
		return false
	}
	if IsOrderCancelRequest(userText) {
		return false
	}
	return (strings.Contains(text, "pesanan") || strings.Contains(text, "order")) &&
		(strings.Contains(text, "?") || strings.Contains(text, "ada") ||
			strings.Contains(text, "berapa") || strings.Contains(text, "cek") ||
			strings.Contains(text, "status") || strings.Contains(text, "nomor"))
}

func parseOrderRefFromMessage(userText string) string {
	m := orderRefInMessageRe.FindStringSubmatch(userText)
	if len(m) < 2 {
		return ""
	}
	return "WB-" + strings.ToUpper(m[1])
}

type persistedOrder struct {
	ID              string
	Status          string
	ItemsJSON       []byte
	ShippingJSON    []byte
	Subtotal        float64
	Total           float64
	CreatedAt       time.Time
}

var cancellableOrderStatuses = map[string]bool{
	"draft":      true,
	"processing": true,
	"confirmed":  true,
	"paid":       true,
}

func loadLatestOrderForConversation(ctx context.Context, q tenantQuerier, tenantSchema, convoID string) (*persistedOrder, error) {
	if strings.TrimSpace(convoID) == "" {
		return nil, nil
	}
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id::text, status, items, shipping_address, subtotal, total, created_at
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`, tenantSchema), convoID)

	var o persistedOrder
	err := row.Scan(&o.ID, &o.Status, &o.ItemsJSON, &o.ShippingJSON, &o.Subtotal, &o.Total, &o.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func loadActiveOrderForConversation(ctx context.Context, q tenantQuerier, tenantSchema, convoID string) (*persistedOrder, error) {
	if strings.TrimSpace(convoID) == "" {
		return nil, nil
	}
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id::text, status, items, shipping_address, subtotal, total, created_at
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status NOT IN ('cancelled')
		ORDER BY created_at DESC
		LIMIT 1`, tenantSchema), convoID)

	var o persistedOrder
	err := row.Scan(&o.ID, &o.Status, &o.ItemsJSON, &o.ShippingJSON, &o.Subtotal, &o.Total, &o.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func countCancellableOrdersForConversation(ctx context.Context, q tenantQuerier, tenantSchema, convoID string) (int, error) {
	if strings.TrimSpace(convoID) == "" {
		return 0, nil
	}
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status IN ('draft','processing','confirmed','paid')`, tenantSchema), convoID)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func loadOrderByRefForConversation(ctx context.Context, q tenantQuerier, tenantSchema, convoID, ref string) (*persistedOrder, error) {
	prefix := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(ref), "WB-"))
	if prefix == "" || strings.TrimSpace(convoID) == "" {
		return nil, nil
	}
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id::text, status, items, shipping_address, subtotal, total, created_at
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND UPPER(REPLACE(id::text, '-', '')) LIKE $2 || '%%'
		ORDER BY created_at DESC
		LIMIT 1`, tenantSchema), convoID, prefix)

	var o persistedOrder
	err := row.Scan(&o.ID, &o.Status, &o.ItemsJSON, &o.ShippingJSON, &o.Subtotal, &o.Total, &o.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func resolvePersistedOrderAction(ctx context.Context, q tenantQuerier, tenantSchema, convoID, userText string) (*persistedOrder, bool, error) {
	if ref := parseOrderRefFromMessage(userText); ref != "" {
		o, err := loadOrderByRefForConversation(ctx, q, tenantSchema, convoID, ref)
		return o, false, err
	}
	n, err := countCancellableOrdersForConversation(ctx, q, tenantSchema, convoID)
	if err != nil {
		return nil, false, err
	}
	if n > 1 {
		return nil, true, nil
	}
	o, err := loadActiveOrderForConversation(ctx, q, tenantSchema, convoID)
	if err != nil {
		return nil, false, err
	}
	if o != nil {
		return o, false, nil
	}
	o, err = loadLatestOrderForConversation(ctx, q, tenantSchema, convoID)
	return o, false, err
}

func cancelPersistedOrder(ctx context.Context, q tenantQuerier, tenantSchema, orderID string) error {
	res, err := q.ExecContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order"
		SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL
		  AND status IN ('draft','processing','confirmed','paid')`, tenantSchema), orderID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if err := finance.RemoveOrderIncomeTransaction(ctx, tenantSchema, orderID); err != nil {
		rlog.Warn("order cancel: finance cleanup", "orderId", orderID, "err", err)
	}
	return nil
}

func formatPersistedOrderSummary(o *persistedOrder) string {
	if o == nil {
		return ""
	}
	ref := FormatOrderNumber(o.ID)
	var items []order.OrderItem
	_ = json.Unmarshal(o.ItemsJSON, &items)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Nomor pesanan: %s\n", ref))
	b.WriteString(fmt.Sprintf("Status: %s\n\n", orderStatusLabelID(o.Status)))

	if len(items) > 0 {
		b.WriteString("Produk:\n")
		for _, it := range items {
			qty := it.Qty
			if qty < 1 {
				qty = 1
			}
			line := fmt.Sprintf("• %s × %d", strings.TrimSpace(it.Name), qty)
			if it.UnitPrice > 0 {
				line += fmt.Sprintf(" (%s)", formatMoney(it.UnitPrice*float64(qty)))
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	var ship order.ShippingAddress
	if len(o.ShippingJSON) > 0 {
		_ = json.Unmarshal(o.ShippingJSON, &ship)
	}
	if strings.TrimSpace(ship.Name) != "" {
		b.WriteString(fmt.Sprintf("Penerima: %s\n", strings.TrimSpace(ship.Name)))
	}
	if strings.TrimSpace(ship.City) != "" {
		b.WriteString(fmt.Sprintf("Kota: %s\n", strings.TrimSpace(ship.City)))
	}
	if o.Total > 0 {
		b.WriteString(fmt.Sprintf("\nTotal: %s", formatMoney(o.Total)))
	}
	return strings.TrimSpace(b.String())
}

func orderStatusLabelID(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft":
		return "menunggu konfirmasi toko"
	case "processing":
		return "diproses"
	case "confirmed", "paid":
		return "dikonfirmasi"
	case "shipped":
		return "dikirim"
	case "completed":
		return "selesai"
	case "cancelled":
		return "dibatalkan"
	default:
		return status
	}
}

func orderCancelCustomerReply(tone string, ref string) string {
	base := fmt.Sprintf("Baik kak, pesanan %s sudah kami batalkan.", ref)
	if tone == "formal" {
		return base + " Silakan tanya produk, harga, atau stok jika ada yang dibutuhkan."
	}
	return base + " Mau tanya produk atau harga, langsung chat aja 😊"
}

func orderAlreadyCancelledReply(ref string) string {
	return fmt.Sprintf("Pesanan %s sudah dalam status dibatalkan ya kak.", ref)
}

func orderNoneToCancelReply() string {
	return "Saat ini tidak ada pesanan aktif yang bisa dibatalkan dari chat ini. Kalau mau order baru, sebut produk dari katalog ya kak."
}

func orderNoneFoundReply() string {
	return "Belum ada pesanan tercatat dari chat ini. Mau lihat katalog atau mulai order baru?"
}

func orderMultiDisambiguationReply() string {
	return "Kak, dari chat ini ada lebih dari satu pesanan aktif. Sebut nomor pesanan (contoh WB-A1B2C3D4) atau produknya ya, biar kami bantu batalkan/cek yang benar."
}
