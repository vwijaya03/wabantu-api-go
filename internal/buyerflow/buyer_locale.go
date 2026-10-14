package buyerflow

import (
	"regexp"
	"strings"
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

// PickBuyerLanguageReply returns en when the buyer message is English, otherwise id.
func PickBuyerLanguageReply(userText string, formal bool, idReply, enReply string) string {
	if IsEnglishBuyerMessage(userText) {
		return enReply
	}
	return idReply
}
