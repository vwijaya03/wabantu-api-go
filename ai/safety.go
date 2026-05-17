package ai

import (
	"regexp"
	"strings"
)

var promptInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+instructions?`),
	regexp.MustCompile(`(?i)system\s+prompt`),
	regexp.MustCompile(`(?i)developer\s+message`),
	regexp.MustCompile(`(?i)act\s+as\s+(an?\s+)?(admin|developer|root)`),
	regexp.MustCompile(`(?i)(show|reveal|leak).*(secret|token|password|key)`),
	regexp.MustCompile(`(?i)(drop|truncate|delete|alter)\s+(table|database|schema)`),
}

var idStopwords = map[string]struct{}{
	"yang": {}, "dan": {}, "atau": {}, "untuk": {}, "dengan": {},
	"dari": {}, "kami": {}, "kamu": {}, "saya": {}, "anda": {},
	"ini": {}, "itu": {}, "ada": {}, "mau": {}, "juga": {},
	"agar": {}, "supaya": {}, "bisa": {}, "lebih": {}, "sudah": {},
	"belum": {}, "dalam": {}, "pada": {}, "di": {}, "ke": {},
	"the": {}, "and": {}, "for": {}, "with": {},
}

var questionKeywords = []string{
	"apa", "apakah", "berapa", "gimana", "bagaimana",
	"kapan", "bisa", "stok", "size", "ukuran",
	"harga", "order", "pesan", "kirim",
}

var greetingPrefixes = []string{
	"selamat pagi", "selamat siang", "selamat sore", "selamat malam",
	"halo", "hai", "assalamualaikum", "salam", "permisi",
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9\s]`)
var digitOnly = regexp.MustCompile(`^\d+$`)

func SanitizeForPrompt(raw string) string {
	if raw == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(raw, "\x00", "")
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > 2000 {
		cleaned = cleaned[:2000]
	}
	return cleaned
}

func IsPromptInjectionLikely(raw string) bool {
	if raw == "" {
		return false
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return false
	}
	for _, p := range promptInjectionPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

func IsQuestionLike(raw string) bool {
	if raw == "" {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false
	}
	if strings.Contains(text, "?") {
		return true
	}
	for _, kw := range questionKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func IsGreetingLike(raw string) bool {
	if raw == "" {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false
	}
	for _, g := range greetingPrefixes {
		if text == g || strings.HasPrefix(text, g+" ") {
			return true
		}
	}
	return false
}

func ExtractScopeKeywords(scopeText string) []string {
	lower := strings.ToLower(scopeText)
	cleaned := nonAlphaNum.ReplaceAllString(lower, " ")
	words := strings.Fields(cleaned)

	seen := make(map[string]struct{})
	var result []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len(w) < 4 {
			continue
		}
		if _, stop := idStopwords[w]; stop {
			continue
		}
		if digitOnly.MatchString(w) {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		result = append(result, w)
	}
	return result
}

func IsWithinBusinessScope(userText string, scopeKW, fallbackKW []string) bool {
	text := strings.ToLower(userText)
	lookup := scopeKW
	if len(lookup) == 0 {
		lookup = fallbackKW
	}
	if len(lookup) == 0 {
		return true
	}
	for _, kw := range lookup {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
