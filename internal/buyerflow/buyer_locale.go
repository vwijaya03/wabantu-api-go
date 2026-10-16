package buyerflow

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	LangID = "id"
	LangEN = "en"
)

var englishStrongPhrases = []string{
	"hi i want", "hello i want", "i want to shop", "want to shop",
	"how long is shipping", "how long does shipping", "how many days until",
	"when will it arrive", "when will my order", "where is my order",
	"i want to pay", "how do i pay", "talk to a human", "speak to a human",
	"connect me to", "can i buy for someone else", "buy for someone else",
	"order for someone else",
	"cancel order", "cancel my order", "please cancel",
}

var englishFunctionWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "to": {}, "for": {}, "of": {},
	"you": {}, "your": {}, "my": {}, "me": {}, "i": {}, "we": {}, "our": {},
	"how": {}, "what": {}, "where": {}, "when": {}, "why": {}, "which": {},
	"can": {}, "could": {}, "would": {}, "please": {}, "want": {}, "need": {},
	"is": {}, "are": {}, "do": {}, "does": {}, "did": {}, "will": {}, "have": {},
	"this": {}, "that": {}, "with": {}, "about": {}, "from": {},
	"shop": {}, "shopping": {}, "shipping": {}, "delivery": {}, "order": {},
	"pay": {}, "payment": {}, "human": {}, "talk": {}, "speak": {}, "help": {},
}

var indonesianStrongWords = map[string]struct{}{
	"kak": {}, "kakak": {}, "dong": {}, "gak": {}, "ga": {}, "nggak": {}, "ngga": {},
	"yg": {}, "yah": {}, "aja": {}, "deh": {}, "nih": {}, "sih": {}, "banget": {},
	"mau": {}, "beli": {}, "pesan": {}, "ongkir": {}, "berapa": {}, "gimana": {},
	"bagaimana": {}, "kapan": {}, "dimana": {}, "bisa": {}, "tolong": {}, "terima": {},
	"makasih": {}, "siap": {}, "punten": {}, "monggo": {},
}

var englishWordRe = regexp.MustCompile(`[a-z]+`)

var explicitEnglishSwitchPhrases = []string{
	"in english", "please use english", "use english", "english please",
	"speak in english", "talk in english", "can i speak in english", "can i speak english",
	"cant read indonesian", "can't read indonesian", "cannot read indonesian",
	"i cant read indonesian", "i can't read indonesian",
	"pakai bahasa inggris", "bahasa inggris", "inggris saja",
}
var explicitIndonesianSwitchPhrases = []string{
	"bahasa indonesia", "in indonesian", "please use indonesian", "use indonesian",
	"pakai bahasa indonesia", "indonesia saja",
}

var savedBuyerLangStack []string

// PushSavedBuyerLang scopes template/reply language for the current turn (simulator + autoreply).
func PushSavedBuyerLang(saved string) func() {
	savedBuyerLangStack = append(savedBuyerLangStack, normalizeLang(saved))
	return func() {
		if len(savedBuyerLangStack) > 0 {
			savedBuyerLangStack = savedBuyerLangStack[:len(savedBuyerLangStack)-1]
		}
	}
}

// SetSavedBuyerLang updates the innermost saved language (after persisting preference mid-turn).
func SetSavedBuyerLang(saved string) {
	if len(savedBuyerLangStack) == 0 {
		savedBuyerLangStack = append(savedBuyerLangStack, normalizeLang(saved))
		return
	}
	savedBuyerLangStack[len(savedBuyerLangStack)-1] = normalizeLang(saved)
}

// CurrentSavedBuyerLang returns the active saved buyer language for this turn.
func CurrentSavedBuyerLang() string {
	if len(savedBuyerLangStack) == 0 {
		return ""
	}
	return savedBuyerLangStack[len(savedBuyerLangStack)-1]
}

func normalizeLang(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LangEN, "english", "inggris":
		return LangEN
	case LangID, "indonesian", "indonesia":
		return LangID
	default:
		return strings.TrimSpace(s)
	}
}

// IsEnglishBuyerMessage — mostly English function words / shopping phrases; not one borrowed token ("ready").
func IsEnglishBuyerMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, p := range englishStrongPhrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	words := englishWordRe.FindAllString(text, -1)
	if len(words) == 0 {
		return false
	}
	eng, idn := 0, 0
	for _, w := range words {
		if _, ok := englishFunctionWords[w]; ok {
			eng++
		}
		if _, ok := indonesianStrongWords[w]; ok {
			idn++
		}
	}
	if eng >= 2 && eng > idn {
		return true
	}
	if eng >= 3 && idn == 0 {
		return true
	}
	return false
}

func isClearlyIndonesianBuyerMessage(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	if IsEnglishBuyerMessage(userText) {
		return false
	}
	words := englishWordRe.FindAllString(text, -1)
	idn := 0
	for _, w := range words {
		if _, ok := indonesianStrongWords[w]; ok {
			idn++
		}
	}
	return idn >= 1
}

// ClearMessageLanguage returns en|id when the current message clearly indicates language.
func ClearMessageLanguage(userText string) (lang string, ok bool) {
	if IsEnglishBuyerMessage(userText) {
		return LangEN, true
	}
	if isClearlyIndonesianBuyerMessage(userText) {
		return LangID, true
	}
	return "", false
}

// ParseExplicitLanguageSwitch detects an explicit language preference command.
func ParseExplicitLanguageSwitch(userText string) (lang string, ok bool) {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return "", false
	}
	for _, p := range explicitEnglishSwitchPhrases {
		if strings.Contains(text, p) && !languagePhraseNegated(text, p) {
			return LangEN, true
		}
	}
	if strings.Contains(text, "speak english") || strings.Contains(text, "talk english") {
		if !languagePhraseNegated(text, "english") {
			return LangEN, true
		}
	}
	for _, p := range explicitIndonesianSwitchPhrases {
		if strings.Contains(text, p) && !languagePhraseNegated(text, p) {
			return LangID, true
		}
	}
	return "", false
}

// languagePhraseNegated — "can't speak bahasa indonesia" is a refusal, not a switch to Indonesian.
func languagePhraseNegated(text, phrase string) bool {
	idx := strings.Index(text, phrase)
	if idx < 0 {
		return false
	}
	start := idx - 48
	if start < 0 {
		start = 0
	}
	window := text[start:idx]
	for _, n := range []string{
		"can't", "cannot", "cant", "don't", "do not", "dont",
		"not ", "no,", "no i", "bukan", "jangan", "tidak", "gak bisa", "ga bisa", "nggak",
	} {
		if strings.Contains(window, n) {
			return true
		}
	}
	return false
}

func looksLikeShippingAddress(text string) bool {
	if postalCodeIDRe.MatchString(text) {
		return true
	}
	if strings.Contains(text, "address:") || strings.HasPrefix(strings.TrimSpace(text), "alamat") {
		return true
	}
	return false
}

// ParseLanguagePreferenceAnswer parses a reply to the bilingual language question.
func ParseLanguagePreferenceAnswer(userText string) (lang string, ok bool) {
	text := strings.ToLower(strings.TrimSpace(userText))
	switch text {
	case "1", "en", "english", "inggris", "bahasa inggris":
		return LangEN, true
	case "2", "id", "indonesia", "indonesian", "bahasa indonesia":
		return LangID, true
	}
	if looksLikeShippingAddress(text) {
		return "", false
	}
	if strings.Contains(text, "inggris") || strings.Contains(text, "english") {
		if !languagePhraseNegated(text, "english") && !languagePhraseNegated(text, "inggris") {
			return LangEN, true
		}
	}
	if strings.Contains(text, "indonesia") || strings.Contains(text, "indonesian") {
		if languagePhraseNegated(text, "indonesia") || languagePhraseNegated(text, "indonesian") {
			return LangEN, true
		}
		return LangID, true
	}
	return "", false
}

// ResolveBuyerLanguage: explicit switch → clear message language → saved preference → Indonesian.
func ResolveBuyerLanguage(userText, savedPref string) string {
	if lang, ok := ParseExplicitLanguageSwitch(userText); ok {
		return lang
	}
	if lang, ok := ClearMessageLanguage(userText); ok {
		return lang
	}
	if saved := normalizeLang(savedPref); saved == LangEN || saved == LangID {
		return saved
	}
	if saved := CurrentSavedBuyerLang(); saved == LangEN || saved == LangID {
		return saved
	}
	return LangID
}

// LanguageToPersist returns a language code to store on the contact after this message.
func LanguageToPersist(userText, savedPref string) (lang string, ok bool) {
	if lang, ok := ParseExplicitLanguageSwitch(userText); ok {
		return lang, true
	}
	if ans, ok := ParseLanguagePreferenceAnswer(userText); ok && strings.TrimSpace(savedPref) == "" {
		return ans, true
	}
	if lang, ok := ClearMessageLanguage(userText); ok {
		if normalizeLang(savedPref) == lang {
			return "", false
		}
		return lang, true
	}
	return "", false
}

// ShouldAskBuyerLanguagePreference — bare greeting with no saved preference yet.
func ShouldAskBuyerLanguagePreference(userText, savedPref string) bool {
	if strings.TrimSpace(savedPref) != "" || CurrentSavedBuyerLang() != "" {
		return false
	}
	if !IsGreetingLike(userText) {
		return false
	}
	return isBareGreetingForLanguageAsk(userText)
}

func isBareGreetingForLanguageAsk(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	switch text {
	case "hi", "hello", "hey", "halo", "hai", "hei", "hallo":
		return true
	}
	for _, g := range []string{"hi", "hello", "hey", "halo", "hai", "hei", "hallo"} {
		if !strings.HasPrefix(text, g+" ") {
			continue
		}
		rest := strings.TrimSpace(text[len(g):])
		switch rest {
		case "", "kak", "ka", "min", "gan":
			return true
		}
	}
	return false
}

// LanguagePreferenceAskReply — one bilingual bubble, one question.
func LanguagePreferenceAskReply() string {
	return "Halo! Mau lanjut chat in Bahasa Indonesia atau English?\n" +
		"Hello! Would you like to continue in Bahasa Indonesia or English?\n" +
		"Reply *Indonesia* / *English* (or *1* / *2*)."
}

// LanguageSwitchConfirmReply — short confirmation after an explicit switch.
func LanguageSwitchConfirmReply(lang string) string {
	if lang == LangEN {
		return "Sure — I'll reply in English from here."
	}
	return "Siap — aku balas pakai Bahasa Indonesia ya."
}

// ExplicitLanguageSwitchHasShoppingRequest — switch phrase plus a shopping need in the same message.
func ExplicitLanguageSwitchHasShoppingRequest(userText string) bool {
	if _, ok := ParseExplicitLanguageSwitch(userText); !ok {
		return false
	}
	stripped := stripLanguageSwitchPhrases(userText)
	return isCommerceDominant(stripped) || IsEnglishBuyerMessage(stripped) || isClearlyIndonesianBuyerMessage(stripped)
}

func stripLanguageSwitchPhrases(userText string) string {
	text := strings.ToLower(strings.TrimSpace(userText))
	for _, p := range append(append([]string{}, explicitEnglishSwitchPhrases...), explicitIndonesianSwitchPhrases...) {
		text = strings.ReplaceAll(text, p, " ")
	}
	text = strings.ReplaceAll(text, ",", " ")
	return strings.Join(strings.Fields(text), " ")
}

// PickBuyerLanguageReply returns en when ResolveBuyerLanguage says English, otherwise id.
func PickBuyerLanguageReply(userText string, formal bool, idReply, enReply string) string {
	if ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()) == LangEN {
		return enReply
	}
	return idReply
}

var kbRetrievalBridgePairs = []struct {
	en []string
	id []string
}{
	{[]string{"deliver", "delivery", "ship", "shipping", "send", "arrive", "today", "same day", "courier", "fee", "estimate", "eta"},
		[]string{"kirim", "pengiriman", "dikirim", "estimasi", "hari", "hari yang sama", "kurir", "ongkir", "sampai"}},
	{[]string{"pay", "payment", "paid", "transfer"},
		[]string{"bayar", "pembayaran", "transfer"}},
	{[]string{"minimum order", "min order"},
		[]string{"minimum order", "minimal order", "pesan minimal"}},
}

// ExpandKBRetrievalQuery adds Indonesian tokens so lexical KB match works for English questions.
func ExpandKBRetrievalQuery(query string) string {
	text := strings.ToLower(strings.TrimSpace(query))
	if text == "" {
		return query
	}
	seen := make(map[string]struct{})
	var extra []string
	for _, pair := range kbRetrievalBridgePairs {
		matched := false
		for _, w := range pair.en {
			if strings.Contains(text, w) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, tok := range pair.id {
			if _, ok := seen[tok]; ok {
				continue
			}
			seen[tok] = struct{}{}
			extra = append(extra, tok)
		}
	}
	if len(extra) == 0 {
		return query
	}
	return query + " " + strings.Join(extra, " ")
}

func containsIndonesianMarkers(s string) bool {
	s = strings.ToLower(s)
	for _, m := range []string{
		" kami ", " anda ", " pesanan ", " pengiriman ", " pembayaran ", " hari ", " hingga ",
		" seluruh ", " di ", " ya ", " wib", " kurir ", " ongkir", " estimasi ",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	for _, r := range s {
		if r > unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// DeliveryAreaForReply maps known Indonesian delivery-area phrases for English replies.
func DeliveryAreaForReply(area string, lang string) string {
	area = strings.TrimSpace(area)
	if area == "" || lang != LangEN {
		return area
	}
	lower := strings.ToLower(area)
	switch {
	case strings.Contains(lower, "seluruh indonesia"), strings.Contains(lower, "seluruh kota di indonesia"):
		return "across Indonesia"
	case strings.Contains(lower, "pulau jawa"):
		return "across Java"
	case strings.Contains(lower, "indonesia"):
		return "across Indonesia"
	default:
		if containsIndonesianMarkers(" " + lower + " ") {
			return ""
		}
		return area
	}
}

var (
	kbSameDayENRe  = regexp.MustCompile(`(?i)pukul\s*(\d{1,2}:\d{2})\s*hingga\s*(\d{1,2}:\d{2})\s*wib`)
	kbDayRangeENRe = regexp.MustCompile(`(?i)(\d+)\s*-\s*(\d+)\s*(?:hari|business\s+days?|days?)`)
)

type shippingKBFacts struct {
	etaID, etaEN         string
	courierID, courierEN string
	sameDayID, sameDayEN string
}

func isShippingKBCategory(cat string) bool {
	c := strings.ToLower(strings.TrimSpace(cat))
	return strings.Contains(c, "pengiriman") || strings.Contains(c, "shipping") || strings.Contains(c, "delivery")
}

func extractShippingKBFacts(kb []KBEntry) shippingKBFacts {
	var f shippingKBFacts
	for _, e := range kb {
		if !e.IsActive {
			continue
		}
		cat := ""
		if e.Category != nil {
			cat = *e.Category
		}
		if !isShippingKBCategory(cat) {
			continue
		}
		ans := strings.TrimSpace(e.Answer)
		if ans == "" {
			continue
		}
		lower := strings.ToLower(ans)
		if strings.Contains(lower, "estimasi") && strings.Contains(lower, "hari") {
			f.etaID = ans
			f.etaEN = localizeKBETAEN(ans)
		}
		if f.etaEN == "" && kbDayRangeENRe.FindStringSubmatch(ans) != nil &&
			(strings.Contains(lower, "day") || strings.Contains(lower, "business")) {
			f.etaEN = ans
		}
		if strings.Contains(lower, "j&t") || strings.Contains(lower, "jne") ||
			(strings.Contains(lower, "kurir") && strings.Contains(lower, "indonesia")) {
			f.courierID = ans
			f.courierEN = localizeKBCourierEN(ans)
		}
		if strings.Contains(lower, "08:00") && strings.Contains(lower, "hari yang sama") {
			if len(ans) > len(f.sameDayID) {
				f.sameDayID = ans
				f.sameDayEN = localizeKBSameDayEN(ans)
			}
		}
	}
	return f
}

func localizeKBETAEN(answer string) string {
	if m := kbDayRangeENRe.FindStringSubmatch(answer); len(m) >= 3 {
		return "Estimated delivery is " + m[1] + "-" + m[2] + " days."
	}
	return ""
}

func localizeKBCourierEN(answer string) string {
	lower := strings.ToLower(answer)
	if strings.Contains(lower, "j&t") && strings.Contains(lower, "jne") {
		return "We deliver across Indonesia using J&T and JNE couriers."
	}
	return ""
}

func localizeKBSameDayEN(answer string) string {
	m := kbSameDayENRe.FindStringSubmatch(answer)
	if len(m) != 3 {
		return ""
	}
	out := "Orders and payments we receive between " + m[1] + " and " + m[2] + " WIB ship the same day."
	if strings.Contains(strings.ToLower(answer), "12:00") &&
		strings.Contains(strings.ToLower(answer), "hari kerja berikutnya") {
		out += " Orders after 12:00 WIB ship on the next working day."
	}
	return out
}

// LocalizeKBAnswerForBuyer returns English phrasing when the buyer language is English.
func LocalizeKBAnswerForBuyer(answer, userText string) string {
	ans := strings.TrimSpace(answer)
	if ans == "" {
		return ans
	}
	if ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()) != LangEN {
		return ans
	}
	if !containsIndonesianMarkers(" " + strings.ToLower(ans) + " ") {
		return ans
	}
	if en := localizeKBSameDayEN(ans); en != "" {
		return en
	}
	if en := localizeKBCourierEN(ans); en != "" {
		return en
	}
	if en := localizeKBETAEN(ans); en != "" {
		return en
	}
	return ""
}

func asksSameDayDelivery(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"same day", "today", "deliver today", "delivery today", "ship today", "send today",
		"send it today", "send also today", "also today",
		"hari ini", "kirim hari ini", "sampai hari ini",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// BuildShippingFAQReply composes shipping FAQ answers using KB facts and buyer language.
func BuildShippingFAQReply(userText string, profile *BusinessProfile, kb []KBEntry, formal bool) string {
	lang := ResolveBuyerLanguage(userText, CurrentSavedBuyerLang())
	facts := extractShippingKBFacts(kb)
	area := DeliveryAreaForReply(deliveryAreaLabel(profile), lang)
	askFee := IsShippingQuoteOrFeeQuestion(userText)
	askSameDay := asksSameDayDelivery(userText)

	var parts []string
	if lang == LangEN {
		if facts.courierEN != "" {
			parts = append(parts, facts.courierEN)
		} else if area != "" {
			parts = append(parts, "We ship to "+area+".")
		}
		if askFee {
			if formal {
				parts = append(parts, "Shipping cost depends on your address and courier — we'll confirm once we have your full delivery address.")
			} else {
				parts = append(parts, "Shipping cost depends on your address and courier — we'll confirm once we have your full address.")
			}
		}
		if askSameDay && facts.sameDayEN != "" {
			parts = append(parts, facts.sameDayEN)
		} else if !askFee && facts.etaEN != "" {
			parts = append(parts, facts.etaEN)
		} else if !askFee && facts.sameDayEN != "" && askSameDay {
			parts = append(parts, facts.sameDayEN)
		}
		if len(parts) == 0 {
			return shippingETAGenericReplyFor(userText, formal)
		}
		return strings.Join(parts, " ")
	}

	if area != "" {
		if formal {
			parts = append(parts, "Kami melayani pengiriman ke "+area+".")
		} else {
			parts = append(parts, "Kami kirim ke "+area+" ya kak.")
		}
	}
	if facts.courierID != "" {
		parts = append(parts, facts.courierID)
	}
	if askFee {
		if formal {
			parts = append(parts, "Ongkir dihitung setelah alamat lengkap dikonfirmasi.")
		} else {
			parts = append(parts, "Ongkirnya nanti dihitung setelah alamat lengkap ya kak.")
		}
	}
	if askSameDay && facts.sameDayID != "" {
		parts = append(parts, facts.sameDayID)
	} else if facts.etaID != "" {
		parts = append(parts, facts.etaID)
	} else if facts.sameDayID != "" && askSameDay {
		parts = append(parts, facts.sameDayID)
	}
	if len(parts) == 0 {
		return buildShippingDefaultReply(userText, profile, formal)
	}
	return strings.Join(parts, " ")
}
