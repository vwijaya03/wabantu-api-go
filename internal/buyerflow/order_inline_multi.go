package buyerflow

import (
	"fmt"
	"regexp"
	"strings"
)

var inlineOrderConjunctionRe = regexp.MustCompile(`(?i)(?:,\s*)?\b(?:lalu|dan juga|plus|sama)\b\s+|,\s*\bdan\b\s+`)
var looseDanConjunctionRe = regexp.MustCompile(`(?i)\s+\bdan\b\s+`)

// IsAddItemToOrderMessage — buyer menambah produk saat checkout aktif.
func IsAddItemToOrderMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{"lalu ", "lalu,", "tambah ", "sekalian ", "plus ", "sama ", "dan juga ", "tambahannya", "itu saja"}
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
	parts := trimNonEmpty(inlineOrderConjunctionRe.Split(text, -1))
	if len(parts) >= 2 {
		return parts
	}
	if loose := splitLooseDanProductSegments(text); len(loose) >= 2 {
		return loose
	}
	return parts
}

func trimNonEmpty(parts []string) []string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitLooseDanProductSegments(text string) []string {
	parts := trimNonEmpty(looseDanConjunctionRe.Split(text, -1))
	if len(parts) < 2 {
		return nil
	}
	n := 0
	for _, p := range parts {
		if looksLikeOrderSegment(p) {
			n++
		}
	}
	if n < 2 {
		return nil
	}
	return parts
}

func looksLikeOrderSegment(seg string) bool {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return false
	}
	if mentionsOrderQty(seg) {
		return true
	}
	lower := strings.ToLower(seg)
	if strings.Contains(lower, "gram") || strings.Contains(lower, " gr") || strings.HasSuffix(lower, "gr") {
		return true
	}
	for _, tok := range tokenize(lower) {
		if gluedMeasureRe.MatchString(tok) {
			return true
		}
	}
	return false
}

func splitOrderTextSegments(userText string) []string {
	userText = normalizeOrderListText(userText)
	inline := splitInlineOrderSegments(userText)
	if len(inline) >= 2 {
		return inline
	}
	var lines []string
	for _, line := range strings.Split(userText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isOrderListHeaderLine(line) || isAppendFooterLine(line) {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) >= 2 {
		return lines
	}
	return inline
}

func isCheckoutFormFieldMessage(userText string) bool {
	if hasRecipientHintInMessage(userText) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if orderAddrHintRe.MatchString(text) || postalCodeIDRe.MatchString(text) {
		return true
	}
	return false
}

func isAppendFooterLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "tambahannya") || strings.Contains(lower, "itu saja")
}

// ParseStructuredOrderLine — satu baris order (qty + SKU) untuk structured/inline multi.
func ParseStructuredOrderLine(raw string, catalog []CatalogItem) OrderLineState {
	return parseStructuredOrderLine(raw, catalog, nil)
}

// ParseStructuredOrderLineWithVector — structured line parse with optional vector context.
func ParseStructuredOrderLineWithVector(raw string, catalog []CatalogItem, vctx *CatalogVectorContext) OrderLineState {
	return parseStructuredOrderLine(raw, catalog, vctx)
}

func parseStructuredOrderLine(raw string, catalog []CatalogItem, vctx *CatalogVectorContext) OrderLineState {
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
	match := matchCatalogLine(text, catalog, vctx)
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
	return ParseInlineMultiOrderLinesWithVector(userText, catalog, nil)
}

func ParseInlineMultiOrderLinesWithVector(userText string, catalog []CatalogItem, vctx *CatalogVectorContext) []OrderLineState {
	segments := splitInlineOrderSegments(userText)
	if len(segments) < 2 {
		return nil
	}
	var lines []OrderLineState
	for _, seg := range segments {
		line := parseStructuredOrderLine(seg, catalog, vctx)
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
	if st.Qty < 1 {
		st.Qty = 1
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

func parseAppendSegments(userText string, catalog []CatalogItem, vctx *CatalogVectorContext) (qtyOnly *int, newLines []OrderLineState) {
	segments := splitOrderTextSegments(userText)
	if len(segments) >= 2 {
		for _, seg := range segments {
			line := parseStructuredOrderLine(seg, catalog, vctx)
			if line.CatalogItemID != "" {
				newLines = append(newLines, line)
				continue
			}
			if q, ok := parseOrderQty(seg); ok && matchCatalogLine(seg, catalog, vctx) == nil {
				qtyOnly = &q
			}
		}
		if len(newLines) == 0 {
			newLines = appendLinesFromIdentifiedCatalog(userText, catalog, newLines)
		}
		return qtyOnly, newLines
	}
	// Conjunction (lalu/plus) is a multi-line splitter, not an append permission.
	// "lalu maggi percik 1" while percik is already in cart still needs this branch
	// so MergeOrderLines can bump qty of the same SKU.
	if IsAddItemToOrderMessage(userText) || IsCheckoutMergeIntent(userText) {
		line := parseStructuredOrderLine(userText, catalog, vctx)
		if line.CatalogItemID != "" {
			newLines = []OrderLineState{line}
		}
		newLines = appendLinesFromIdentifiedCatalog(userText, catalog, newLines)
	}
	return qtyOnly, newLines
}

func appendLinesFromIdentifiedCatalog(userText string, catalog []CatalogItem, existing []OrderLineState) []OrderLineState {
	seen := map[string]struct{}{}
	for _, ln := range existing {
		if ln.CatalogItemID != "" {
			seen[ln.CatalogItemID] = struct{}{}
		}
	}
	out := existing
	for _, it := range catalogItemsIdentifiedInText(userText, catalog) {
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, orderLineFromCatalogItem(it, 1))
	}
	return out
}

// TryAppendItemsDuringCheckout appends parsed items to active checkout state.
func TryAppendItemsDuringCheckout(st *OrderState, userText string, catalog []CatalogItem, tmpl orderFlowTemplates, formal bool, vctx *CatalogVectorContext) (bool, string) {
	if st == nil {
		return false, ""
	}
	if IsOrderRevisionMessage(userText) || IsCartLineCorrectionIntent(userText) || IsNegatedFullOrderCancel(userText) {
		return false, ""
	}
	if isCheckoutFormFieldMessage(userText) {
		return false, ""
	}
	step := normalizeOrderState(*st).Step
	if !isCheckoutAppendStep(step) {
		return false, ""
	}

	if shouldReviseToSiblingSKU(*st, userText, catalog) {
		match := resolveOrderProductMatch(userText, nil, catalog, vctx)
		if match != nil {
			applyCatalogMatch(st, match)
			st.Items = nil
			if q, ok := parseOrderQty(userText); ok {
				st.Qty = q
			} else if st.Qty < 1 {
				st.Qty = 1
			}
			if !st.VariantComplete() {
				st.Step = "ask_variant"
				return true, buildOrderFlowReply(*st, tmpl.AskVariant, catalog)
			}
			if st.Qty < 1 {
				st.Step = "ask_qty"
				return true, buildOrderFlowReply(*st, tmpl.AskQty, catalog)
			}
			st.Step = "ask_recipient"
			return true, buildOrderFlowReply(*st, tmpl.AskRecipient, catalog)
		}
	}

	qtyOnly, newLines := parseAppendSegments(userText, catalog, vctx)
	if len(newLines) < 2 && lexicalBrandAmbiguous(userText, catalog) {
		if reply, ok := orderLexicalBrandPickerReply(formal, userText, catalog); ok {
			return true, reply
		}
	}
	if len(newLines) == 0 {
		if line := parseCheckoutAppendLine(*st, userText, catalog, vctx); line.CatalogItemID != "" {
			newLines = []OrderLineState{line}
		}
	}
	if len(newLines) == 0 && !IsAddItemToOrderMessage(userText) && !IsCheckoutMergeIntent(userText) {
		if qtyOnly != nil {
			return true, buildOrderFlowReply(*st, tmpl.AskRecipient, catalog)
		}
		return false, ""
	}

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
		if checkoutLinesNeedApparelVariant(*st) {
			st.Step = "ask_variant"
			return true, checkoutItemAddedAck(formal) + "\n\n" + buildOrderFlowReply(*st, tmpl.AskVariant, catalog)
		}
		st.Step = "ask_qty"
		return true, checkoutItemAddedAck(formal) + "\n\n" + buildOrderFlowReply(*st, tmpl.AskQty, catalog)
	}
	return true, checkoutItemAddedAck(formal) + "\n\n" + buildOrderFlowReply(*st, tmpl.AskRecipient, catalog)
}

func checkoutLinesNeedApparelVariant(st OrderState) bool {
	if st.HasMultiItems() {
		for _, ln := range st.Items {
			if !lineVariantComplete(ln) {
				return true
			}
		}
		return false
	}
	return !st.VariantComplete()
}

func parseCheckoutAppendLine(st OrderState, userText string, catalog []CatalogItem, vctx *CatalogVectorContext) OrderLineState {
	match := resolveOrderProductMatch(userText, nil, catalog, vctx)
	if match == nil {
		return OrderLineState{}
	}
	if match.ID == st.CatalogItemID {
		return OrderLineState{}
	}
	for _, ln := range st.Items {
		if ln.CatalogItemID == match.ID {
			return OrderLineState{}
		}
	}
	return parseStructuredOrderLine(userText, catalog, vctx)
}

func namesOtherCheckoutSKU(st OrderState, userText string, catalog []CatalogItem) bool {
	if IsOrderRevisionMessage(userText) {
		return false
	}
	match := resolveOrderProductMatch(userText, nil, catalog, nil)
	if match == nil {
		return lexicalBrandAmbiguous(userText, catalog)
	}
	if match.ID == st.CatalogItemID {
		return false
	}
	for _, ln := range st.Items {
		if ln.CatalogItemID == match.ID {
			return false
		}
	}
	return true
}

// shouldImplicitAppendDifferentSKU — append SKU baru (bukan revisi qty item yang sama).
func shouldImplicitAppendDifferentSKU(st OrderState, userText string, catalog []CatalogItem) bool {
	if lexicalBrandAmbiguous(userText, catalog) {
		return true
	}
	return parseCheckoutAppendLine(st, userText, catalog, nil).CatalogItemID != ""
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
