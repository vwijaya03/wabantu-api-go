package buyerflow

import (
	"strings"
)

// IsNegatedFullOrderCancel — "bukan batal semua", bukan perintah batal.
func IsNegatedFullOrderCancel(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" {
		return false
	}
	hasNeg := strings.Contains(text, "bukan") || strings.Contains(text, "jangan") ||
		strings.Contains(text, "ga usah") || strings.Contains(text, "gak usah") ||
		strings.Contains(text, "nggak usah")
	hasAll := strings.Contains(text, "semua") || strings.Contains(text, "seluruh")
	hasCancel := orderCancelWordRe.MatchString(text) || strings.Contains(text, "dibatalkan")
	return hasNeg && hasAll && hasCancel
}

// IsCartLineCorrectionIntent — ganti/hapus baris keranjang, bukan status DB / batal semua.
func IsCartLineCorrectionIntent(userText string) bool {
	text := normalizeBuyerTextForRules(userText)
	if text == "" || IsNegatedFullOrderCancel(userText) || IsCancelClarificationQuestion(userText) {
		return false
	}
	if isWholeOrderCancelPhrase(text) {
		return false
	}
	if strings.Contains(text, "batalkan yang") || strings.Contains(text, "batalin yang") ||
		strings.Contains(text, "batal yang") {
		return !strings.Contains(text, "semua")
	}
	if strings.Contains(text, "bukan yang") {
		return true
	}
	if strings.Contains(text, "tidak mau") || strings.Contains(text, "ga mau") ||
		strings.Contains(text, "gak mau") || strings.Contains(text, "nggak mau") {
		return strings.Contains(text, "yang") || strings.Contains(text, "bukan")
	}
	if strings.Contains(text, "ga jadi beli") || strings.Contains(text, "gak jadi beli") ||
		strings.Contains(text, "nggak jadi beli") || strings.Contains(text, "tidak jadi beli") {
		return skuCancelRemainder(text)
	}
	if lineCancelWithSKURemainder(text) {
		return true
	}
	return false
}

// lineCancelWithSKURemainder — batal/batalkan/cancel/dibatalkan + sisa teks SKU (bukan batal order kosong).
func lineCancelWithSKURemainder(text string) bool {
	if strings.Contains(text, "dibatalkan") || strings.Contains(text, "dibatalin") {
		return skuCancelRemainder(text)
	}
	return orderCancelWordRe.MatchString(text) && skuCancelRemainder(text)
}

func tryCheckoutCartEdits(st *OrderState, userText string, catalog []CatalogItem, tmpl orderFlowTemplates, formal bool) (bool, string) {
	if st == nil {
		return false, ""
	}
	if IsNegatedFullOrderCancel(userText) {
		ack := "Siap kak, pesanan tidak dibatalkan ya."
		if formal {
			ack = "Baik kak, pesanan tidak dibatalkan."
		}
		return true, ack + "\n\n" + buildOrderFlowReply(*st, checkoutEditPrompt(*st, tmpl), catalog)
	}
	return tryApplyCartLineCorrection(st, userText, catalog, tmpl, formal)
}

func checkoutEditPrompt(st OrderState, tmpl orderFlowTemplates) string {
	switch st.Step {
	case "ask_qty":
		return tmpl.AskQty
	case "ask_address", "ask_address_full":
		return tmpl.AskAddressFull
	default:
		return tmpl.AskRecipient
	}
}

func tryApplyCartLineCorrection(st *OrderState, userText string, catalog []CatalogItem, tmpl orderFlowTemplates, formal bool) (bool, string) {
	if st == nil || len(catalog) == 0 {
		return false, ""
	}
	if !IsCartLineCorrectionIntent(userText) && !IsOrderAmendMessage(userText) {
		return false, ""
	}
	if !ApplyDraftLineMutations(st, userText, catalog) {
		return false, ""
	}
	if !st.ProductComplete() && len(st.Items) == 0 {
		st.Step = "ask_product"
		ack := "Siap kak, item itu sudah dihapus dari pesanan. Mau ganti produk apa?"
		if formal {
			ack = "Baik kak, item tersebut sudah dihapus. Silakan sebut produk penggantinya."
		}
		return true, ack
	}
	if st.Step == "" || st.Step == "ask_product" {
		st.Step = "ask_recipient"
	}
	return true, buildOrderFlowReply(*st, checkoutEditPrompt(*st, tmpl), catalog)
}

func splitCartCorrectionSpans(userText string) (reject, want string) {
	text := strings.TrimSpace(userText)
	lower := strings.ToLower(text)
	for _, m := range []string{"bukan yang", "batalkan yang", "batalin yang", "batal yang"} {
		if i := strings.LastIndex(lower, m); i >= 0 {
			return strings.TrimSpace(text[i:]), strings.TrimSpace(text[:i])
		}
	}
	for _, m := range []string{"tidak mau", "nggak mau", "gak mau", "ga mau"} {
		if i := strings.LastIndex(lower, m); i >= 0 {
			before := strings.TrimSpace(text[:i])
			after := strings.TrimSpace(text[i+len(m):])
			beforeLower := strings.ToLower(before)
			if j := strings.LastIndex(beforeLower, "yang"); j >= 0 {
				return strings.TrimSpace(before[j:] + " " + after), strings.TrimSpace(before[:j])
			}
			return after, before
		}
	}
	for _, m := range []string{"ga jadi beli", "gak jadi beli", "nggak jadi beli", "tidak jadi beli"} {
		if i := strings.Index(lower, m); i >= 0 {
			return strings.TrimSpace(text[i+len(m):]), strings.TrimSpace(text[:i])
		}
	}
	for _, m := range []string{"nya saya mau cancel", "nya mau cancel", "nya cancel", "nya batal"} {
		if i := strings.Index(lower, m); i >= 0 {
			return strings.TrimSpace(text[:i]), ""
		}
	}
	for _, m := range []string{"nya tolong dibatalkan", "tolong dibatalkan", "dibatalkan", "dibatalin"} {
		if i := strings.Index(lower, m); i >= 0 {
			before := strings.TrimSpace(text[:i])
			after := trimCorrectionLinker(strings.TrimSpace(text[i+len(m):]))
			return before, after
		}
	}
	for _, m := range []string{"saya batalkan", "mau batalkan", "batalkan", "batalin"} {
		if i := strings.Index(lower, m); i >= 0 {
			after := strings.TrimSpace(text[i+len(m):])
			before := strings.TrimSpace(text[:i])
			rej, add := splitRejectAndAdd(after)
			return rej, strings.TrimSpace(before + " " + add)
		}
	}
	return "", text
}

func trimCorrectionLinker(s string) string {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	for _, p := range []string{", lalu ", ", kemudian ", "lalu ", "kemudian "} {
		if strings.HasPrefix(low, p) {
			s = strings.TrimSpace(s[len(p):])
			low = strings.ToLower(s)
		}
	}
	return strings.TrimSpace(strings.TrimLeft(s, ",;"))
}

func splitRejectAndAdd(after string) (reject, add string) {
	low := strings.ToLower(after)
	for _, sep := range []string{", lalu ", ", kemudian ", " dan nambah ", " dan tambah ", " nambah ", " tambah "} {
		if i := strings.Index(low, sep); i >= 0 {
			return strings.TrimSpace(after[:i]), strings.TrimSpace(after[i:])
		}
	}
	return strings.TrimSpace(after), ""
}

func catalogItemsIdentifiedInText(userText string, catalog []CatalogItem) []CatalogItem {
	if strings.TrimSpace(userText) == "" || len(catalog) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []CatalogItem
	add := func(it CatalogItem) {
		if it.ID == "" {
			return
		}
		if _, ok := seen[it.ID]; ok {
			return
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	if u := uniqueBrandSKUFromText(userText, catalog); u != nil {
		add(*u)
	}
	if u := uniqueSizedSKUFromText(userText, catalog); u != nil {
		add(*u)
	}
	textLower := strings.ToLower(userText)
	for _, it := range catalog {
		name := strings.ToLower(strings.TrimSpace(it.Name))
		if name != "" && strings.Contains(textLower, name) {
			add(it)
		}
	}
	for _, brand := range brandsMentionedInText(userText, catalog) {
		items := catalogItemsForBrand(brand, catalog)
		for _, it := range itemsHitByUniqueDistinctiveTokens(userText, brand, items) {
			add(it)
		}
	}
	return out
}

func brandsMentionedInText(userText string, catalog []CatalogItem) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, tok := range tokenize(userText) {
		b := fuzzyBrandTokenName(tok, catalog)
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

func cartItemsMatchingBrandOrText(st OrderState, text string, catalog []CatalogItem) []CatalogItem {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	ids := map[string]struct{}{}
	if st.CatalogItemID != "" {
		ids[st.CatalogItemID] = struct{}{}
	}
	for _, ln := range st.Items {
		if ln.CatalogItemID != "" {
			ids[ln.CatalogItemID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var out []CatalogItem
	seen := map[string]struct{}{}
	addIfCart := func(it CatalogItem) {
		if _, ok := ids[it.ID]; !ok {
			return
		}
		if _, dup := seen[it.ID]; dup {
			return
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	for _, it := range catalogItemsIdentifiedInText(text, catalog) {
		addIfCart(it)
	}
	if len(out) > 0 {
		return out
	}
	for _, brand := range brandsMentionedInText(text, catalog) {
		for _, it := range catalogItemsForBrand(brand, catalog) {
			addIfCart(it)
		}
	}
	return out
}

func subtractCatalogItems(items, drop []CatalogItem) []CatalogItem {
	if len(items) == 0 || len(drop) == 0 {
		return items
	}
	skip := catalogItemIDs(drop)
	var out []CatalogItem
	for _, it := range items {
		if _, ok := skip[it.ID]; ok {
			continue
		}
		out = append(out, it)
	}
	return out
}

func catalogItemIDs(items []CatalogItem) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it.ID != "" {
			out[it.ID] = struct{}{}
		}
	}
	return out
}

func filterOrderLinesExcluding(lines []OrderLineState, skip map[string]struct{}) []OrderLineState {
	if len(skip) == 0 {
		return lines
	}
	var out []OrderLineState
	for _, ln := range lines {
		if _, ok := skip[ln.CatalogItemID]; ok {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func orderCartContainsID(st OrderState, id string) bool {
	if id == "" {
		return false
	}
	if st.CatalogItemID == id {
		return true
	}
	for _, ln := range st.Items {
		if ln.CatalogItemID == id {
			return true
		}
	}
	return false
}

func orderLineFromCatalogItem(it CatalogItem, qty int) OrderLineState {
	if qty < 1 {
		qty = 1
	}
	unit := it.SellUnit
	if unit == "" {
		unit = "pcs"
	}
	return OrderLineState{
		CatalogItemID: it.ID,
		ExternalCode:  it.ExternalCode,
		ProductName:   it.Name,
		Qty:           qty,
		UnitPrice:     it.SellPrice,
		SellUnit:      unit,
	}
}

func syncOrderStateFromItems(st *OrderState) {
	if st == nil {
		return
	}
	if len(st.Items) == 0 {
		st.CatalogItemID = ""
		st.ExternalCode = ""
		st.ProductName = ""
		st.Qty = 0
		st.UnitPrice = 0
		st.SellUnit = ""
		return
	}
	applyLineToOrderState(st, st.Items[0])
}
