package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"encore.dev/rlog"

	"encore.app/wabantu/inventory"
	"encore.app/wabantu/order"
)

var errMultiDraftDisambiguation = errors.New("multi draft disambiguation required")

func persistDraftOrder(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema string,
	convoID, contactID string,
	st orderState,
) (orderID string, err error) {
	st = normalizeOrderState(st)
	if st.HasMultiItems() {
		return persistDraftOrderMulti(ctx, tq, tenantSchema, convoID, contactID, st)
	}
	if !st.ProductComplete() || strings.TrimSpace(st.CatalogItemID) == "" || !st.VariantComplete() || st.Qty < 1 ||
		strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "" ||
		!st.ShippingComplete() {
		return "", fmt.Errorf("order data incomplete")
	}
	reject, _, whID := ensureDraftOrderStock(ctx, tq, st)
	if reject {
		return "", fmt.Errorf("order qty exceeds available stock")
	}
	if whID != "" {
		st.WarehouseID = whID
	}
	qty := st.Qty
	if qty < 1 {
		qty = 1
	}
	variant := buildVariantLabel(st.Size, st.Color)
	if variant == "" {
		variant = strings.TrimSpace(st.Variant)
	}
	unitPrice := st.UnitPrice
	item := order.OrderItem{
		CatalogItemID: st.CatalogItemID,
		ExternalCode:  st.ExternalCode,
		Name:          st.ProductName,
		Variant:       variant,
		Size:          st.Size,
		Color:         st.Color,
		Qty:           float64(qty),
		UnitPrice:     unitPrice,
		SellUnit:      st.SellUnit,
		WarehouseID:   strings.TrimSpace(st.WarehouseID),
	}
	subtotal := float64(qty) * unitPrice
	addr := order.ShippingAddress{
		Name:       st.RecipientName,
		Phone:      st.RecipientPhone,
		Street:     st.Street,
		RT:         st.RT,
		RW:         st.RW,
		Kelurahan:  st.Kelurahan,
		Kecamatan:  st.Kecamatan,
		City:       st.City,
		Province:   st.Province,
		PostalCode: st.PostalCode,
		Country:    st.Country,
	}
	if addr.Country == "" {
		addr.Country = "Indonesia"
	}

	itemsJSON, _ := json.Marshal([]order.OrderItem{item})
	addrJSON, _ := json.Marshal(addr)

	id, updated, needPick, _, uerr := upsertDraftOrderItems(ctx, tq, tenantSchema, convoID, contactID, st.PersistedOrderID, []order.OrderItem{item}, addrJSON, st.ForceNewOrder)
	if uerr != nil {
		return "", uerr
	}
	if needPick {
		return "", errMultiDraftDisambiguation
	}
	if updated {
		syncContactDisplayNameFromOrder(ctx, tq, contactID, st.RecipientName)
		rlog.Info("AI order: draft updated", "orderId", id, "convoId", convoID)
		return id, nil
	}

	var convArg, contactArg any
	if strings.TrimSpace(convoID) != "" {
		convArg = convoID
	}
	if strings.TrimSpace(contactID) != "" {
		contactArg = contactID
	}

	insertQ := fmt.Sprintf(`
		INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, shipping_address, notes,
			 status, subtotal, shipping_cost, total)
		VALUES ($1, $2, $3, $4, '', 'draft', $5, 0, $5)
		RETURNING id::text`, tenantSchema)

	err = tq.QueryRowContext(ctx, insertQ, convArg, contactArg, itemsJSON, addrJSON, subtotal).Scan(&orderID)
	if err != nil {
		return "", err
	}
	syncContactDisplayNameFromOrder(ctx, tq, contactID, st.RecipientName)
	syncPersistedOrderStock(ctx, tenantSchema, orderID, "draft", []order.OrderItem{item})
	rlog.Info("AI order: draft persisted",
		"orderId", orderID,
		"convoId", convoID,
		"product", previewText(st.ProductName, 60),
		"qty", qty,
		"postalCode", st.PostalCode,
	)
	return orderID, nil
}

func persistDraftOrderMulti(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema string,
	convoID, contactID string,
	st orderState,
) (orderID string, err error) {
	st = normalizeOrderState(st)
	if !st.ReadyToPersist() {
		return "", fmt.Errorf("order data incomplete")
	}
	var orderItems []order.OrderItem
	var subtotal float64
	for i, ln := range st.Items {
		lineSt := orderStateFromLine(ln)
		reject, _, whID := ensureDraftOrderStock(ctx, tq, lineSt)
		if reject {
			return "", fmt.Errorf("order qty exceeds available stock for line %d", i+1)
		}
		if whID != "" {
			st.Items[i].WarehouseID = whID
			ln.WarehouseID = whID
		}
		qty := ln.Qty
		if qty < 1 {
			qty = 1
		}
		variant := buildVariantLabel(ln.Size, ln.Color)
		item := order.OrderItem{
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
		}
		orderItems = append(orderItems, item)
		subtotal += float64(qty) * ln.UnitPrice
	}
	addr := order.ShippingAddress{
		Name:       st.RecipientName,
		Phone:      st.RecipientPhone,
		Street:     st.Street,
		RT:         st.RT,
		RW:         st.RW,
		Kelurahan:  st.Kelurahan,
		Kecamatan:  st.Kecamatan,
		City:       st.City,
		Province:   st.Province,
		PostalCode: st.PostalCode,
		Country:    st.Country,
	}
	if addr.Country == "" {
		addr.Country = "Indonesia"
	}
	itemsJSON, _ := json.Marshal(orderItems)
	addrJSON, _ := json.Marshal(addr)

	id, updated, needPick, _, uerr := upsertDraftOrderItems(ctx, tq, tenantSchema, convoID, contactID, st.PersistedOrderID, orderItems, addrJSON, st.ForceNewOrder)
	if uerr != nil {
		return "", uerr
	}
	if needPick {
		return "", errMultiDraftDisambiguation
	}
	if updated {
		syncContactDisplayNameFromOrder(ctx, tq, contactID, st.RecipientName)
		rlog.Info("AI order: multi-item draft updated", "orderId", id, "convoId", convoID, "itemCount", len(orderItems))
		return id, nil
	}

	var convArg, contactArg any
	if strings.TrimSpace(convoID) != "" {
		convArg = convoID
	}
	if strings.TrimSpace(contactID) != "" {
		contactArg = contactID
	}

	insertQ := fmt.Sprintf(`
		INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, shipping_address, notes,
			 status, subtotal, shipping_cost, total)
		VALUES ($1, $2, $3, $4, '', 'draft', $5, 0, $5)
		RETURNING id::text`, tenantSchema)

	err = tq.QueryRowContext(ctx, insertQ, convArg, contactArg, itemsJSON, addrJSON, subtotal).Scan(&orderID)
	if err != nil {
		return "", err
	}
	syncContactDisplayNameFromOrder(ctx, tq, contactID, st.RecipientName)
	syncPersistedOrderStock(ctx, tenantSchema, orderID, "draft", orderItems)
	rlog.Info("AI order: multi-item draft persisted",
		"orderId", orderID,
		"convoId", convoID,
		"itemCount", len(orderItems),
		"postalCode", st.PostalCode,
	)
	return orderID, nil
}

func aiOrderStockItems(items []order.OrderItem) []inventory.OrderStockItem {
	out := make([]inventory.OrderStockItem, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.CatalogItemID) == "" {
			continue
		}
		out = append(out, inventory.OrderStockItem{
			LineID:        it.LineID,
			CatalogItemID: it.CatalogItemID,
			WarehouseID:   it.WarehouseID,
			Qty:           it.Qty,
		})
	}
	return out
}

// syncPersistedOrderStock mirrors order.Create stock sync so status changes from draft
// to processing later reconcile inventory idempotently.
func syncPersistedOrderStock(ctx context.Context, tenantSchema, orderID, status string, items []order.OrderItem) {
	if strings.TrimSpace(orderID) == "" || len(items) == 0 {
		return
	}
	if err := inventory.SyncOrderStock(ctx, tenantSchema, orderID, status, aiOrderStockItems(items), "system"); err != nil {
		rlog.Warn("AI order: stock sync failed", "err", err, "orderId", orderID, "status", status)
	}
}

// persistDraftOrderEarly saves cart lines as draft without requiring recipient/shipping (mid-checkout).
// Returns (orderID, needPick, pickList, error).
func persistDraftOrderEarly(
	ctx context.Context,
	tq tenantScopedQuerier,
	tenantSchema, convoID, contactID string,
	st orderState,
) (string, bool, []persistedOrder, error) {
	st = normalizeOrderState(st)
	if !st.CartReadyForDraft() {
		return "", false, nil, nil
	}
	orderItems, subtotal, err := orderItemsFromCartState(ctx, tq, st)
	if err != nil {
		return "", false, nil, err
	}
	if len(orderItems) == 0 {
		return "", false, nil, nil
	}
	addr := order.ShippingAddress{Country: "Indonesia"}
	addrJSON, _ := json.Marshal(addr)

	id, updated, needPick, pickList, uerr := upsertDraftOrderItems(ctx, tq, tenantSchema, convoID, contactID, st.PersistedOrderID, orderItems, nil, st.ForceNewOrder)
	if uerr != nil {
		return "", false, nil, uerr
	}
	if needPick {
		return "", true, pickList, nil
	}
	if updated {
		rlog.Info("AI order: early draft updated", "orderId", id, "convoId", convoID, "itemCount", len(orderItems))
		return id, false, nil, nil
	}

	var convArg, contactArg any
	if strings.TrimSpace(convoID) != "" {
		convArg = convoID
	}
	if strings.TrimSpace(contactID) != "" {
		contactArg = contactID
	}
	itemsJSON, _ := json.Marshal(orderItems)
	var orderID string
	insertQ := fmt.Sprintf(`
		INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, shipping_address, notes,
			 status, subtotal, shipping_cost, total)
		VALUES ($1, $2, $3, $4, '', 'draft', $5, 0, $5)
		RETURNING id::text`, tenantSchema)
	if err := tq.QueryRowContext(ctx, insertQ, convArg, contactArg, itemsJSON, addrJSON, subtotal).Scan(&orderID); err != nil {
		return "", false, nil, err
	}
	syncPersistedOrderStock(ctx, tenantSchema, orderID, "draft", orderItems)
	rlog.Info("AI order: early draft persisted", "orderId", orderID, "convoId", convoID, "itemCount", len(orderItems))
	return orderID, false, nil, nil
}

func orderItemsFromCartState(ctx context.Context, tq tenantScopedQuerier, st orderState) ([]order.OrderItem, float64, error) {
	st = normalizeOrderState(st)
	var orderItems []order.OrderItem
	var subtotal float64
	if st.HasMultiItems() {
		for i, ln := range st.Items {
			lineSt := orderStateFromLine(ln)
			reject, _, whID := ensureDraftOrderStock(ctx, tq, lineSt)
			if reject {
				return nil, 0, fmt.Errorf("order qty exceeds available stock for line %d", i+1)
			}
			if whID != "" {
				ln.WarehouseID = whID
			}
			qty := ln.Qty
			if qty < 1 {
				qty = 1
			}
			item := order.OrderItem{
				CatalogItemID: ln.CatalogItemID,
				ExternalCode:  ln.ExternalCode,
				Name:          ln.ProductName,
				Variant:       buildVariantLabel(ln.Size, ln.Color),
				Size:          ln.Size,
				Color:         ln.Color,
				Qty:           float64(qty),
				UnitPrice:     ln.UnitPrice,
				SellUnit:      ln.SellUnit,
				WarehouseID:   strings.TrimSpace(ln.WarehouseID),
			}
			orderItems = append(orderItems, item)
			subtotal += float64(qty) * ln.UnitPrice
		}
		return orderItems, subtotal, nil
	}
	reject, _, whID := ensureDraftOrderStock(ctx, tq, st)
	if reject {
		return nil, 0, fmt.Errorf("order qty exceeds available stock")
	}
	if whID != "" {
		st.WarehouseID = whID
	}
	qty := st.Qty
	if qty < 1 {
		qty = 1
	}
	variant := buildVariantLabel(st.Size, st.Color)
	if variant == "" {
		variant = strings.TrimSpace(st.Variant)
	}
	item := order.OrderItem{
		CatalogItemID: st.CatalogItemID,
		ExternalCode:  st.ExternalCode,
		Name:          st.ProductName,
		Variant:       variant,
		Size:          st.Size,
		Color:         st.Color,
		Qty:           float64(qty),
		UnitPrice:     st.UnitPrice,
		SellUnit:      st.SellUnit,
		WarehouseID:   strings.TrimSpace(st.WarehouseID),
	}
	orderItems = append(orderItems, item)
	subtotal = float64(qty) * st.UnitPrice
	return orderItems, subtotal, nil
}
