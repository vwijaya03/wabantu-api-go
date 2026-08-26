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

func orderShortLabel(o persistedOrder) string {
	ref := FormatOrderNumber(o.ID)
	var items []order.OrderItem
	_ = json.Unmarshal(o.ItemsJSON, &items)
	name := "Pesanan"
	if len(items) > 0 && strings.TrimSpace(items[0].Name) != "" {
		name = strings.TrimSpace(items[0].Name)
		if len(name) > 40 {
			name = name[:40] + "…"
		}
	}
	return fmt.Sprintf("%s — %s (%s · %s)", ref, name, orderStatusLabelID(o.Status), paymentStatusLabelID(o.PaymentStatus))
}

func formatOrderPickListReply(intro string, orders []persistedOrder, actionHint string) string {
	var b strings.Builder
	b.WriteString(intro)
	b.WriteString("\n\n")
	for i, o := range orders {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, orderShortLabel(o)))
	}
	b.WriteString("\n")
	b.WriteString(actionHint)
	return strings.TrimSpace(b.String())
}

func orderNoActiveOrdersReply() string {
	return "Saat ini tidak ada pesanan aktif dari chat ini ya kak. Kalau mau order baru, sebut produk dari katalog."
}

func orderRefNotFoundReply(ref string) string {
	if ref == "" {
		return "Nomor pesanan tidak ditemukan di chat ini. Pastikan WB-XXXX dari pesanan kamu di percakapan ini ya kak."
	}
	return fmt.Sprintf("Nomor pesanan %s tidak ditemukan di chat ini. Cek lagi nomornya ya kak.", ref)
}

func orderCancelPickRefReply(orders []persistedOrder) string {
	return formatOrderPickListReply(
		"Untuk batalkan, pilih nomor pesanan yang benar ya kak:",
		orders,
		"Ketik: batalkan WB-XXXXXXXX (contoh batalkan WB-A1B2C3D4).",
	)
}

func orderStatusPickRefReply(orders []persistedOrder) string {
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
	if o == nil {
		return ""
	}
	ref := FormatOrderNumber(o.ID)
	var items []order.OrderItem
	_ = json.Unmarshal(o.ItemsJSON, &items)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Nomor pesanan: %s\n", ref))
	b.WriteString(fmt.Sprintf("Status pesanan: %s\n", orderStatusLabelID(o.Status)))
	b.WriteString(fmt.Sprintf("Pembayaran: %s\n", paymentStatusLabelID(o.PaymentStatus)))
	if detail := formatPaymentProofDetail(o); detail != "" {
		b.WriteString(detail + "\n")
	}
	b.WriteString("\n")

	if len(items) > 0 {
		b.WriteString("Produk:\n")
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

// formatQtyLabel renders an order qty, keeping whole numbers integer-clean ("3")
// and showing fractions when present ("1.5") — qty is float64 since PR-A8.
func formatQtyLabel(qty float64) string {
	if qty == float64(int64(qty)) {
		return fmt.Sprintf("%d", int64(qty))
	}
	return fmt.Sprintf("%g", qty)
}

func paymentStatusLabelID(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "proof_submitted":
		return "bukti transfer perlu dicek"
	case "verified":
		return "pembayaran sudah diverifikasi"
	case "rejected":
		return "bukti transfer ditolak"
	case "unpaid", "":
		return "belum ada bukti transfer"
	default:
		return status
	}
}

func formatPaymentProofDetail(o *persistedOrder) string {
	if o == nil || strings.ToLower(strings.TrimSpace(o.PaymentStatus)) != "rejected" {
		return ""
	}
	var meta paymentProofMeta
	if len(o.PaymentProofMetaJSON) > 2 {
		_ = json.Unmarshal(o.PaymentProofMetaJSON, &meta)
	}
	if order.IsPaymentProofBlocked(meta) {
		return fmt.Sprintf(
			"Batas pengiriman bukti (%d/%d penolakan) tercapai. Hubungi admin toko.",
			meta.RejectionCount, order.PaymentProofMaxRejections,
		)
	}
	if reason := strings.TrimSpace(meta.RejectReason); reason != "" {
		return "Alasan: " + reason
	}
	for _, f := range meta.Flags {
		switch f {
		case "duplicate_hash":
			return "Alasan: bukti transfer yang sama sudah dipakai untuk pesanan lain. Kirim bukti yang sesuai pesanan ini."
		case "mismatch_amount":
			return "Alasan: nominal transfer tidak sesuai total pesanan."
		case "kb_empty":
			return "Alasan: rekening tujuan belum lengkap di FAQ toko — tim akan cek manual."
		case "ocr_failed":
			return "Alasan: bukti tidak terbaca otomatis — tim akan cek manual."
		case "quota_exceeded":
			return "Alasan: verifikasi otomatis sementara tidak tersedia — tim akan cek manual."
		}
	}
	return "Silakan kirim ulang bukti transfer yang sesuai total dan rekening tujuan."
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
	return "Kak, dari chat ini ada lebih dari satu pesanan aktif. Sebut nomor pesanan (contoh WB-A1B2C3D4) ya, biar kami bantu batalkan/cek yang benar."
}
