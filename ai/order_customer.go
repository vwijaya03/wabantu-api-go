package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/finance"
	"encore.app/wabantu/order"
)

type persistedOrder struct {
	ID                   string
	ContactID            string
	ConversationID       string
	Status               string
	PaymentStatus        string
	PaymentProofMetaJSON []byte
	ItemsJSON            []byte
	ShippingJSON         []byte
	Subtotal             float64
	Total                float64
	CreatedAt            time.Time
}

var cancellableOrderStatuses = map[string]bool{
	"draft":      true,
	"processing": true,
	"confirmed":  true,
	"paid":       true,
}

func loadLatestOrderForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) (*persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL%s
		ORDER BY created_at DESC
		LIMIT 1`, persistedOrderSelectCols, tenantSchema, owner), scope.ConversationID, scope.ContactID)

	o, err := scanPersistedOrderRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func loadActiveOrderForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) (*persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status NOT IN ('cancelled')%s
		ORDER BY created_at DESC
		LIMIT 1`, persistedOrderSelectCols, tenantSchema, owner), scope.ConversationID, scope.ContactID)

	o, err := scanPersistedOrderRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func countCancellableOrdersForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) (int, error) {
	if !scope.valid() {
		return 0, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status IN ('draft','processing','confirmed','paid')%s`, tenantSchema, owner),
		scope.ConversationID, scope.ContactID)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func loadCancellableOrdersForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) ([]persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status IN ('draft','processing','confirmed','paid')%s
		ORDER BY created_at DESC`, persistedOrderSelectCols, tenantSchema, owner),
		scope.ConversationID, scope.ContactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []persistedOrder
	for rows.Next() {
		o, err := scanPersistedOrderRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func loadActiveOrdersForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) ([]persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status NOT IN ('cancelled')%s
		ORDER BY created_at DESC`, persistedOrderSelectCols, tenantSchema, owner),
		scope.ConversationID, scope.ContactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []persistedOrder
	for rows.Next() {
		o, err := scanPersistedOrderRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

type persistedOrderResolve struct {
	Order                 *persistedOrder
	NeedPick              bool
	NotFound              bool
	AccessDenied          bool
	ActiveOnly            bool
	RecipientHintNotFound bool
	List                  []persistedOrder
}

func resolveOrderRefFromUserOrHistory(userText string, history []dbMessage) string {
	if ref := parseOrderRefFromMessage(userText); ref != "" {
		return ref
	}
	if wantsOrderContextFromHistory(userText) {
		return parseOrderRefFromHistory(history)
	}
	return ""
}

func loadOrderByRecipientHintForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope, name, phone string) (*persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if name == "" && phone == "" {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL%s
		ORDER BY created_at DESC
		LIMIT 20`, persistedOrderSelectCols, tenantSchema, owner), scope.ConversationID, scope.ContactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nameLower := strings.ToLower(name)
	phoneNorm := normalizePhoneID(phone)
	for rows.Next() {
		o, err := scanPersistedOrderRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		var ship order.ShippingAddress
		if len(o.ShippingJSON) > 0 {
			_ = json.Unmarshal(o.ShippingJSON, &ship)
		}
		shipName := strings.ToLower(strings.TrimSpace(ship.Name))
		shipPhone := normalizePhoneID(strings.TrimSpace(ship.Phone))
		nameOK := nameLower == "" || shipName == "" ||
			strings.Contains(shipName, nameLower) || strings.Contains(nameLower, shipName)
		phoneOK := phoneNorm == "" || shipPhone == "" || shipPhone == phoneNorm
		if nameLower != "" && shipName != "" && nameOK {
			if phoneNorm == "" || phoneOK {
				return o, nil
			}
		}
		if phoneNorm != "" && shipPhone != "" && phoneOK && nameLower == "" {
			return o, nil
		}
	}
	return nil, rows.Err()
}

func resolvePersistedOrderStatus(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope, userText string, history []dbMessage) (persistedOrderResolve, error) {
	activeOnly := WantsActiveOrderOnly(userText)
	if ref := resolveOrderRefFromUserOrHistory(userText, history); ref != "" {
		o, denied, err := loadOrderByRefForContact(ctx, q, tenantSchema, scope, ref)
		if err != nil {
			return persistedOrderResolve{}, err
		}
		if denied {
			return persistedOrderResolve{AccessDenied: true}, nil
		}
		if o == nil {
			return persistedOrderResolve{NotFound: true}, nil
		}
		if activeOnly && strings.EqualFold(o.Status, "cancelled") {
			return persistedOrderResolve{ActiveOnly: true}, nil
		}
		return persistedOrderResolve{Order: o}, nil
	}
	if hasRecipientHintInMessage(userText) {
		name, phone := parseRecipientHintFromMessage(userText)
		o, err := loadOrderByRecipientHintForContact(ctx, q, tenantSchema, scope, name, phone)
		if err != nil {
			return persistedOrderResolve{}, err
		}
		if o == nil {
			return persistedOrderResolve{RecipientHintNotFound: true}, nil
		}
		if activeOnly && strings.EqualFold(o.Status, "cancelled") {
			return persistedOrderResolve{ActiveOnly: true}, nil
		}
		return persistedOrderResolve{Order: o}, nil
	}
	if activeOnly {
		list, err := loadActiveOrdersForContact(ctx, q, tenantSchema, scope)
		if err != nil {
			return persistedOrderResolve{}, err
		}
		if len(list) == 0 {
			return persistedOrderResolve{ActiveOnly: true}, nil
		}
		if len(list) == 1 {
			o := list[0]
			return persistedOrderResolve{Order: &o}, nil
		}
		return persistedOrderResolve{NeedPick: true, List: list}, nil
	}
	n, err := countCancellableOrdersForContact(ctx, q, tenantSchema, scope)
	if err != nil {
		return persistedOrderResolve{}, err
	}
	if n > 1 {
		list, err := loadCancellableOrdersForContact(ctx, q, tenantSchema, scope)
		if err != nil {
			return persistedOrderResolve{}, err
		}
		return persistedOrderResolve{NeedPick: true, List: list}, nil
	}
	o, err := loadActiveOrderForContact(ctx, q, tenantSchema, scope)
	if err != nil {
		return persistedOrderResolve{}, err
	}
	if o != nil {
		return persistedOrderResolve{Order: o}, nil
	}
	o, err = loadLatestOrderForContact(ctx, q, tenantSchema, scope)
	if err != nil {
		return persistedOrderResolve{}, err
	}
	if o == nil {
		return persistedOrderResolve{}, nil
	}
	return persistedOrderResolve{Order: o}, nil
}

func resolvePersistedOrderCancel(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope, userText string) (persistedOrderResolve, error) {
	if ref := parseOrderRefFromMessage(userText); ref != "" {
		o, denied, err := loadOrderByRefForContact(ctx, q, tenantSchema, scope, ref)
		if err != nil {
			return persistedOrderResolve{}, err
		}
		if denied {
			return persistedOrderResolve{AccessDenied: true}, nil
		}
		if o == nil {
			return persistedOrderResolve{NotFound: true}, nil
		}
		return persistedOrderResolve{Order: o}, nil
	}
	list, err := loadCancellableOrdersForContact(ctx, q, tenantSchema, scope)
	if err != nil {
		return persistedOrderResolve{}, err
	}
	if len(list) == 0 {
		return persistedOrderResolve{}, nil
	}
	return persistedOrderResolve{NeedPick: true, List: list}, nil
}

// loadOrderByRefForContact — ref + contact scope; denied=true jika order ada di conversation tapi milik contact lain.
func loadOrderByRefForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope, ref string) (*persistedOrder, bool, error) {
	prefix := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(ref), "WB-"))
	if prefix == "" || !scope.valid() {
		return nil, false, nil
	}
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND UPPER(REPLACE(id::text, '-', '')) LIKE $2 || '%%'
		ORDER BY created_at DESC
		LIMIT 1`, persistedOrderSelectCols, tenantSchema), scope.ConversationID, prefix)

	o, err := scanPersistedOrderRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !OrderAccessibleByContact(o, scope.ContactID, scope.ConversationID) {
		return nil, true, nil
	}
	return o, false, nil
}

func resolvePersistedOrderAction(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope, userText string) (*persistedOrder, bool, error) {
	res, err := resolvePersistedOrderStatus(ctx, q, tenantSchema, scope, userText, nil)
	if err != nil {
		return nil, false, err
	}
	if res.NeedPick || res.NotFound || res.ActiveOnly || res.AccessDenied {
		return res.Order, res.NeedPick, nil
	}
	return res.Order, false, nil
}

func orderShortLabel(o persistedOrder) string { return orderShortLabelL(o, false) }

func orderShortLabelL(o persistedOrder, en bool) string {
	ref := FormatOrderNumber(o.ID)
	var items []order.OrderItem
	_ = json.Unmarshal(o.ItemsJSON, &items)
	name := "Pesanan"
	if en {
		name = "Order"
	}
	if len(items) > 0 && strings.TrimSpace(items[0].Name) != "" {
		name = strings.TrimSpace(items[0].Name)
		if len(name) > 40 {
			name = name[:40] + "…"
		}
	}
	return fmt.Sprintf("%s — %s (%s · %s)", ref, name, orderStatusLabel(o.Status, en), paymentStatusLabel(o.PaymentStatus, en))
}

func formatOrderPickListReply(intro string, orders []persistedOrder, actionHint string) string {
	return formatOrderPickListReplyL(false, intro, orders, actionHint)
}

func formatOrderPickListReplyL(en bool, intro string, orders []persistedOrder, actionHint string) string {
	var b strings.Builder
	b.WriteString(intro)
	b.WriteString("\n\n")
	for i, o := range orders {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, orderShortLabelL(o, en)))
	}
	b.WriteString("\n")
	b.WriteString(actionHint)
	return strings.TrimSpace(b.String())
}

func orderNoActiveOrdersReply(en bool) string {
	if en {
		return "There are no active orders from this chat right now. To place a new order, name a product from the catalog."
	}
	return "Saat ini tidak ada pesanan aktif dari chat ini ya kak. Kalau mau order baru, sebut produk dari katalog."
}

func orderRefNotFoundReply(ref string) string { return orderRefNotFoundReplyL(ref, false) }

func orderRefNotFoundReplyL(ref string, en bool) string {
	if en {
		if ref == "" {
			return "Order number was not found in this chat. Please use the WB-XXXX number of an order from this conversation."
		}
		return fmt.Sprintf("Order number %s was not found in this chat. Please check the number again.", ref)
	}
	if ref == "" {
		return "Nomor pesanan tidak ditemukan di chat ini. Pastikan WB-XXXX dari pesanan kamu di percakapan ini ya kak."
	}
	return fmt.Sprintf("Nomor pesanan %s tidak ditemukan di chat ini. Cek lagi nomornya ya kak.", ref)
}

// orderRecipientHintNotFoundReplyL: no order under the recipient name the buyer gave.
func orderRecipientHintNotFoundReplyL(en bool) string {
	if en {
		return "No order under that name in this chat. Give the order number (e.g. WB-A1B2C3D4) or ask about your active orders."
	}
	return orderRecipientHintNotFoundReply()
}

func orderCancelPickRefReply(orders []persistedOrder) string {
	return formatOrderPickListReply(
		"Untuk batalkan, pilih nomor pesanan yang benar ya kak:",
		orders,
		"Ketik: batalkan WB-XXXXXXXX (contoh batalkan WB-A1B2C3D4).",
	)
}

func orderStatusPickRefReply(orders []persistedOrder, en bool) string {
	if en {
		return formatOrderPickListReplyL(true,
			"There are several orders in this chat:",
			orders,
			"Give the order number (e.g. WB-A1B2C3D4) to check a specific one.",
		)
	}
	return formatOrderPickListReply(
		"Dari chat ini ada beberapa pesanan:",
		orders,
		"Sebut nomor pesanan (contoh WB-A1B2C3D4) kalau mau cek yang spesifik.",
	)
}

func cancelPersistedOrder(ctx context.Context, q tenantQuerier, tenantSchema, orderID string, scope orderAccessScope) error {
	owner := sqlOrderOwnerFilter(2, 3)
	res, err := q.ExecContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order"
		SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL
		  AND conversation_id = $2::uuid
		  AND status IN ('draft','processing','confirmed','paid')%s`, tenantSchema, owner),
		orderID, scope.ConversationID, scope.ContactID)
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
	return formatPersistedOrderSummaryL(o, false)
}

// formatPersistedOrderSummaryL renders a saved order for the buyer in Indonesian or English.
func formatPersistedOrderSummaryL(o *persistedOrder, en bool) string {
	if o == nil {
		return ""
	}
	l := func(id, enText string) string {
		if en {
			return enText
		}
		return id
	}
	ref := FormatOrderNumber(o.ID)
	var items []order.OrderItem
	_ = json.Unmarshal(o.ItemsJSON, &items)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s: %s\n", l("Nomor pesanan", "Order number"), ref))
	b.WriteString(fmt.Sprintf("%s: %s\n", l("Status pesanan", "Order status"), orderStatusLabel(o.Status, en)))
	b.WriteString(fmt.Sprintf("%s: %s\n", l("Pembayaran", "Payment"), paymentStatusLabel(o.PaymentStatus, en)))
	if detail := formatPaymentProofDetail(o, en); detail != "" {
		b.WriteString(detail + "\n")
	}
	b.WriteString("\n")

	if len(items) > 0 {
		b.WriteString(l("Produk", "Items") + ":\n")
		for _, it := range items {
			qty := it.Qty
			if qty < 1 {
				qty = 1
			}
			line := fmt.Sprintf("• %s × %s", strings.TrimSpace(it.Name), formatQtyLabel(qty))
			if it.UnitPrice > 0 {
				line += fmt.Sprintf(" (%s)", formatMoney(it.UnitPrice*qty))
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
		b.WriteString(fmt.Sprintf("%s: %s\n", l("Penerima", "Recipient"), strings.TrimSpace(ship.Name)))
	}
	if strings.TrimSpace(ship.City) != "" {
		b.WriteString(fmt.Sprintf("%s: %s\n", l("Kota", "City"), strings.TrimSpace(ship.City)))
	}
	if o.Total > 0 {
		b.WriteString(fmt.Sprintf("\nTotal: %s", formatMoney(o.Total)))
	}
	return strings.TrimSpace(b.String())
}

// formatQtyLabel renders an order qty, keeping whole numbers integer-clean ("3")
// and showing fractions when present ("1.5") — qty is float64 since PR-A8.
func formatQtyLabel(qty float64) string {
	if qty == float64(int64(qty)) {
		return fmt.Sprintf("%d", int64(qty))
	}
	return fmt.Sprintf("%g", qty)
}

func paymentStatusLabelID(status string) string { return paymentStatusLabel(status, false) }

func paymentStatusLabel(status string, en bool) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "proof_submitted":
		if en {
			return "transfer proof needs checking"
		}
		return "bukti transfer perlu dicek"
	case "verified":
		if en {
			return "payment verified"
		}
		return "pembayaran sudah diverifikasi"
	case "rejected":
		if en {
			return "transfer proof rejected"
		}
		return "bukti transfer ditolak"
	case "unpaid", "":
		if en {
			return "no transfer proof yet"
		}
		return "belum ada bukti transfer"
	default:
		return status
	}
}

func formatPaymentProofDetail(o *persistedOrder, en bool) string {
	if o == nil || strings.ToLower(strings.TrimSpace(o.PaymentStatus)) != "rejected" {
		return ""
	}
	l := func(id, enText string) string {
		if en {
			return enText
		}
		return id
	}
	var meta paymentProofMeta
	if len(o.PaymentProofMetaJSON) > 2 {
		_ = json.Unmarshal(o.PaymentProofMetaJSON, &meta)
	}
	if order.IsPaymentProofBlocked(meta) {
		return fmt.Sprintf(
			l("Batas pengiriman bukti (%d/%d penolakan) tercapai. Hubungi admin toko.",
				"The proof submission limit (%d/%d rejections) has been reached. Please contact the shop admin."),
			meta.RejectionCount, order.PaymentProofMaxRejections,
		)
	}
	if reason := strings.TrimSpace(meta.RejectReason); reason != "" {
		return l("Alasan: ", "Reason: ") + reason
	}
	for _, f := range meta.Flags {
		switch f {
		case "duplicate_hash":
			return l("Alasan: bukti transfer yang sama sudah dipakai untuk pesanan lain. Kirim bukti yang sesuai pesanan ini.",
				"Reason: this transfer proof was already used for another order. Please send the proof for this order.")
		case "mismatch_amount":
			return l("Alasan: nominal transfer tidak sesuai total pesanan.",
				"Reason: the transfer amount does not match the order total.")
		case "kb_empty":
			return l("Alasan: rekening tujuan belum lengkap di FAQ toko — tim akan cek manual.",
				"Reason: the destination account is not complete in the shop FAQ — the team will check manually.")
		case "ocr_failed":
			return l("Alasan: bukti tidak terbaca otomatis — tim akan cek manual.",
				"Reason: the proof could not be read automatically — the team will check manually.")
		case "quota_exceeded":
			return l("Alasan: verifikasi otomatis sementara tidak tersedia — tim akan cek manual.",
				"Reason: automatic verification is temporarily unavailable — the team will check manually.")
		}
	}
	return l("Silakan kirim ulang bukti transfer yang sesuai total dan rekening tujuan.",
		"Please resend a transfer proof that matches the total and the destination account.")
}

func orderStatusLabelID(status string) string { return orderStatusLabel(status, false) }

func orderStatusLabel(status string, en bool) string {
	l := func(id, enText string) string {
		if en {
			return enText
		}
		return id
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft":
		return l("menunggu konfirmasi toko", "awaiting shop confirmation")
	case "processing":
		return l("diproses", "being processed")
	case "confirmed", "paid":
		return l("dikonfirmasi", "confirmed")
	case "shipped":
		return l("dikirim", "shipped")
	case "completed":
		return l("selesai", "completed")
	case "cancelled":
		return l("dibatalkan", "cancelled")
	default:
		return status
	}
}

func orderCancelCustomerReply(userText, tone, ref string) string {
	return OrderCancelCustomerReply(userText, tone, ref)
}

func orderAlreadyCancelledReply(ref string) string {
	return fmt.Sprintf("Pesanan %s sudah dalam status dibatalkan ya kak.", ref)
}

func orderNoneToCancelReply(userText string) string {
	return OrderNoneToCancelReply(userText)
}

func orderNoneFoundReply(en bool) string {
	if en {
		return "No orders have been recorded from this chat yet. Would you like to see the catalog or start a new order?"
	}
	return "Belum ada pesanan tercatat dari chat ini. Mau lihat katalog atau mulai order baru?"
}

func orderMultiDisambiguationReply() string {
	return "Kak, dari chat ini ada lebih dari satu pesanan aktif. Sebut nomor pesanan (contoh WB-A1B2C3D4) ya, biar kami bantu batalkan/cek yang benar."
}
