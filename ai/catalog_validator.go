package ai

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
)

var idrAmountRe = regexp.MustCompile(`(?i)rp\s*\.?\s*([\d.,]+)`)

// CatalogValidationResult — hasil gate validator (poin 4).
type CatalogValidationResult struct {
	OK     bool
	Reason string
}

// validateReplyAgainstCatalog memastikan harga di balasan LLM cocok dengan DB.
//
// facts is the allow-list built by groundingFacts: catalog prices and qty x price, the buyer's
// current cart lines and subtotal, and amounts quoted in the KB. A reply whose every Rp amount is
// in that list is grounded and is accepted before any catalog-matching price rejection; this is
// what lets "Total: Rp875.000" (the cart) and "2x Salmon = Rp150.000" through. The price
// matching below still runs for replies that quote something else.
func validateReplyAgainstCatalog(reply string, catalog []dbCatalogItem, facts salesbrain.Facts) CatalogValidationResult {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return CatalogValidationResult{OK: false, Reason: "empty_reply"}
	}
	if len(catalog) == 0 {
		return CatalogValidationResult{OK: true}
	}

	amounts := extractIDRAmounts(reply)
	if len(amounts) == 0 {
		return CatalogValidationResult{OK: true}
	}
	if replyAmountsInFacts(reply, facts) {
		return CatalogValidationResult{OK: true}
	}
	mentioned := findMentionedCatalogItems(reply, catalog)
	if len(mentioned) == 0 {
		return CatalogValidationResult{OK: false, Reason: "price_without_catalog_match"}
	}

	allowed := buildAllowedCatalogPrices(mentioned)
	for _, amt := range amounts {
		if !priceAllowedInCatalog(amt, allowed) {
			return CatalogValidationResult{OK: false, Reason: "price_mismatch"}
		}
	}
	return CatalogValidationResult{OK: true}
}

func extractIDRAmounts(text string) []float64 {
	matches := idrAmountRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var out []float64
	seen := make(map[int]struct{})
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		raw := strings.ReplaceAll(m[1], ".", "")
		raw = strings.ReplaceAll(raw, ",", "")
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 {
			continue
		}
		v := parsed
		key := int(math.Round(v))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func findMentionedCatalogItems(reply string, catalog []dbCatalogItem) []*dbCatalogItem {
	replyLower := strings.ToLower(reply)
	var out []*dbCatalogItem
	for i := range catalog {
		it := &catalog[i]
		nameLower := strings.ToLower(it.Name)
		short := strings.ToLower(shortDisplayName(it.Name))
		if nameLower != "" && strings.Contains(replyLower, nameLower) {
			out = append(out, it)
			continue
		}
		if short != "" && len(short) >= 8 && strings.Contains(replyLower, short) {
			out = append(out, it)
			continue
		}
		score := overlapScore(tokenize(replyLower), tokenize(nameLower))
		if score >= 0.25 {
			out = append(out, it)
		}
	}
	return out
}

func buildAllowedCatalogPrices(items []*dbCatalogItem) []float64 {
	seen := make(map[int]struct{})
	var allowed []float64
	add := func(v float64) {
		if v <= 0 {
			return
		}
		key := int(math.Round(v))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		allowed = append(allowed, v)
	}
	for _, it := range items {
		if it == nil {
			continue
		}
		info := parseCatalogPriceInfo(it)
		add(info.ListPrice)
		if info.IsPackListing {
			add(info.PerPiecePrice)
			add(info.ListPrice * float64(info.PackCount)) // legacy wrong math still in history
		}
	}
	return allowed
}

func priceAllowedInCatalog(amount float64, allowed []float64) bool {
	if amount <= 0 || len(allowed) == 0 {
		return false
	}
	for _, a := range allowed {
		if math.Abs(amount-a) <= 1 || math.Abs(amount-a) <= a*0.02 {
			return true
		}
	}
	return false
}

type catalogReplyFunc func(userText string, profile *dbBusinessProfile, catalog []dbCatalogItem, history []dbMessage) (string, bool)

func isGenericCatalogNotFound(reply string) bool {
	lower := strings.ToLower(strings.TrimSpace(reply))
	return strings.Contains(lower, "belum menemukan data") && strings.Contains(lower, "katalog")
}

// replyAmountsInFacts reports whether every Rp amount in reply is in the facts allow-list
// (salesbrain.Validate with only its amount violations kept). A reply without amounts passes.
func replyAmountsInFacts(reply string, facts salesbrain.Facts) bool {
	for _, v := range salesbrain.Validate(salesbrain.Decision{Reply: reply}, facts, "") {
		if v.Code == salesbrain.VAmount {
			return false
		}
	}
	return true
}

// groundingFacts builds the amount allow-list for grounding: catalog rows (price and qty x price),
// KB answers, and the buyer's current cart (lines and their sum). cart may be nil.
func groundingFacts(catalog []dbCatalogItem, kb []dbKBEntry, cart *orderState) salesbrain.Facts {
	in := salesbrain.FactsInput{}
	for i, c := range catalog {
		in.Catalog = append(in.Catalog, salesbrain.CatalogFact{
			No: i + 1, Name: c.Name, Price: int64(math.Round(c.SellPrice)), Code: c.ExternalCode,
		})
	}
	for _, e := range kb {
		cat := ""
		if e.Category != nil {
			cat = *e.Category
		}
		in.KB = append(in.KB, salesbrain.KBFact{Question: e.Question, Answer: e.Answer, Category: cat})
	}
	in.Cart, in.CartSubtotal = cartFactLines(cart)
	return salesbrain.BuildFacts(in)
}

// cartFactLines turns the Redis cart into salesbrain order lines and their subtotal. A cart with
// no Items keeps its single product in the flat fields.
func cartFactLines(cart *orderState) ([]salesbrain.OrderLine, int64) {
	if cart == nil {
		return nil, 0
	}
	var lines []salesbrain.OrderLine
	if len(cart.Items) > 0 {
		for _, it := range cart.Items {
			lines = append(lines, salesbrain.OrderLine{Name: it.ProductName, Qty: it.Qty, UnitPrice: int64(math.Round(it.UnitPrice))})
		}
	} else if strings.TrimSpace(cart.ProductName) != "" || cart.UnitPrice > 0 {
		lines = append(lines, salesbrain.OrderLine{Name: cart.ProductName, Qty: cart.Qty, UnitPrice: int64(math.Round(cart.UnitPrice))})
	}
	var subtotal int64
	for _, l := range lines {
		subtotal += l.UnitPrice * int64(l.Qty)
	}
	return lines, subtotal
}

// groundLLMReply — validator + fallback catalog_db jika LLM halu harga/produk.
// catalogFn may use vector hybrid matching when provided; defaults to lexical catalog_db.
//
// A reply that cannot be grounded is never replaced by a hard-coded Indonesian sentence: the
// fallback is the KB payment answer (payment questions) or salesbrain.TplCheckWithTeam in the
// buyer's language (lang; empty means Indonesian). reason is non-empty whenever grounded is true.
func groundLLMReply(
	reply string,
	userText string,
	lang salesbrain.Lang,
	profile *dbBusinessProfile,
	catalog []dbCatalogItem,
	kb []dbKBEntry,
	cart *orderState,
	history []dbMessage,
	catalogFn catalogReplyFunc,
) (final string, grounded bool, reason string) {
	if lang == "" {
		lang = salesbrain.LangID
	}
	v := validateReplyAgainstCatalog(reply, catalog, groundingFacts(catalog, kb, cart))
	genericMiss := isGenericCatalogNotFound(reply)
	if v.OK && !genericMiss {
		return reply, false, ""
	}
	reason = v.Reason
	if genericMiss {
		reason = "generic_catalog_not_found"
	}
	if reason == "" {
		reason = "ungrounded_reply"
	}
	if catalogFn == nil {
		catalogFn = replyFromBusinessCatalog
	}
	if catReply, ok := catalogFn(userText, profile, catalog, history); ok {
		return catReply, true, reason
	}
	if IsCatalogExclusionQuestion(userText) || IsCatalogBrowsingIntent(userText) || isGeneralStoreCatalogQuestion(userText) {
		formal := profile != nil && strOrEmpty(profile.Tone) == "formal"
		bizName := ""
		if profile != nil {
			bizName = strings.TrimSpace(profile.BusinessName)
		}
		bfCatalog := make([]bf.CatalogItem, len(catalog))
		for i, c := range catalog {
			bfCatalog[i] = bf.CatalogItem{ID: c.ID, Name: c.Name, SellPrice: c.SellPrice, SellUnit: c.SellUnit, ExternalCode: c.ExternalCode, Description: c.Description}
		}
		var bfProfile *bf.BusinessProfile
		if profile != nil {
			bfProfile = &bf.BusinessProfile{BusinessName: profile.BusinessName, Tone: profile.Tone}
		}
		filtered := bf.BuildCatalogListReplyFiltered(formal, bizName, bfCatalog, bfProfile, userText)
		if strings.TrimSpace(filtered) != "" {
			return filtered, true, reason
		}
	}
	if IsPaymentQuestion(userText) {
		if ans, ok := tryPaymentFAQAnswer(userText, kb); ok && strings.TrimSpace(ans) != "" {
			return ans, true, "payment_kb"
		}
		return salesbrain.T(salesbrain.TplCheckWithTeam, lang), true, "payment_no_kb"
	}
	return salesbrain.T(salesbrain.TplCheckWithTeam, lang), true, reason
}
