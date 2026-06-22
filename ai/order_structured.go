package ai

import (
	"regexp"
	"strings"
)

var structuredOrderNumberedLineRe = regexp.MustCompile(`(?m)^\s*\d+\.\s*(.+)$`)

// IsStructuredOrderList — pesan berisi daftar barang bernomor atau header order terstruktur.
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
	return false
}
