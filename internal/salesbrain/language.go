package salesbrain

import (
	"regexp"
	"strings"
	"unicode"
)

// LanguageDecision is the outcome of DecideLanguage for one buyer turn.
type LanguageDecision struct {
	Lang      Lang // language to reply in this turn
	Persist   Lang // non-empty: write contact.preferred_language (the language becomes locked)
	Switched  bool // the buyer explicitly chose this language this turn: confirm it in one sentence
	AskChoice bool // ambiguous greeting from an unlocked buyer: ask ID/EN in one bilingual bubble
}

// DecideLanguage applies the language-lock policy. saved is contact.preferred_language
// ("" = not locked yet). Once locked, only an explicit request (ParseExplicitSwitch) changes it.
func DecideLanguage(saved string, tenantDefault Lang, msg string, awaitingChoice bool) LanguageDecision {
	if tenantDefault != LangEN {
		tenantDefault = LangID
	}
	if locked := ParseLang(saved); locked != "" {
		if to, ok := ParseExplicitSwitch(msg); ok && to != locked {
			return LanguageDecision{Lang: to, Persist: to, Switched: true}
		}
		return LanguageDecision{Lang: locked}
	}
	if awaitingChoice {
		if l, ok := parseChoiceAnswer(msg); ok {
			return LanguageDecision{Lang: l, Persist: l, Switched: true}
		}
	}
	if to, ok := ParseExplicitSwitch(msg); ok {
		return LanguageDecision{Lang: to, Persist: to, Switched: true}
	}
	if l, ok := ClearLanguage(msg); ok {
		return LanguageDecision{Lang: l, Persist: l}
	}
	switch greetingKind(msg) {
	case greetAmbiguous:
		return LanguageDecision{Lang: tenantDefault, AskChoice: true}
	case greetID:
		return LanguageDecision{Lang: LangID}
	case greetEN:
		return LanguageDecision{Lang: LangEN}
	}
	return LanguageDecision{Lang: tenantDefault}
}

var apostropheReplacer = strings.NewReplacer("’", "'", "‘", "'", "´", "'", "`", "'")

func normalizeText(s string) string {
	s = apostropheReplacer.Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

func wordTokens(s string) []string {
	raw := strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'')
	})
	out := raw[:0]
	for _, t := range raw {
		if t = strings.Trim(t, "'"); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var structuredDataRe = regexp.MustCompile(`(?m)^\s*(name|nama|phone|hp|no hp|no\. hp|telp|telepon|whatsapp|wa|address|alamat|kode pos|postal code|city|kota)\s*:`)

// IsStructuredData reports form-style data (Name:/Phone:/Address: lines). Such messages never
// decide or switch the language.
func IsStructuredData(msg string) bool {
	return structuredDataRe.MatchString(strings.ToLower(msg))
}

var (
	clauseSplitRe = regexp.MustCompile(`[.!?;,\n]+|\s+but\s+|\s+tapi\s+|\s+karena\s+`)
	switchVerbs   = set("please", "pls", "plz", "reply", "respond", "answer", "talk", "speak", "write",
		"chat", "text", "continue", "use", "switch", "change", "communicate", "prefer",
		"pakai", "pake", "gunakan", "ganti", "ubah", "balas", "jawab", "bicara", "ngomong",
		"lanjut", "tulis", "bisa", "tolong", "minta")
	switchTails = set("please", "pls", "plz", "only", "aja", "saja", "dong", "ya", "deh")
	cantWords   = set("can't", "cant", "cannot", "don't", "dont", "unable", "couldn't", "not",
		"gak", "ga", "gk", "nggak", "ngga", "enggak", "tidak", "tak", "kurang")
	understandWords = set("speak", "read", "understand", "write", "know",
		"bisa", "ngerti", "mengerti", "paham", "faham", "baca")
	shippingWords = set("ship", "shipping", "deliver", "delivery", "send", "kirim", "dikirim",
		"pengiriman", "ongkir", "area", "kota", "city", "seluruh", "all", "from", "dari", "timur", "barat")
	placePrepositions = set("in", "ke", "to", "pakai", "pake")
)

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// ParseExplicitSwitch detects an explicit request to change the reply language. A language
// name only counts when tied to a request verb or tail ("please reply in English", "english
// please", "pake indo aja") or to an inability statement in the same clause ("can't speak
// bahasa indonesia" → English). Place names, structured data and bare mentions never count.
func ParseExplicitSwitch(msg string) (Lang, bool) {
	if IsStructuredData(msg) {
		return "", false
	}
	norm := normalizeText(msg)
	for _, clause := range clauseSplitRe.Split(norm, -1) {
		toks := wordTokens(clause)
		for i := range toks {
			lang, width := languageNameAt(toks, i)
			if lang == "" {
				continue
			}
			before := toks[max(0, i-5):i]
			after := toks[min(len(toks), i+width):min(len(toks), i+width+2)]
			if hasInability(before) {
				return lang.Other(), true
			}
			if containsAny(before, switchVerbs) || containsAny(after, switchTails) {
				return lang, true
			}
			if len(toks) == width {
				return lang, true // the whole clause is just a language name: "english", "bahasa indonesia"
			}
		}
	}
	return "", false
}

// languageNameAt returns the language named at toks[i] and how many tokens the name spans.
func languageNameAt(toks []string, i int) (Lang, int) {
	t := toks[i]
	next := ""
	if i+1 < len(toks) {
		next = toks[i+1]
	}
	prev := ""
	if i > 0 {
		prev = toks[i-1]
	}
	switch t {
	case "english":
		return LangEN, 1
	case "inggris":
		if prev == "bahasa" || prev == "bhs" {
			return "", 0 // counted at "bahasa"
		}
		return LangEN, 1
	case "indonesian":
		return LangID, 1
	case "bahasa", "bhs":
		switch next {
		case "indonesia", "indo":
			return LangID, 2
		case "inggris", "english":
			return LangEN, 2
		}
	case "indo":
		if prev == "bahasa" || prev == "bhs" {
			return "", 0
		}
		if placePrepositions[prev] || len(toks) <= 2 {
			return LangID, 1
		}
	case "indonesia":
		if prev == "bahasa" || prev == "bhs" {
			return "", 0
		}
		if placePrepositions[prev] && containsAny(toks, switchVerbs) && !containsAny(toks, shippingWords) {
			return LangID, 1
		}
	}
	return "", 0
}

func hasInability(window []string) bool {
	seenCant := false
	for _, w := range window {
		if cantWords[w] {
			seenCant = true
			continue
		}
		if seenCant && understandWords[w] {
			return true
		}
	}
	return false
}

func containsAny(toks []string, words map[string]bool) bool {
	for _, t := range toks {
		if words[t] {
			return true
		}
	}
	return false
}

func parseChoiceAnswer(msg string) (Lang, bool) {
	toks := wordTokens(normalizeText(msg))
	if len(toks) == 0 {
		return "", false
	}
	if len(toks) <= 4 {
		for _, t := range toks {
			switch t {
			case "english", "inggris", "eng", "en":
				return LangEN, true
			case "indonesia", "indonesian", "indo", "bahasa", "id", "ind":
				return LangID, true
			}
		}
	}
	return ClearLanguage(msg)
}

var (
	enFunctionWords = set("the", "a", "an", "is", "are", "was", "do", "does", "did", "you", "your",
		"i", "i'm", "im", "me", "my", "we", "want", "would", "like", "to", "of", "for", "can",
		"could", "what", "how", "much", "many", "it", "it's", "have", "has", "please", "and", "or",
		"about", "with", "this", "that", "will", "be", "any", "there", "which", "when", "where",
		"why", "not", "yes", "thanks", "thank", "if", "just", "still", "also", "get", "know",
		"tell", "should", "send", "may", "need")
	idFunctionWords = set("yang", "dan", "di", "ke", "dari", "ini", "itu", "ada", "apa", "apakah",
		"berapa", "bisa", "mau", "saya", "aku", "kak", "kakak", "gak", "ga", "nggak", "tidak", "ya",
		"dong", "sudah", "udah", "belum", "untuk", "buat", "dengan", "harga", "kirim", "pesan",
		"pesanan", "tolong", "bagaimana", "gimana", "kapan", "kalau", "kalo", "kok", "deh", "sih",
		"juga", "lagi", "beli", "nya", "mohon", "boleh", "jadi", "sama", "atau", "tapi", "karena",
		"sekarang", "besok", "hari", "terima", "kasih", "makasih", "min", "gan", "dulu", "aja",
		"saja", "bang", "mbak", "mas", "bu", "pak", "jual", "disini", "begitu")
)

// ClearLanguage returns the language of a message when it is clearly one language: at least 4
// word tokens, at least 2 function-word hits, and ≥70% of the hits in one language.
func ClearLanguage(msg string) (Lang, bool) {
	if IsStructuredData(msg) {
		return "", false
	}
	toks := wordTokens(normalizeText(msg))
	if len(toks) < 4 {
		if len(toks) >= 2 {
			// short but unmistakable Indonesian chat markers ("tuna nya kak?")
			id := 0
			for _, t := range toks {
				if idFunctionWords[t] {
					id++
				}
			}
			if id >= 2 && id == countHits(toks, idFunctionWords, enFunctionWords) {
				return LangID, true
			}
		}
		return "", false
	}
	en := 0
	id := 0
	for _, t := range toks {
		if enFunctionWords[t] {
			en++
		}
		if idFunctionWords[t] {
			id++
		}
	}
	total := en + id
	if total < 2 {
		return "", false
	}
	switch {
	case en >= 2 && float64(en) >= 0.7*float64(total):
		return LangEN, true
	case id >= 2 && float64(id) >= 0.7*float64(total):
		return LangID, true
	}
	return "", false
}

func countHits(toks []string, a, b map[string]bool) int {
	n := 0
	for _, t := range toks {
		if a[t] || b[t] {
			n++
		}
	}
	return n
}

type greetKind int

const (
	greetNone greetKind = iota
	greetAmbiguous
	greetID
	greetEN
)

func collapseRepeats(s string) string {
	var b strings.Builder
	var last rune
	for i, r := range s {
		if i > 0 && r == last {
			continue
		}
		b.WriteRune(r)
		last = r
	}
	return b.String()
}

var (
	idGreetTails = set("kak", "ka", "kakak", "min", "gan", "mbak", "mas", "bang", "bu", "pak", "sis")
	enGreetTails = set("morning", "evening", "afternoon", "night", "there", "everyone")
)

func greetingKind(msg string) greetKind {
	toks := wordTokens(normalizeText(msg))
	if len(toks) == 0 || len(toks) > 4 {
		return greetNone
	}
	joined := strings.Join(toks, " ")
	first := collapseRepeats(toks[0])
	switch {
	case strings.HasPrefix(joined, "selamat "), strings.HasPrefix(joined, "assalam"),
		first == "permisi", first == "punten", first == "pagi", first == "siang",
		first == "sore", first == "malam":
		return greetID
	case strings.HasPrefix(joined, "good "), first == "morning", first == "evening", first == "afternoon":
		return greetEN
	}
	rest := toks[1:]
	switch first {
	case "halo", "hai":
		if containsAny(rest, enGreetTails) {
			return greetEN
		}
		return greetID
	case "hi", "helo", "hey", "hei", "hy":
		if containsAny(rest, idGreetTails) {
			return greetID
		}
		if containsAny(rest, enGreetTails) {
			return greetEN
		}
		return greetAmbiguous
	}
	return greetNone
}
