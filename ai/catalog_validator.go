package ai

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	bf "encore.app/wabantu/internal/buyerflow"
)

var idrAmountRe = regexp.MustCompile(`(?i)rp\s*\.?\s*([\d.,]+)`)

// CatalogValidationResult — hasil gate validator (poin 4).
type CatalogValidationResult struct {
	OK     bool
	Reason string
}

// validateReplyAgainstCatalog memastikan harga di balasan LLM cocok dengan DB.
func validateReplyAgainstCatalog(reply string, catalog []dbCatalogItem) CatalogValidationResult {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return CatalogValidationResult{OK: false, Reason: "empty_reply"}
	}
	if len(catalog) == 0 {
		return CatalogValidationResult{OK: true}
	}

	amounts := extractIDRAmounts(reply)
	mentioned := findMentionedCatalogItems(reply, catalog)
	if len(amounts) == 0 {
		return CatalogValidationResult{OK: true}
	}
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

// groundLLMReply — validator + fallback catalog_db jika LLM halu harga/produk.
// catalogFn may use vector hybrid matching when provided; defaults to lexical catalog_db.
func groundLLMReply(
	reply string,
	userText string,
	profile *dbBusinessProfile,
	catalog []dbCatalogItem,
	history []dbMessage,
	catalogFn catalogReplyFunc,
) (final string, grounded bool, reason string) {
	v := validateReplyAgainstCatalog(reply, catalog)
	genericMiss := isGenericCatalogNotFound(reply)
	if v.OK && !genericMiss {
		return reply, false, ""
	}
	reason = v.Reason
	if genericMiss {
		reason = "generic_catalog_not_found"
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
			bfCatalog[i] = bf.CatalogItem{ID: c.ID, Name: c.Name, SellPrice: c.SellPrice, SellUnit: c.SellUnit, ExternalCode: c.ExternalCode}
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
		return "Maaf kak, info rekening pembayaran belum tersedia otomatis. Tim CS akan bantu ya 🙏", true, "payment_no_kb"
	}
	return "Saya belum menemukan data tersebut di katalog saat ini.", true, reason
}
