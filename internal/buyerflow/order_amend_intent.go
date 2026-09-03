package buyerflow

import "strings"

// IsOrderAmendMessage — buyer ingin menambah/menggabungkan item ke draft order.
func IsOrderAmendMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"ga masuk", "gak masuk", "belum masuk", "tidak masuk", "blm masuk",
		"tambah ke pesanan", "tambahin ke pesanan", "masukin ke pesanan",
		"jadikan 1", "jadiin 1", "gabung", "gabungkan", "satu pesanan",
		"pesanan sebelumnya", "order sebelumnya", "order td", "pesanan td",
		"belum ke order", "belum masuk order", "belum masuk pesanan",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// ExtractAmendLinesFromText parses catalog lines from amend-related text.
func ExtractAmendLinesFromText(userText string, catalog []CatalogItem, vctx *CatalogVectorContext) []OrderLineState {
	_, fromInline := parseAppendSegments(userText, catalog, vctx)
	if len(fromInline) > 0 {
		return fromInline
	}
	line := parseStructuredOrderLine(userText, catalog, vctx)
	if line.CatalogItemID != "" {
		return []OrderLineState{line}
	}
	return nil
}

// ExtractAmendLinesFromHistory finds products mentioned in recent buyer messages not yet in existingIDs.
func ExtractAmendLinesFromHistory(history []Message, catalog []CatalogItem, existingIDs map[string]bool, vctx *CatalogVectorContext) []OrderLineState {
	var out []OrderLineState
	seen := make(map[string]bool)
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Author != "contact" && m.Direction != "in" {
			continue
		}
		body := strings.TrimSpace(m.Body)
		if body == "" {
			continue
		}
		_, fromInline := parseAppendSegments(body, catalog, vctx)
		candidates := fromInline
		if len(candidates) == 0 {
			if line := parseStructuredOrderLine(body, catalog, vctx); line.CatalogItemID != "" {
				candidates = []OrderLineState{line}
			}
		}
		for _, ln := range candidates {
			if ln.CatalogItemID == "" || existingIDs[ln.CatalogItemID] || seen[ln.CatalogItemID] {
				continue
			}
			out = append(out, ln)
			seen[ln.CatalogItemID] = true
		}
	}
	return out
}
