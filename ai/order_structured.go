package ai

import (
	"fmt"
	"regexp"
	"strings"
)

var structuredOrderNumberedLineRe = regexp.MustCompile(`(?m)^\s*\d+\.\s*(.+)$`)

// orderLineState — satu baris dalam pesanan multi-item (Redis JSON).
type orderLineState struct {
	CatalogItemID string  `json:"catalogItemId,omitempty"`
	ExternalCode  string  `json:"externalCode,omitempty"`
	ProductName   string  `json:"productName,omitempty"`
	Size          string  `json:"size,omitempty"`
	Color         string  `json:"color,omitempty"`
	Qty           int     `json:"qty,omitempty"`
	UnitPrice     float64 `json:"unitPrice,omitempty"`
	SellUnit      string  `json:"sellUnit,omitempty"`
	WarehouseID   string  `json:"warehouseId,omitempty"`
}

// IsStructuredOrderList — pesan berisi daftar barang bernomor, multi-baris tanpa nomor, atau header order terstruktur.
func IsStructuredOrderList(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsExplicitNewOrderStart(userText) && structuredOrderNumberedLineRe.MatchString(userText) {
		return true
	}
	if strings.Contains(text, "barang yang dibeli") && structuredOrderNumberedLineRe.MatchString(userText) {
		return true
	}
	if structuredOrderNumberedLineRe.MatchString(userText) && mentionsOrderQty(userText) {
		return true
	}
	if countOrderCandidateLines(userText) >= 2 {
		return true
	}
	return false
}

func isOrderListHeaderLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return true
	}
	if mentionsOrderQty(line) {
		return false
	}
	for _, p := range []string{
		"mau order", "mau buat", "buat pesanan", "order baru", "pesanan baru",
		"barang yang dibeli", "order ini", "pesan ini", "mau pesan", "pesan baru",
	} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func countOrderCandidateLines(userText string) int {
	n := 0
	for _, line := range strings.Split(userText, "\n") {
		line = strings.TrimSpace(line)
		if isOrderListHeaderLine(line) {
			continue
		}
		if mentionsOrderQty(line) {
			n++
		}
	}
	return n
}

func extractUnnumberedOrderLines(userText string) []string {
	var out []string
	for _, line := range strings.Split(userText, "\n") {
		line = strings.TrimSpace(line)
		if isOrderListHeaderLine(line) {
			continue
		}
		if mentionsOrderQty(line) {
			out = append(out, line)
		}
	}
	return out
}

func extractStructuredOrderLines(userText string) []string {
	if numbered := extractNumberedOrderLines(userText); len(numbered) > 0 {
		return numbered
	}
	return extractUnnumberedOrderLines(userText)
}

func extractNumberedOrderLines(userText string) []string {
	matches := structuredOrderNumberedLineRe.FindAllStringSubmatch(userText, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			line := strings.TrimSpace(m[1])
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

type structuredOrderParseResult struct {
	Lines     []orderLineState
	Unmatched []string
}

func parseStructuredOrderLines(userText string, catalog []dbCatalogItem) structuredOrderParseResult {
	var res structuredOrderParseResult
	if !IsStructuredOrderList(userText) {
		return res
	}
	rawLines := extractStructuredOrderLines(userText)
	if len(rawLines) == 0 {
		return res
	}
	for _, raw := range rawLines {
		line := parseSingleStructuredLine(raw, catalog)
		if line.CatalogItemID == "" && line.ProductName == "" {
			res.Unmatched = append(res.Unmatched, raw)
			continue
		}
		res.Lines = append(res.Lines, line)
	}
	return res
}

func parseSingleStructuredLine(raw string, catalog []dbCatalogItem) orderLineState {
	var line orderLineState
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
		// Coba tanpa suffix ukuran/qty
		cleaned := orderSizeLineRe.ReplaceAllString(text, "")
		cleaned = orderQtyLusinRe.ReplaceAllString(cleaned, "")
		cleaned = orderQtyWithUnitRe.ReplaceAllString(cleaned, "")
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

func orderStateFromStructuredLines(lines []orderLineState) orderState {
	st := orderState{Items: lines, Step: "ask_recipient"}
	if len(lines) == 1 {
		applyLineToOrderState(&st, lines[0])
	}
	return st
}

func applyLineToOrderState(st *orderState, line orderLineState) {
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

func (st orderState) hasMultiItems() bool {
	return len(st.Items) > 0
}

func lineVariantComplete(line orderLineState) bool {
	it := &dbCatalogItem{Name: line.ProductName, ExternalCode: line.ExternalCode}
	if !catalogItemNeedsVariant(it) {
		return true
	}
	return line.Size != "" || line.Color != ""
}

func (st orderState) structuredLinesReady() bool {
	if !st.hasMultiItems() {
		return false
	}
	for _, ln := range st.Items {
		if strings.TrimSpace(ln.CatalogItemID) == "" || ln.Qty < 1 || !lineVariantComplete(ln) {
			return false
		}
	}
	return true
}

func (st orderState) readyToPersist() bool {
	if st.hasMultiItems() {
		if !st.structuredLinesReady() {
			return false
		}
	} else {
		st = normalizeOrderState(st)
		if !st.productComplete() || strings.TrimSpace(st.CatalogItemID) == "" || !st.variantComplete() || st.Qty < 1 {
			return false
		}
	}
	if strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "" {
		return false
	}
	return st.shippingComplete()
}

func orderStateFromLine(line orderLineState) orderState {
	st := orderState{}
	applyLineToOrderState(&st, line)
	return st
}

func structuredOrderUnmatchedReply(formal bool, unmatched []string) string {
	var b strings.Builder
	if formal {
		b.WriteString("Mohon maaf, beberapa baris belum cocok dengan katalog kami:\n")
	} else {
		b.WriteString("Maaf kak, beberapa baris belum ketemu di katalog:\n")
	}
	for i, u := range unmatched {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, u))
	}
	if formal {
		b.WriteString("\nSilakan sebut nama produk yang ada di katalog ya.")
	} else {
		b.WriteString("\nCoba sebut nama produk yang ada di katalog ya kak.")
	}
	return strings.TrimSpace(b.String())
}

func guardStructuredOrderStock(st orderState, catalog []dbCatalogItem, formal bool) (orderState, string, bool) {
	if !st.hasMultiItems() {
		return st, "", false
	}
	for i := range st.Items {
		lineSt := orderStateFromLine(st.Items[i])
		lineSt, reply, blocked := guardOrderQtyStep(lineSt, catalog, formal, "ask_qty")
		if blocked {
			prefix := fmt.Sprintf("Baris %d (%s): ", i+1, shortDisplayName(st.Items[i].ProductName))
			return st, prefix + reply, true
		}
		st.Items[i].WarehouseID = lineSt.WarehouseID
	}
	return st, "", false
}

func guardOrderStateQty(st orderState, catalog []dbCatalogItem, formal bool, qtyStep string) (orderState, string, bool) {
	if st.hasMultiItems() {
		return guardStructuredOrderStock(st, catalog, formal)
	}
	return guardOrderQtyStep(st, catalog, formal, qtyStep)
}

// structuredOrderFlowOutcome — hasil evaluasi pesan multi-baris untuk order_flow.
type structuredOrderFlowOutcome struct {
	Matched    bool
	Lines      []orderLineState
	Unmatched  []string
	State      orderState
	Blocked    bool
	BlockReply string
	NeedVariant bool
}

func evaluateStructuredOrder(userText string, catalog []dbCatalogItem, formal bool) structuredOrderFlowOutcome {
	var out structuredOrderFlowOutcome
	if !IsStructuredOrderList(userText) {
		return out
	}
	out.Matched = true
	parsed := parseStructuredOrderLines(userText, catalog)
	out.Unmatched = parsed.Unmatched
	out.Lines = parsed.Lines
	if len(parsed.Lines) == 0 {
		return out
	}
	st := orderStateFromStructuredLines(parsed.Lines)
	if !st.structuredLinesReady() {
		st.Step = "ask_variant"
		out.State = st
		out.NeedVariant = true
		return out
	}
	st, reply, blocked := guardStructuredOrderStock(st, catalog, formal)
	if blocked {
		out.Blocked = true
		out.BlockReply = reply
		return out
	}
	st.Step = "ask_recipient"
	out.State = st
	return out
}
