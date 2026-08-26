package buyerflow

import "strings"

func applyLineToOrderState(st *OrderState, line OrderLineState) {
	if st == nil {
		return
	}
	st.CatalogItemID = line.CatalogItemID
	st.ExternalCode = line.ExternalCode
	st.ProductName = line.ProductName
	st.Size = line.Size
	st.Color = line.Color
	st.Qty = line.Qty
	st.UnitPrice = line.UnitPrice
	st.SellUnit = line.SellUnit
	st.WarehouseID = line.WarehouseID
}

func (st OrderState) HasMultiItems() bool {
	return len(st.Items) > 0
}

func lineVariantComplete(line OrderLineState) bool {
	it := &CatalogItem{Name: line.ProductName, ExternalCode: line.ExternalCode}
	if !catalogItemNeedsVariant(it) {
		return true
	}
	return line.Size != "" || line.Color != ""
}

func (st OrderState) StructuredLinesReady() bool {
	if !st.HasMultiItems() {
		return false
	}
	for _, ln := range st.Items {
		if strings.TrimSpace(ln.CatalogItemID) == "" || ln.Qty < 1 || !lineVariantComplete(ln) {
			return false
		}
	}
	return true
}

func (st OrderState) ReadyToPersist() bool {
	if st.HasMultiItems() {
		if !st.StructuredLinesReady() {
			return false
		}
	} else {
		norm := normalizeOrderState(st)
		if !norm.ProductComplete() || strings.TrimSpace(norm.CatalogItemID) == "" || !norm.VariantComplete() || norm.Qty < 1 {
			return false
		}
	}
	if strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "" {
		return false
	}
	return st.ShippingComplete()
}
