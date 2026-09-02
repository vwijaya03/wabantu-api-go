package ai

import (
	"fmt"
	"regexp"
	"strings"

	bf "encore.app/wabantu/internal/buyerflow"
)

var structuredOrderNumberedLineRe = regexp.MustCompile(`(?m)^\s*\d+\.\s*(.+)$`)

// IsStructuredOrderList — see buyerflow_bridge (internal/buyerflow).

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
	if unnumbered := extractUnnumberedOrderLines(userText); len(unnumbered) > 0 {
		return unnumbered
	}
	if bf.IsInlineMultiOrderMessage(userText) {
		return bf.SplitInlineOrderSegments(userText)
	}
	return nil
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
	line := bf.ParseStructuredOrderLine(raw, toBFCatalog(catalog))
	return orderLineState(line)
}

func toBFCatalog(catalog []dbCatalogItem) []bf.CatalogItem {
	out := make([]bf.CatalogItem, len(catalog))
	for i, c := range catalog {
		out[i] = bf.CatalogItem{
			ID: c.ID, ExternalCode: c.ExternalCode, Name: c.Name,
			SellPrice: c.SellPrice, SellUnit: c.SellUnit,
		}
	}
	return out
}

func orderStateFromStructuredLines(lines []orderLineState) orderState {
	st := orderState{Items: lines, Step: "ask_recipient"}
	if len(lines) == 1 {
		bf.ApplyLineToOrderState(&st, lines[0])
	}
	return st
}

func orderStateFromLine(line orderLineState) orderState {
	st := orderState{}
	bf.ApplyLineToOrderState(&st, line)
	return st
}

func lineVariantComplete(line orderLineState) bool {
	it := &dbCatalogItem{Name: line.ProductName, ExternalCode: line.ExternalCode}
	if !catalogItemNeedsVariant(it) {
		return true
	}
	return line.Size != "" || line.Color != ""
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
	out, reply, blocked := bf.GuardStructuredOrderStock(st, toBFCatalog(catalog), formal)
	return out, reply, blocked
}

func guardOrderStateQty(st orderState, catalog []dbCatalogItem, formal bool, qtyStep string) (orderState, string, bool) {
	if st.HasMultiItems() {
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
	if !st.StructuredLinesReady() {
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
