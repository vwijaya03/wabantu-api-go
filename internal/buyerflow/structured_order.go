package buyerflow

import (
	"regexp"
	"strings"
)

var structuredOrderNumberedLineRe = regexp.MustCompile(`(?m)^\s*\d+\.\s*(.+)$`)

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
	if IsInlineMultiOrderMessage(userText) {
		segments := splitInlineOrderSegments(userText)
		qtySegs := 0
		for _, seg := range segments {
			if mentionsOrderQty(seg) {
				qtySegs++
			}
		}
		return qtySegs >= 2 || (qtySegs >= 1 && len(segments) >= 2)
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
	for _, h := range []string{
		"barang yang dibeli", "detail pesanan", "pesanan saya", "order saya",
		"alamat pengiriman", "data penerima", "nama penerima",
		"nama:", "nama :", "hp:", "hp :",
	} {
		if strings.Contains(lower, h) {
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
