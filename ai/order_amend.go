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
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status = 'draft'%s
		ORDER BY created_at DESC
		LIMIT 1`, persistedOrderSelectCols, tenantSchema, owner), scope.ConversationID, scope.ContactID)

	o, err := scanPersistedOrderRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
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
	q := fmt.Sprintf(`
		UPDATE "%s"."order"
		SET items = $2, subtotal = $3, total = $3, updated_at = NOW()
		WHERE id = $1::uuid AND status = 'draft' AND deleted_at IS NULL`, tenantSchema)
	res, err := tq.ExecContext(ctx, q, orderID, itemsJSON, subtotal)
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

func upsertDraftOrderItems(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema, convoID, contactID string,
	items []order.OrderItem,
	addrJSON []byte,
) (orderID string, updated bool, err error) {
	scope := orderAccessScope{ConversationID: convoID, ContactID: contactID}
	draft, err := loadLatestDraftOrderForContact(ctx, tq, tenantSchema, scope)
	if err != nil || draft == nil {
		return "", false, err
	}
	if err := updateDraftOrderItems(ctx, tq, tenantSchema, draft.ID, items); err != nil {
		return "", false, err
	}
	if len(addrJSON) > 0 {
		q := fmt.Sprintf(`UPDATE "%s"."order" SET shipping_address = $2, updated_at = NOW() WHERE id = $1::uuid`, tenantSchema)
		_, _ = tq.ExecContext(ctx, q, draft.ID, addrJSON)
	}
	return draft.ID, true, nil
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
	draft, err := loadLatestDraftOrderForContact(ctx, ts, payload.TenantSchema, scope)
	if err != nil {
		return false, err
	}
	formal := strOrEmpty(profile.Tone) == "formal"
	send := func(text string) (bool, error) {
		out := metaNoLLM(reasonAIGenerated, PathOrderFlow)
		out.LogAndRecord(ctx, convo.ID, payload.InboundMessageID, 0, 0)
		err := s.sendAiMessage(ctx, ts, payload.TenantID, convo, channel, contact, applyOutputPolicy(text), "ai", payload.InboundMessageID, out)
		return err == nil, err
	}

	if draft == nil {
		latest, _ := loadLatestOrderForContact(ctx, ts, payload.TenantSchema, scope)
		if latest != nil && latest.Status != "draft" {
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
	if err := updateDraftOrderItems(ctx, ts, payload.TenantSchema, draft.ID, merged); err != nil {
		rlog.Warn("AI order amend: update failed", "err", err, "orderId", draft.ID)
		return send("Maaf kak, gagal memperbarui pesanan. Coba sebut produknya lagi ya 🙏")
	}

	st := orderState{Items: orderItemsToLines(merged), Step: "ask_recipient"}
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
