package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/order"
)

func loadLatestDraftOrderForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) (*persistedOrder, error) {
	drafts, err := loadDraftOrdersForContact(ctx, q, tenantSchema, scope)
	if err != nil || len(drafts) == 0 {
		return nil, err
	}
	o := drafts[0]
	return &o, nil
}

func loadDraftOrdersForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) ([]persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status = 'draft'%s
		ORDER BY created_at DESC`, persistedOrderSelectCols, tenantSchema, owner), scope.ConversationID, scope.ContactID)
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

func loadDraftOrderByIDForContact(ctx context.Context, q tenantQuerier, tenantSchema, orderID string, scope orderAccessScope) (*persistedOrder, error) {
	if !scope.valid() || strings.TrimSpace(orderID) == "" {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(2, 3)
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE id = $1::uuid AND conversation_id = $2::uuid AND deleted_at IS NULL
		  AND status = 'draft'%s
		LIMIT 1`, persistedOrderSelectCols, tenantSchema, owner), orderID, scope.ConversationID, scope.ContactID)

	o, err := scanPersistedOrderRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func resolveDraftForAmend(
	ctx context.Context,
	q tenantQuerier,
	tenantSchema string,
	scope orderAccessScope,
	userText string,
) (draft *persistedOrder, needPick bool, list []persistedOrder, blockedStatus string, blockedRef string, err error) {
	if ref := parseOrderRefFromMessage(userText); ref != "" {
		o, denied, lerr := loadOrderByRefForContact(ctx, q, tenantSchema, scope, ref)
		if lerr != nil {
			return nil, false, nil, "", "", lerr
		}
		if denied {
			return nil, false, nil, "access_denied", "", nil
		}
		if o == nil {
			return nil, false, nil, "", "", nil
		}
		formattedRef := FormatOrderNumber(o.ID)
		if isOrderAmendBlockedStatus(o.Status) {
			return nil, false, nil, o.Status, formattedRef, nil
		}
		if !isOrderDraftAmendable(o.Status) {
			return nil, false, nil, "non_draft", formattedRef, nil
		}
		return o, false, nil, "", "", nil
	}

	drafts, err := loadDraftOrdersForContact(ctx, q, tenantSchema, scope)
	if err != nil {
		return nil, false, nil, "", "", err
	}
	switch len(drafts) {
	case 0:
		return nil, false, nil, "", "", nil
	case 1:
		return &drafts[0], false, nil, "", "", nil
	default:
		return nil, true, drafts, "", "", nil
	}
}

func orderItemsFromJSON(raw []byte) ([]order.OrderItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []order.OrderItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func orderLinesToItems(lines []bf.OrderLineState) []order.OrderItem {
	items := make([]order.OrderItem, 0, len(lines))
	for _, ln := range lines {
		qty := ln.Qty
		if qty < 1 {
			qty = 1
		}
		variant := bf.BuildVariantLabel(ln.Size, ln.Color)
		items = append(items, order.OrderItem{
			CatalogItemID: ln.CatalogItemID,
			ExternalCode:  ln.ExternalCode,
			Name:          ln.ProductName,
			Variant:       variant,
			Size:          ln.Size,
			Color:         ln.Color,
			Qty:           float64(qty),
			UnitPrice:     ln.UnitPrice,
			SellUnit:      ln.SellUnit,
			WarehouseID:   strings.TrimSpace(ln.WarehouseID),
		})
	}
	return items
}

func orderItemsToLines(items []order.OrderItem) []bf.OrderLineState {
	lines := make([]bf.OrderLineState, 0, len(items))
	for _, it := range items {
		qty := int(it.Qty)
		if qty < 1 {
			qty = 1
		}
		lines = append(lines, bf.OrderLineState{
			CatalogItemID: it.CatalogItemID,
			ExternalCode:  it.ExternalCode,
			ProductName:   it.Name,
			Size:          it.Size,
			Color:         it.Color,
			Qty:           qty,
			UnitPrice:     it.UnitPrice,
			SellUnit:      it.SellUnit,
			WarehouseID:   it.WarehouseID,
		})
	}
	return lines
}

func mergeOrderItemLines(existing []order.OrderItem, added []bf.OrderLineState) []order.OrderItem {
	merged := bf.MergeOrderLines(orderItemsToLines(existing), added)
	return orderLinesToItems(merged)
}

func updateDraftOrderItems(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema string,
	orderID string,
	scope orderAccessScope,
	items []order.OrderItem,
) error {
	var subtotal float64
	for _, it := range items {
		subtotal += it.Qty * it.UnitPrice
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return err
	}
	owner := sqlOrderOwnerFilter(4, 5)
	q := fmt.Sprintf(`
		UPDATE "%s"."order"
		SET items = $2, subtotal = $3, total = $3, updated_at = NOW()
		WHERE id = $1::uuid AND status = 'draft' AND deleted_at IS NULL
		  AND conversation_id = $4::uuid%s`, tenantSchema, owner)
	res, err := tq.ExecContext(ctx, q, orderID, itemsJSON, subtotal, scope.ConversationID, scope.ContactID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("draft order not found or not amendable")
	}
	syncPersistedOrderStock(ctx, tenantSchema, orderID, "draft", items)
	return nil
}

func updateDraftShippingAddress(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema, orderID string,
	scope orderAccessScope,
	addrJSON []byte,
) error {
	if !shippingAddressJSONIsComplete(addrJSON) {
		return nil
	}
	owner := sqlOrderOwnerFilter(3, 4)
	q := fmt.Sprintf(`
		UPDATE "%s"."order"
		SET shipping_address = $2, updated_at = NOW()
		WHERE id = $1::uuid AND status = 'draft' AND deleted_at IS NULL
		  AND conversation_id = $3::uuid%s`, tenantSchema, owner)
	_, err := tq.ExecContext(ctx, q, orderID, addrJSON, scope.ConversationID, scope.ContactID)
	return err
}

// shippingAddressJSONIsComplete — alamat nyata (nama+HP atau jalan+kota). Placeholder {Country:Indonesia} tidak.
func shippingAddressJSONIsComplete(addrJSON []byte) bool {
	if len(addrJSON) == 0 {
		return false
	}
	var addr order.ShippingAddress
	if err := json.Unmarshal(addrJSON, &addr); err != nil {
		return false
	}
	return shippingAddressIsComplete(addr)
}

func shippingAddressIsComplete(addr order.ShippingAddress) bool {
	hasIdentity := strings.TrimSpace(addr.Name) != "" && strings.TrimSpace(addr.Phone) != ""
	hasStreet := strings.TrimSpace(addr.Street) != ""
	hasCity := strings.TrimSpace(addr.City) != "" || strings.TrimSpace(addr.PostalCode) != ""
	return hasIdentity || (hasStreet && hasCity)
}

func applyShippingJSONToOrderState(st *orderState, raw []byte) {
	if st == nil || len(raw) == 0 {
		return
	}
	var addr order.ShippingAddress
	if err := json.Unmarshal(raw, &addr); err != nil {
		return
	}
	if strings.TrimSpace(addr.Name) != "" {
		st.RecipientName = addr.Name
	}
	if strings.TrimSpace(addr.Phone) != "" {
		st.RecipientPhone = addr.Phone
	}
	if strings.TrimSpace(addr.Street) != "" {
		st.Street = addr.Street
	}
	if strings.TrimSpace(addr.RT) != "" {
		st.RT = addr.RT
	}
	if strings.TrimSpace(addr.RW) != "" {
		st.RW = addr.RW
	}
	if strings.TrimSpace(addr.Kelurahan) != "" {
		st.Kelurahan = addr.Kelurahan
	}
	if strings.TrimSpace(addr.Kecamatan) != "" {
		st.Kecamatan = addr.Kecamatan
	}
	if strings.TrimSpace(addr.City) != "" {
		st.City = addr.City
	}
	if strings.TrimSpace(addr.Province) != "" {
		st.Province = addr.Province
	}
	if strings.TrimSpace(addr.PostalCode) != "" {
		st.PostalCode = addr.PostalCode
	}
	if strings.TrimSpace(addr.Country) != "" {
		st.Country = addr.Country
	}
}

// orderStateFromPersistedDraft — hydrate Redis cart dari items + shipping draft DB.
func orderStateFromPersistedDraft(o *persistedOrder) orderState {
	st := orderState{Step: "ask_recipient"}
	if o == nil {
		return st
	}
	st.PersistedOrderID = o.ID
	items, _ := orderItemsFromJSON(o.ItemsJSON)
	lines := orderItemsToLines(items)
	if len(lines) > 0 {
		st.Items = lines
		bf.ApplyLineToOrderState(&st, lines[0])
		if strings.TrimSpace(st.Product) == "" {
			st.Product = lines[0].ProductName
		}
	}
	applyShippingJSONToOrderState(&st, o.ShippingJSON)
	return st
}

type draftWriteKind int

const (
	draftWriteInsert draftWriteKind = iota
	draftWriteUpdatePinned
	draftWriteNeedPick
)

func decideDraftWrite(forceInsert, pinFound bool, leftoverDraftCount int) draftWriteKind {
	if forceInsert {
		return draftWriteInsert
	}
	if pinFound {
		return draftWriteUpdatePinned
	}
	if leftoverDraftCount > 0 {
		return draftWriteNeedPick
	}
	return draftWriteInsert
}

func upsertDraftOrderItems(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema, convoID, contactID, preferredOrderID string,
	items []order.OrderItem,
	addrJSON []byte,
	forceInsert bool,
) (orderID string, updated bool, needPick bool, pickList []persistedOrder, err error) {
	scope := orderAccessScope{ConversationID: convoID, ContactID: contactID}

	if forceInsert {
		return "", false, false, nil, nil
	}

	var pinFound bool
	if strings.TrimSpace(preferredOrderID) != "" {
		draft, derr := loadDraftOrderByIDForContact(ctx, tq, tenantSchema, preferredOrderID, scope)
		if derr != nil {
			return "", false, false, nil, derr
		}
		if draft != nil {
			pinFound = true
			if err := updateDraftOrderItems(ctx, tq, tenantSchema, draft.ID, scope, items); err != nil {
				return "", false, false, nil, err
			}
			if err := updateDraftShippingAddress(ctx, tq, tenantSchema, draft.ID, scope, addrJSON); err != nil {
				rlog.Warn("AI order: draft shipping update skipped", "err", err, "orderId", draft.ID)
			}
			return draft.ID, true, false, nil, nil
		}
	}

	drafts, err := loadDraftOrdersForContact(ctx, tq, tenantSchema, scope)
	if err != nil {
		return "", false, false, nil, err
	}
	switch decideDraftWrite(false, pinFound, len(drafts)) {
	case draftWriteNeedPick:
		return "", false, true, drafts, nil
	default:
		return "", false, false, nil, nil
	}
}

func (s *AutoReplyService) amendIdempotent(ctx context.Context, tenantID, inboundID string) bool {
	if strings.TrimSpace(inboundID) == "" {
		return false
	}
	key := amendIdempotencyKey + tenantID + ":" + inboundID
	ok, err := s.rdb.SetNX(ctx, key, "1", amendIdempotencyTTL).Result()
	if err != nil {
		rlog.Warn("order amend idempotency check failed", "err", err)
		return false
	}
	return !ok
}

func orderAmendNoDraftReply(formal bool) string {
	if formal {
		return "Mohon maaf kak, belum ada pesanan draft yang bisa diperbarui. Sebut produk dan jumlahnya ya, nanti kami bantu buat pesanan baru."
	}
	return "Maaf kak, belum ada pesanan draft yang bisa diperbarui. Sebut produk + jumlahnya ya, nanti aku bantu buat pesanan baru 🙏"
}

func orderAmendNonDraftReply(formal bool) string {
	if formal {
		return "Pesanan tersebut sudah diproses dan tidak bisa diubah lewat chat. Tim CS akan bantu jika perlu penyesuaian ya."
	}
	return "Pesanan itu sudah diproses kak, jadi belum bisa diubah lewat chat. Kalau perlu penyesuaian, tim CS akan bantu ya 🙏"
}

func (s *AutoReplyService) handleOrderAmend(
	ctx context.Context,
	ts tenantScopedQuerier,
	payload AiReplyJobPayload,
	convo *dbConversation,
	channel *dbChannel,
	contact *dbContact,
	userText string,
	profile *dbBusinessProfile,
	history []dbMessage,
) (bool, error) {
	if s.amendIdempotent(ctx, payload.TenantID, payload.InboundMessageID) {
		rlog.Info("AI order amend: duplicate inbound skipped", "inboundId", payload.InboundMessageID)
		return true, nil
	}

	scope := orderAccessScope{ConversationID: convo.ID, ContactID: contact.ID}
	formal := strOrEmpty(profile.Tone) == "formal"
	send := func(text string) (bool, error) {
		out := metaNoLLM(reasonAIGenerated, PathOrderFlow)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, applyOutputPolicy(text), "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	draft, needPick, pickList, blockedStatus, blockedRef, err := resolveDraftForAmend(ctx, ts, payload.TenantSchema, scope, userText)
	if err != nil {
		return false, err
	}
	if blockedStatus != "" {
		if blockedStatus == "access_denied" {
			return send(orderAccessDeniedReply())
		}
		if blockedStatus == "non_draft" {
			return send(orderAmendNonDraftReply(formal))
		}
		return send(orderAmendBlockedStatusReply(formal, blockedStatus, blockedRef))
	}
	if needPick {
		return send(orderAmendPickDraftReply(pickList))
	}
	if draft == nil {
		latest, _ := loadLatestOrderForContact(ctx, ts, payload.TenantSchema, scope)
		if latest != nil && !isOrderDraftAmendable(latest.Status) {
			if isOrderAmendBlockedStatus(latest.Status) {
				return send(orderAmendBlockedStatusReply(formal, latest.Status, FormatOrderNumber(latest.ID)))
			}
			return send(orderAmendNonDraftReply(formal))
		}
		return send(orderAmendNoDraftReply(formal))
	}
	if !OrderAccessibleByContact(draft, contact.ID, convo.ID) {
		return send(orderAccessDeniedReply())
	}

	catalog, catErr := loadActiveCatalog(ctx, ts, 40)
	if catErr != nil {
		rlog.Warn("AI order amend: catalog load failed", "err", catErr)
	}
	vctx, catalog := s.fetchCatalogVectorContext(ctx, payload.TenantID, payload.TenantSchema, userText, ts, catalog)
	existing, err := orderItemsFromJSON(draft.ItemsJSON)
	if err != nil {
		return false, err
	}
	existingIDs := make(map[string]bool)
	for _, it := range existing {
		if it.CatalogItemID != "" {
			existingIDs[it.CatalogItemID] = true
		}
	}

	bfHistory := make([]bf.Message, len(history))
	for i, m := range history {
		dir := m.Author
		if dir == "" {
			dir = "contact"
		}
		bfHistory[i] = bf.Message{Author: m.Author, Body: m.Body, Direction: dir}
	}

	var added []bf.OrderLineState
	if lines := bf.ExtractAmendLinesFromText(userText, toBFCatalogSlice(catalog), vctx); len(lines) > 0 {
		for _, ln := range lines {
			if !existingIDs[ln.CatalogItemID] {
				added = append(added, ln)
			}
		}
	}
	if len(added) == 0 {
		histLines := bf.ExtractAmendLinesFromHistory(bfHistory, toBFCatalogSlice(catalog), existingIDs, vctx)
		added = histLines
	}
	if len(added) == 0 {
		if formal {
			return send("Mohon maaf kak, produk tambahan belum terdeteksi. Sebut nama produk + jumlah yang ingin ditambahkan ya.")
		}
		return send("Maaf kak, produk tambahannya belum kebaca. Sebut nama produk + jumlah yang mau ditambah ya 🙏")
	}

	merged := mergeOrderItemLines(existing, added)
	if err := updateDraftOrderItems(ctx, ts, payload.TenantSchema, draft.ID, scope, merged); err != nil {
		rlog.Warn("AI order amend: update failed", "err", err, "orderId", draft.ID)
		return send("Maaf kak, gagal memperbarui pesanan. Coba sebut produknya lagi ya 🙏")
	}

	st := orderStateFromPersistedDraft(draft)
	st.Items = orderItemsToLines(merged)
	if len(st.Items) > 0 {
		bf.ApplyLineToOrderState(&st, st.Items[0])
	}
	s.setOrderState(ctx, payload.TenantID, convo.ID, st)
	summary := formatOrderSummary(st)
	ref := FormatOrderNumber(draft.ID)
	reply := fmt.Sprintf("Siap kak, pesanan %s sudah diperbarui:\n\n%s", ref, summary)
	return send(reply)
}

func toBFCatalogSlice(catalog []dbCatalogItem) []bf.CatalogItem {
	out := make([]bf.CatalogItem, len(catalog))
	for i, c := range catalog {
		out[i] = bf.CatalogItem{
			ID: c.ID, ExternalCode: c.ExternalCode, Name: c.Name,
			SellPrice: c.SellPrice, SellUnit: c.SellUnit,
		}
	}
	return out
}
