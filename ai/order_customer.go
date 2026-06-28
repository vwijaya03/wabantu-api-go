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

// FormatOrderNumber — nomor pesanan singkat untuk pembeli (delegasi ke order package).
func FormatOrderNumber(orderID string) string {
	return order.FormatOrderNumber(orderID)
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

// Inline Nama:/HP: pada satu baris chat (mis. "... ada? Nama: supriyanto").
var recipientNameInlineRe = regexp.MustCompile(`(?i)\bnama\s*:\s*([^?\n,;]+)`)
var recipientPhoneInlineRe = regexp.MustCompile(`(?i)\b(?:hp|no\.?\s*hp|telp)\s*:\s*(\+?[\d\s-]{8,})`)

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

// IsExplicitNewOrderStart — "mau buat pesanan baru", bukan cek status pesanan lama.
func IsExplicitNewOrderStart(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	for _, p := range []string{
		"pesanan baru", "buat pesanan", "order baru", "pesan baru", "buat order",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// IsNewPurchaseIntentQuestion — "mau order X bisa?" bukan tanya status pesanan lama.
func IsNewPurchaseIntentQuestion(userText string) bool {
	if IsPaymentStatusInquiry(userText) {
		return false
	}
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
	if IsConsultingPurchaseQuestion(userText, nil) {
		return true
	}
	wants := strings.Contains(text, "mau") || hasOrderIntentText(userText)
	if !wants {
		return false
	}
	if IsExplicitNewOrderStart(userText) ||
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
	"punya order", "ada order",
	"status pesanan", "nomor pesanan", "no pesanan", "cek pesanan", "lihat pesanan",
	"pesanan yang", "pesanan atas nama", "orderan saya", "pesanan mana",
	"apakah saya punya", "saya punya pesanan", "masih punya pesanan",
	"pembeli atas nama saya", "pembeli saya", "pembeli atas nama ini",
}

var selfBuyerLookupPhrases = []string{
	"pembeli atas nama saya", "pembeli saya ada", "pembeli atas nama ini",
	"pembeli saya", "atas nama saya ada", "pembeli ini ada",
}

var thirdPartyBuyerLookupPhrases = []string{
	"pembeli dengan nama", "customer dengan nama", "data pembeli",
	"pelanggan dengan nama", "pembeli atas nama ", "customer atas nama",
}

var orderHistoryContextPhrases = []string{
	"pesanan tadi", "order tadi", "yang barusan", "yang tadi", "nomor tadi",
	"pesanan yang barusan", "yang kamu kirim", "pesanan yang dikirim",
}

// WantsActiveOrderOnly — tanya pesanan aktif/pending, bukan riwayat dibatalkan.
func WantsActiveOrderOnly(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, s := range []string{
		"pending", "aktif", "masih punya", "masih ada", "belum selesai",
		"belum dibatalkan", "masih order", "masih pesanan",
	} {
		if strings.Contains(text, s) {
			return true
		}
	}
	if (strings.Contains(text, "punya pesanan") || strings.Contains(text, "ada pesanan") ||
		strings.Contains(text, "punya order") || strings.Contains(text, "ada order")) &&
		(strings.Contains(text, "nggak") || strings.Contains(text, " gak") ||
			strings.Contains(text, " ga") || strings.Contains(text, "tidak") ||
			strings.Contains(text, "?")) {
		return true
	}
	return false
}

// IsPaymentStatusInquiry — buyer asks whether payment/proof was received or which order is paid.
func IsPaymentStatusInquiry(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, p := range []string{
		"sudah bayar", "sudah dibayar", "sudah transfer", "sudah tf",
		"bukti bayar", "bukti transfer", "bukti pembayaran",
		"status bayar", "status pembayaran", "pembayaran sudah",
		"yang sudah bayar", "sudah lunas", "sudah kirim bukti",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	if parseOrderRefFromMessage(userText) != "" &&
		(strings.Contains(text, "bayar") || strings.Contains(text, "transfer") ||
			strings.Contains(text, "bukti") || strings.Contains(text, "tf")) {
		return true
	}
	return false
}

// IsOrderStatusInquiry — customer asks about their existing order.
func IsOrderStatusInquiry(userText string) bool {
	if IsPaymentStatusInquiry(userText) {
		return true
	}
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

// parseRecipientHintFromMessage extracts Nama:/HP: blocks from buyer chat text.
func parseRecipientHintFromMessage(userText string) (name, phone string) {
	if m := recipientNameInlineRe.FindStringSubmatch(userText); len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}
	if m := recipientPhoneInlineRe.FindStringSubmatch(userText); len(m) > 1 {
		phone = normalizePhoneID(m[1])
	}
	if name != "" || phone != "" {
		return name, phone
	}
	return parseRecipientLine(userText)
}

func hasRecipientHintInMessage(userText string) bool {
	name, phone := parseRecipientHintFromMessage(userText)
	text := strings.ToLower(strings.TrimSpace(userText))
	if phone != "" {
		return true
	}
	if name != "" && (strings.Contains(text, "nama:") || strings.Contains(text, "nama :") ||
		strings.Contains(text, "pembeli atas nama")) {
		return true
	}
	return false
}

func isSelfBuyerReference(text string) bool {
	for _, p := range selfBuyerLookupPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	if strings.Contains(text, "pembeli") && strings.Contains(text, "nama saya") {
		return true
	}
	return false
}

// IsSelfBuyerOrderLookup — cek pesanan milik chat ini (termasuk frasa "pembeli atas nama saya/ini").
func IsSelfBuyerOrderLookup(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || IsNewPurchaseIntentQuestion(userText) || IsOrderCancelRequest(userText) {
		return false
	}
	if parseOrderRefFromMessage(userText) != "" {
		return false
	}
	if isSelfBuyerReference(text) {
		return true
	}
	if hasRecipientHintInMessage(userText) && strings.Contains(text, "pembeli") {
		return true
	}
	if strings.Contains(text, "pembeli") &&
		(strings.Contains(text, "saya") || strings.Contains(text, "aku") || strings.Contains(text, " ini")) &&
		(strings.Contains(text, "?") || strings.Contains(text, "ada")) {
		return true
	}
	return false
}

// IsThirdPartyBuyerLookup — cari data pembeli/pesanan orang lain (bukan scoped ke chat ini).
func IsThirdPartyBuyerLookup(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if parseOrderRefFromMessage(userText) != "" || IsOrderStatusInquiry(userText) || IsSelfBuyerOrderLookup(userText) {
		return false
	}
	if isSelfBuyerReference(text) || hasRecipientHintInMessage(userText) {
		return false
	}
	for _, p := range thirdPartyBuyerLookupPhrases {
		if !strings.Contains(text, p) {
			continue
		}
		if p == "pembeli atas nama " || p == "customer atas nama" {
			rest := text[strings.Index(text, p)+len(p):]
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "saya") || strings.HasPrefix(rest, "aku") ||
				strings.HasPrefix(rest, "ini") || strings.HasPrefix(rest, "saya ") {
				continue
			}
		}
		return true
	}
	if strings.Contains(text, "pembeli") && strings.Contains(text, "dengan nama") {
		return true
	}
	return false
}

func wantsOrderContextFromHistory(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	for _, p := range orderHistoryContextPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func parseOrderRefFromHistory(history []dbMessage) string {
	if len(history) == 0 {
		return ""
	}
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < 8; i-- {
		if history[i].Direction != "out" && history[i].Direction != "system" {
			continue
		}
		seen++
		if ref := parseOrderRefFromMessage(history[i].Body); ref != "" {
			return ref
		}
	}
	return ""
}

func thirdPartyBuyerLookupDeniedReply() string {
	return "Maaf kak, saya hanya bisa bantu pesanan dari nomor WhatsApp ini. Untuk cek data pembeli lain, silakan hubungi tim toko ya."
}

func orderRecipientHintNotFoundReply() string {
	return "Tidak ada pesanan atas nama itu di chat ini ya kak. Sebut nomor pesanan (contoh WB-A1B2C3D4) atau tanya pesanan aktif Anda."
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
	ContactID       string
	ConversationID  string
	Status          string
	PaymentStatus   string
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
	b.WriteString(fmt.Sprintf("Pembayaran: %s\n\n", paymentStatusLabelID(o.PaymentStatus)))

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
