package buyerflow

import (
	"regexp"
	"strings"
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
		if strings.Contains(text, p) {
			return LangEN, true
		}
	}
	for _, p := range explicitIndonesianSwitchPhrases {
		if strings.Contains(text, p) {
			return LangID, true
		}
	}
	return "", false
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
	if strings.Contains(text, "inggris") || strings.Contains(text, "english") {
		return LangEN, true
	}
	if strings.Contains(text, "indonesia") || strings.Contains(text, "indonesian") {
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
