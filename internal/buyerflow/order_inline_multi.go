package buyerflow

import (
	"fmt"
	"regexp"
	"strings"
)

var inlineOrderConjunctionRe = regexp.MustCompile(`(?i)(?:,\s*)?\b(?:lalu|dan juga|plus|sama)\b\s+|,\s*\bdan\b\s+`)
var looseDanConjunctionRe = regexp.MustCompile(`(?i)\s+\bdan\b\s+`)

// EN: ", and" (list) dan "and" polos — keduanya dijaga looksLikeOrderSegment (lihat splitEnglishAndSegments).
var commaAndConjunctionRe = regexp.MustCompile(`(?i),\s*\band\b\s+`)
var looseAndConjunctionRe = regexp.MustCompile(`(?i)\s+\band\b\s+`)
var questionSegmentStartRe = regexp.MustCompile(`(?i)^(?:how|what|when|where|why|which|who|can|could|do|does|is|are|will|would)\b`)

// IsAddItemToOrderMessage — buyer menambah produk saat checkout aktif.
func IsAddItemToOrderMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{"lalu ", "lalu,", "nambah ", "nambah,", "tambah ", "sekalian ", "plus ", "sama ", "dan juga ", "tambahannya", "itu saja"}
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

var trailingOrderPoliteRe = regexp.MustCompile(`(?i)(?:,\s*)?(?:please|thanks|thank you|pls)\s*[.!?]*\s*$`)

func stripTrailingOrderPolite(text string) string {
	return strings.TrimSpace(trailingOrderPoliteRe.ReplaceAllString(strings.TrimSpace(text), ""))
}

func splitInlineOrderSegments(userText string) []string {
	text := stripTrailingOrderPolite(userText)
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
	if en := splitEnglishAndSegments(text); len(en) >= 2 {
		return en
	}
	if comma := splitLooseCommaProductSegments(text); len(comma) >= 2 {
		return comma
	}
	return parts
}

// splitEnglishAndSegments memecah ", and" / "and" (EN) menjadi segmen produk.
//   - ", and": minimal satu segmen tampak seperti baris order (jangkar); sisanya bukan pertanyaan.
//   - "and" polos: minimal dua segmen tampak seperti baris order (hindari "salt and pepper").
//
// Segmen hasil split yang masih berisi koma ("1 salmon, 1 tuna, and 2 beef") dipecah lagi.
//
// Segmen pertanyaan di belakang ("... and can you do same day delivery?") bukan baris order:
// segmen itu dibuang dan baris order sebelumnya tetap dipertahankan. Segmen yang tampak seperti
// baris order tetap dipakai walau berakhir "please?" (mis. "can i order 1 tuna and 1 beef please?").
func splitEnglishAndSegments(text string) []string {
	anchors := 1
	parts := trimNonEmpty(commaAndConjunctionRe.Split(text, -1))
	if len(parts) < 2 {
		anchors = 2
		parts = trimNonEmpty(looseAndConjunctionRe.Split(text, -1))
	}
	if len(parts) < 2 {
		return nil
	}
	n := 0
	kept := make([]string, 0, len(parts))
	for i, p := range parts {
		isOrder := looksLikeOrderSegment(p)
		if i > 0 && !isOrder && (strings.Contains(p, "?") || questionSegmentStartRe.MatchString(p)) {
			continue
		}
		if isOrder {
			n++
		}
		kept = append(kept, p)
	}
	if n < anchors || len(kept) < 2 {
		return nil
	}
	var out []string
	for _, p := range kept {
		if sub := splitLooseCommaProductSegments(p); len(sub) >= 2 {
			out = append(out, sub...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func splitLooseCommaProductSegments(text string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] != ',' {
			continue
		}
		if i > 0 && i+1 < len(text) && isASCIIDigit(text[i-1]) && isASCIIDigit(text[i+1]) {
			continue
		}
		parts = append(parts, strings.TrimSpace(text[start:i]))
		start = i + 1
	}
	parts = append(parts, strings.TrimSpace(text[start:]))
	parts = trimNonEmpty(parts)
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

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
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
	text := stripOrderSegmentPrefix(raw)
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
	qtyOnly, newLines, _ = parseAppendSegmentsDetailed(userText, catalog, vctx)
	return qtyOnly, newLines
}

// parseAppendSegmentsDetailed is parseAppendSegments plus the segments that name a product line
// with several variants ("australian beef") and so were not added. Callers ask the buyer which
// variant they mean instead of dropping the segment silently.
func parseAppendSegmentsDetailed(userText string, catalog []CatalogItem, vctx *CatalogVectorContext) (qtyOnly *int, newLines []OrderLineState, ambiguous []string) {
	segments := splitOrderTextSegments(userText)
	if len(segments) >= 2 {
		for _, seg := range segments {
			line := parseStructuredOrderLine(seg, catalog, vctx)
			if line.CatalogItemID != "" {
				newLines = append(newLines, line)
				continue
			}
			if lexicalBrandAmbiguous(seg, catalog) {
				ambiguous = append(ambiguous, seg)
				continue
			}
			if q, ok := parseOrderQty(seg); ok && matchCatalogLine(seg, catalog, vctx) == nil {
				qtyOnly = &q
			}
		}
		if len(newLines) == 0 {
			newLines = appendLinesFromIdentifiedCatalog(userText, catalog, newLines)
		}
		return qtyOnly, newLines, ambiguous
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
	return qtyOnly, newLines, nil
}

// ambiguousSegmentsQuestion asks which variant the buyer means for each segment that named a
// multi-variant product line (one list per brand). userText picks the reply language.
func ambiguousSegmentsQuestion(formal bool, segments []string, catalog []CatalogItem, userText string) string {
	var asks []string
	seen := map[string]bool{}
	for _, seg := range segments {
		brand := brandTokenFromText(seg, catalog)
		if brand == "" || seen[brand] {
			continue
		}
		seen[brand] = true
		if body := formatBrandVariantListBody(formal, brand, catalogItemsForBrand(brand, catalog), 5, userText); body != "" {
			asks = append(asks, body)
		}
	}
	return strings.Join(asks, "\n\n")
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

// explicitAddWithQtyRe — an add/order verb followed by a number ("tambah 1 tuna", "can i order 2").
var explicitAddWithQtyRe = regexp.MustCompile(`(?i)\b(?:tambah|nambah|add|order|pesan|beli|mau|want)\b[^?\d]*\b\d+\b`)

// productQuestionHintRe — product facts a buyer asks about ("how much", "is it frozen", "ready kak").
var productQuestionHintRe = regexp.MustCompile(`(?i)\b(?:how much|berapa|harga|price|frozen|fresh|beku|segar|weight|berat|ready|stock|stok|available|is it|is the)\b`)

// isProductQuestionNotCartAdd — "I want to ask about tuna" / "is the tuna frozen?" must not append a line to the draft.
func isProductQuestionNotCartAdd(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || mentionsOrderQty(text) {
		return false
	}
	for _, p := range []string{"want to order", "like to order", "ready to order", "mau beli", "mau order", "mau pesan"} {
		if strings.Contains(text, p) {
			return false
		}
	}
	for _, p := range []string{"ask about", "want to ask", "would like to ask", "do you have", "tanya ", "nanya"} {
		if strings.Contains(text, p) {
			return true
		}
	}
	// A question or product-fact hint only counts when the buyer is not explicitly adding a qty.
	if explicitAddWithQtyRe.MatchString(text) {
		return false
	}
	return looksLikeBuyerQuestion(text) || productQuestionHintRe.MatchString(text)
}

// TryAppendItemsDuringCheckout appends parsed items to active checkout state.
func TryAppendItemsDuringCheckout(st *OrderState, userText string, catalog []CatalogItem, tmpl orderFlowTemplates, formal bool, vctx *CatalogVectorContext) (bool, string) {
	if st == nil {
		return false, ""
	}
	if IsOrderRevisionMessage(userText) || IsCartLineCorrectionIntent(userText) || IsNegatedFullOrderCancel(userText) {
		return false, ""
	}
	if isProductQuestionNotCartAdd(userText) {
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

	qtyOnly, newLines, ambiguousSegs := parseAppendSegmentsDetailed(userText, catalog, vctx)
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
	// Segments naming a multi-variant line ("australian beef") are asked, never dropped silently.
	variantAsk := ambiguousSegmentsQuestion(formal, ambiguousSegs, catalog, userText)
	if len(newLines) == 0 && variantAsk != "" {
		return true, variantAsk
	}
	if variantAsk != "" {
		variantAsk = "\n\n" + variantAsk
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
			return true, checkoutItemAddedAck(formal, userText) + "\n\n" + buildOrderFlowReply(*st, tmpl.AskVariant, catalog) + variantAsk
		}
		st.Step = "ask_qty"
		return true, checkoutItemAddedAck(formal, userText) + "\n\n" + buildOrderFlowReply(*st, tmpl.AskQty, catalog) + variantAsk
	}
	return true, checkoutItemAddedAck(formal, userText) + "\n\n" + buildOrderFlowReply(*st, tmpl.AskRecipient, catalog) + variantAsk
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

// tryStartSegmentedCheckout starts checkout from a comma/conjunction list:
// unique SKUs go into the cart; an ambiguous brand segment opens a variant picker
// instead of auto-picking one SKU from the whole message.
func tryStartSegmentedCheckout(in OrderFlowInput, formal bool, tmpl orderFlowTemplates) (OrderFlowResult, bool) {
	if in.State != nil {
		return OrderFlowResult{}, false
	}
	segs := splitInlineOrderSegments(in.UserText)
	if len(segs) < 2 {
		return OrderFlowResult{}, false
	}
	var lines []OrderLineState
	pickerFrom := ""
	for _, seg := range segs {
		line := parseStructuredOrderLine(seg, in.Catalog, in.VectorCtx)
		if line.CatalogItemID != "" {
			if line.Qty < 1 {
				line.Qty = 1
			}
			lines = append(lines, line)
			continue
		}
		if pickerFrom == "" && lexicalBrandAmbiguous(seg, in.Catalog) {
			pickerFrom = seg
		}
	}
	if len(lines) == 0 {
		return OrderFlowResult{}, false
	}
	if len(lines) == 1 && pickerFrom == "" {
		return OrderFlowResult{}, false
	}

	st := OrderState{Step: "ask_recipient", Items: lines}
	applyLineToOrderState(&st, lines[0])

	if pickerFrom != "" {
		st.Step = "ask_variant"
		reply, ok := orderLexicalBrandPickerReply(formal, pickerFrom, in.Catalog)
		if !ok {
			reply, ok = orderVectorVariantPickerReply(formal, pickerFrom, in.Catalog, in.VectorCtx)
		}
		if !ok {
			return OrderFlowResult{}, false
		}
		summary := formatOrderSummary(st)
		if summary != "" {
			reply = "Sudah masuk keranjang:\n\n" + summary + "\n\n" + reply
		}
		return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}, true
	}

	guarded, reply, blocked := GuardStructuredOrderStock(st, in.Catalog, formal)
	st = guarded
	if blocked {
		return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: reply}, true
	}
	if !st.StructuredLinesReady() {
		if checkoutLinesNeedApparelVariant(st) {
			st.Step = "ask_variant"
			return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: buildOrderFlowReply(st, tmpl.AskVariant, in.Catalog)}, true
		}
		st.Step = "ask_qty"
		return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: buildOrderFlowReply(st, tmpl.AskQty, in.Catalog)}, true
	}
	st.Step = "ask_recipient"
	return OrderFlowResult{State: &st, Path: PathOrderFlow, Reply: buildOrderFlowReply(st, tmpl.AskRecipient, in.Catalog)}, true
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
