package buyerflow

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	adaPesananPhraseRe = regexp.MustCompile(`(?i)\bada pesanan\b`)
	adaWordRe          = regexp.MustCompile(`(?i)\bada\b`)
)

func containsAdaPesananPhrase(text string) bool {
	return adaPesananPhraseRe.MatchString(text)
}

func containsAdaWord(text string) bool {
	return adaWordRe.MatchString(text)
}

func isWholeOrderCancelPhrase(text string) bool {
	for _, p := range []string{
		"batalkan pesanan", "batal pesanan", "cancel order", "batalkan order",
		"cancel pesanan", "batal order", "batal pesan",
	} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

var skuCancelFiller = map[string]struct{}{
	"ya": {}, "deh": {}, "dong": {}, "kak": {}, "pesanan": {}, "order": {},
	"semua": {}, "bisa": {}, "dan": {}, "nambah": {}, "tambah": {}, "nya": {},
	"mau": {}, "saya": {}, "lanjutkan": {}, "pada": {}, "wb": {},
	"batal": {}, "batalkan": {}, "batalin": {}, "cancel": {},
}

func skuCancelRemainder(text string) bool {
	rem := orderRefInMessageRe.ReplaceAllString(text, " ")
	for _, p := range []string{
		"pada pesanan", "saya batalkan", "mau batalkan", "mau saya batalkan",
		"ga jadi beli", "gak jadi beli", "nggak jadi beli", "tidak jadi beli",
		"nya saya mau", "nya mau", "saya mau", "lanjutkan",
		"batalkan", "batalin", "cancel", "ga jadi", "gak jadi", "nggak jadi", "tidak jadi",
	} {
		rem = strings.ReplaceAll(rem, p, " ")
	}
	rem = strings.NewReplacer("?", " ", "!", " ", ".", " ", ",", " ", "-", " ").Replace(rem)
	for _, tok := range strings.Fields(rem) {
		if _, skip := skuCancelFiller[tok]; skip {
			continue
		}
		if len(tok) >= 3 {
			return true
		}
	}
	return false
}

// ApplyDraftLineMutations hydrates-in-place: remove SKU lines, merge adds, bump qty.
func ApplyDraftLineMutations(st *OrderState, userText string, catalog []CatalogItem) bool {
	if st == nil || strings.TrimSpace(userText) == "" || len(catalog) == 0 {
		return false
	}
	ensureMultiItemsFromSingle(st)
	before := cartFingerprint(*st)

	rejectText, wantText := splitCartCorrectionSpans(userText)
	reject := catalogItemsIdentifiedInText(rejectText, catalog)
	if len(reject) == 0 {
		reject = cartItemsMatchingBrandOrText(*st, rejectText, catalog)
	}
	skip := catalogItemIDs(reject)
	if len(skip) > 0 {
		st.Items = filterOrderLinesExcluding(st.Items, skip)
	}

	addSrc := strings.TrimSpace(wantText)
	if addSrc == "" && (IsAddItemToOrderMessage(userText) || IsOrderAmendMessage(userText)) {
		addSrc = userText
	}
	if addSrc != "" {
		_, added := parseAppendSegments(addSrc, catalog, nil)
		if len(added) == 0 {
			added = appendLinesFromIdentifiedCatalog(addSrc, catalog, nil)
		}
		filtered := added[:0]
		for _, ln := range added {
			if _, drop := skip[ln.CatalogItemID]; drop {
				continue
			}
			filtered = append(filtered, ln)
		}
		if len(filtered) > 0 {
			st.Items = MergeOrderLines(st.Items, filtered)
		}
	}
	syncOrderStateFromItems(st)
	if st.Step == "" || st.Step == "ask_product" {
		if len(st.Items) > 0 {
			st.Step = "ask_recipient"
		}
	}
	return cartFingerprint(*st) != before
}

func cartFingerprint(st OrderState) string {
	var b strings.Builder
	b.Grow(len(st.Items) * 24)
	for _, ln := range st.Items {
		b.WriteString(ln.CatalogItemID)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(ln.Qty))
		b.WriteByte('|')
	}
	return b.String()
}
