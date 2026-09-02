package buyerflow

import (
	"fmt"
	"regexp"
	"strings"
)

var inlineOrderConjunctionRe = regexp.MustCompile(`(?i)(?:,\s*)?\b(?:lalu|dan juga|plus|sama)\b\s+`)

// IsAddItemToOrderMessage — buyer menambah produk saat checkout aktif.
func IsAddItemToOrderMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{"lalu ", "lalu,", "tambah ", "sekalian ", "plus ", "sama ", "dan juga "}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return IsInlineMultiOrderMessage(userText)
}

// IsInlineMultiOrderMessage — satu baris berisi ≥2 segmen (split konjungsi).
func IsInlineMultiOrderMessage(userText string) bool {
	return len(splitInlineOrderSegments(userText)) >= 2
}

// SplitInlineOrderSegments splits a single-line multi-product message on conjunctions.
func SplitInlineOrderSegments(userText string) []string {
	return splitInlineOrderSegments(userText)
}

func splitInlineOrderSegments(userText string) []string {
	text := strings.TrimSpace(userText)
	if text == "" {
		return nil
	}
	parts := inlineOrderConjunctionRe.Split(text, -1)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseStructuredOrderLine — satu baris order (qty + SKU) untuk structured/inline multi.
func ParseStructuredOrderLine(raw string, catalog []CatalogItem) OrderLineState {
	var line OrderLineState
	text := strings.TrimSpace(raw)
	if text == "" {
		return line
	}
	if q, ok := parseOrderQty(text); ok {
		line.Qty = q
	}
	sz, cl := parseSizeAndColor(text)
	if sz != "" {
		line.Size = sz
	}
	if cl != "" {
		line.Color = cl
	}
	match := matchCatalogItem(text, catalog)
	if match == nil {
		cleaned := StripOrderSizeTokens(text)
		cleaned = strings.TrimSpace(strings.TrimRight(cleaned, "ya"))
		match = matchCatalogItem(cleaned, catalog)
	}
	if match != nil {
		line.CatalogItemID = match.ID
		line.ExternalCode = match.ExternalCode
		line.ProductName = match.Name
		line.UnitPrice = match.SellPrice
		line.SellUnit = match.SellUnit
		if line.Qty < 1 {
			line.Qty = 1
		}
	}
	return line
}

// ParseInlineMultiOrderLines parses conjunction-split segments into order lines (≥2 matched SKUs).
func ParseInlineMultiOrderLines(userText string, catalog []CatalogItem) []OrderLineState {
	segments := splitInlineOrderSegments(userText)
	if len(segments) < 2 {
		return nil
	}
	var lines []OrderLineState
	for _, seg := range segments {
		line := ParseStructuredOrderLine(seg, catalog)
		if line.CatalogItemID == "" && line.ProductName == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) < 2 {
		return nil
	}
	return lines
}

func orderLineFromState(st OrderState) OrderLineState {
	st = normalizeOrderState(st)
	return OrderLineState{
		CatalogItemID: st.CatalogItemID,
		ExternalCode:  st.ExternalCode,
		ProductName:   st.ProductName,
		Size:          st.Size,
		Color:         st.Color,
		Qty:           st.Qty,
		UnitPrice:     st.UnitPrice,
		SellUnit:      st.SellUnit,
		WarehouseID:   st.WarehouseID,
	}
}

func ensureMultiItemsFromSingle(st *OrderState) {
	if st == nil || st.HasMultiItems() {
		return
	}
	if strings.TrimSpace(st.CatalogItemID) == "" {
		return
	}
	st.Items = []OrderLineState{orderLineFromState(*st)}
}

// MergeOrderLines deduplicates by catalogItemId and sums qty for identical SKUs.
func MergeOrderLines(existing, added []OrderLineState) []OrderLineState {
	byID := make(map[string]int)
	out := make([]OrderLineState, len(existing))
	copy(out, existing)
	for i, ln := range out {
		if ln.CatalogItemID != "" {
			byID[ln.CatalogItemID] = i
		}
	}
	for _, ln := range added {
		if ln.CatalogItemID == "" {
			out = append(out, ln)
			continue
		}
		if idx, ok := byID[ln.CatalogItemID]; ok {
			out[idx].Qty += ln.Qty
			if out[idx].Qty < 1 {
				out[idx].Qty = ln.Qty
			}
			continue
		}
		byID[ln.CatalogItemID] = len(out)
		out = append(out, ln)
	}
	return out
}

func parseAppendSegments(userText string, catalog []CatalogItem) (qtyOnly *int, newLines []OrderLineState) {
	if IsInlineMultiOrderMessage(userText) {
		for _, seg := range splitInlineOrderSegments(userText) {
			line := ParseStructuredOrderLine(seg, catalog)
			if line.CatalogItemID != "" {
				newLines = append(newLines, line)
				continue
			}
			if q, ok := parseOrderQty(seg); ok && matchCatalogItem(seg, catalog) == nil {
				qtyOnly = &q
			}
		}
		return qtyOnly, newLines
	}
	if IsAddItemToOrderMessage(userText) {
		line := ParseStructuredOrderLine(userText, catalog)
		if line.CatalogItemID != "" {
			newLines = []OrderLineState{line}
		}
	}
	return qtyOnly, newLines
}

// TryAppendItemsDuringCheckout appends parsed items to active checkout state.
func TryAppendItemsDuringCheckout(st *OrderState, userText string, catalog []CatalogItem, tmpl orderFlowTemplates, formal bool) (bool, string) {
	if st == nil {
		return false, ""
	}
	step := normalizeOrderState(*st).Step
	if step != "ask_recipient" && step != "ask_qty" && step != "ask_address" && step != "ask_address_full" {
		return false, ""
	}
	if !IsAddItemToOrderMessage(userText) {
		return false, ""
	}

	qtyOnly, newLines := parseAppendSegments(userText, catalog)
	if qtyOnly != nil && !st.HasMultiItems() && strings.TrimSpace(st.CatalogItemID) != "" {
		st.Qty = *qtyOnly
	}
	if len(newLines) == 0 {
		if qtyOnly != nil {
			return true, buildOrderFlowReply(*st, tmpl.AskRecipient, catalog)
		}
		return false, ""
	}

	ensureMultiItemsFromSingle(st)
	st.Items = MergeOrderLines(st.Items, newLines)
	if len(st.Items) > 0 {
		applyLineToOrderState(st, st.Items[0])
	}
	st.Step = step

	guarded, reply, blocked := GuardStructuredOrderStock(*st, catalog, formal)
	*st = guarded
	if blocked {
		return true, reply
	}
	if !st.StructuredLinesReady() {
		st.Step = "ask_variant"
		return true, buildOrderFlowReply(*st, tmpl.AskVariant, catalog)
	}
	return true, buildOrderFlowReply(*st, tmpl.AskRecipient, catalog)
}

func GuardStructuredOrderStock(st OrderState, catalog []CatalogItem, formal bool) (OrderState, string, bool) {
	if !st.HasMultiItems() {
		return st, "", false
	}
	for i := range st.Items {
		lineSt := OrderState{}
		applyLineToOrderState(&lineSt, st.Items[i])
		lineSt, reply, blocked := guardOrderQtyStep(lineSt, catalog, formal, "ask_qty")
		if blocked {
			prefix := fmt.Sprintf("Baris %d (%s): ", i+1, shortDisplayName(st.Items[i].ProductName))
			return st, prefix + reply, true
		}
		st.Items[i].WarehouseID = lineSt.WarehouseID
	}
	return st, "", false
}
