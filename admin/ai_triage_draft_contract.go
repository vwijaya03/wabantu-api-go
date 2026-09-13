package admin

import (
	"regexp"
	"strings"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/shared/triageincident"
)

var (
	trailingQtyAfterUnit = regexp.MustCompile(`(?i)(gram|gr|kg|pcs|ml|buah|pack|g)\s+(\d+)\s*$`)
	orderVerbPrefix      = regexp.MustCompile(`(?i)^(hah+\s*[?.!]*)?\s*(nambah|tambahkan|tambah|beli|mau|order)\s+`)
)

func buildDraftContract(channel, path, category, userText, replyText string) ai.BehaviorContract {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "whatsapp"
	}
	c := ai.BehaviorContract{
		Version: 1,
		Lane:    inferLane(path, category, userText),
		Channel: channel,
		Assertions: ai.BehaviorAssertions{
			WantPath: strings.TrimSpace(path),
		},
	}
	lowUser := strings.ToLower(userText)
	if strings.Contains(lowUser, "oatlife") && !strings.Contains(lowUser, "white") {
		c.Assertions.NeedCustomerInput = true
		c.Clarification = "Jangan menebak varian; tanya pelanggan jika SKU ambigu."
		return c
	}

	miss := catalogMissPhrases(replyText)
	cart := extractCartHints(userText)
	orderLike := isOrderLikeUserText(userText) || len(cart) > 0

	if orderLike && len(miss) > 0 {
		c.Lane = triageincident.LaneBuyerflow
		c.Assertions.WantPath = buyerflow.PathOrderFlow
		c.Assertions.CartInclude = cart
		c.Assertions.ReplyExcludes = miss
		c.Clarification = "SKU ada di katalog. Tambah ke keranjang; jangan bilang data tidak ditemukan."
		return c
	}
	if len(miss) > 0 {
		c.Lane = triageincident.LaneGroundedContent
		if c.Assertions.WantPath == buyerflow.PathLLM || c.Assertions.WantPath == buyerflow.PathLLMGrounded || c.Assertions.WantPath == buyerflow.PathLLMTools {
			c.Assertions.WantPath = buyerflow.PathCatalogDB
		}
		c.Assertions.ReplyExcludes = miss
		c.Assertions.ForbiddenClaims = []string{"produk tidak ada di katalog"}
		if hints := productNameHints(userText); len(hints) > 0 {
			c.Assertions.ReplyContains = hints
		}
		c.Clarification = "Katalog punya SKU yang disebut pembeli; balasan harus menunjuk produk itu."
		return c
	}
	if orderLike && len(cart) > 0 {
		c.Lane = triageincident.LaneBuyerflow
		if c.Assertions.WantPath == buyerflow.PathLLM || c.Assertions.WantPath == buyerflow.PathLLMGrounded || c.Assertions.WantPath == buyerflow.PathLLMTools {
			c.Assertions.WantPath = buyerflow.PathOrderFlow
		}
		c.Assertions.CartInclude = cart
	}
	return c
}

func inferLane(path, category, userText string) string {
	if isOrderLikeUserText(userText) {
		return triageincident.LaneBuyerflow
	}
	switch path {
	case buyerflow.PathOrderFlow, buyerflow.PathOrderStatus, buyerflow.PathConsulting:
		return triageincident.LaneBuyerflow
	default:
		if category == "wrong_answer" && isOrderLikeUserText(userText) {
			return triageincident.LaneBuyerflow
		}
		return triageincident.LaneGroundedContent
	}
}

func isOrderLikeUserText(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if buyerflow.IsAddItemToOrderMessage(userText) || buyerflow.IsInlineMultiOrderMessage(userText) {
		return true
	}
	if buyerflow.HasPurchaseIntent(userText) {
		return true
	}
	for _, w := range []string{"nambah", "tambah", "beli ", "order "} {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func catalogMissPhrases(reply string) []string {
	low := strings.ToLower(reply)
	needles := []string{"belum menemukan", "tidak menemukan", "tidak ada di katalog", "tidak tersedia di katalog"}
	var out []string
	for _, n := range needles {
		if strings.Contains(low, n) {
			out = append(out, n)
		}
	}
	return out
}

func extractCartHints(userText string) []ai.CartAssertion {
	parts := strings.Split(strings.ReplaceAll(userText, " dan ", ","), ",")
	var out []ai.CartAssertion
	seen := map[string]bool{}
	for _, raw := range parts {
		name, qty, ok := parseCartSegment(raw)
		if !ok {
			continue
		}
		key := name + ":" + strings.TrimSpace(strings.ToLower(name))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ai.CartAssertion{NameContains: name, Qty: qty})
	}
	return out
}

func parseCartSegment(seg string) (string, int, bool) {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return "", 0, false
	}
	seg = orderVerbPrefix.ReplaceAllString(seg, "")
	seg = strings.TrimSpace(seg)
	if seg == "" || strings.Contains(seg, "?") {
		return "", 0, false
	}
	m := trailingQtyAfterUnit.FindStringSubmatch(seg)
	if m == nil {
		return "", 0, false
	}
	qty := 1
	if n := m[2]; n != "" {
		qty = atoiDefault(n, 1)
		if qty < 1 {
			qty = 1
		}
	}
	name := strings.TrimSpace(seg[:len(seg)-len(m[0])])
	name += " " + m[1]
	name = strings.ToLower(strings.Join(strings.Fields(name), " "))
	if name == "" || !hasLetter(name) {
		return "", 0, false
	}
	return name, qty, true
}

func productNameHints(userText string) []string {
	var out []string
	for _, c := range extractCartHints(userText) {
		if c.NameContains != "" {
			out = append(out, c.NameContains)
		}
	}
	return out
}

func atoiDefault(s string, fallback int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func hasLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}
