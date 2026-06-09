package events

import (
	"strings"
	"unicode"
)

// pdfSafeText normalizes text for gofpdf core fonts (Latin-1). Unicode outside
// Windows-1252 causes panics in SplitText/MultiCell (e.g. em-dash, ellipsis).
func pdfSafeText(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\u2014", "-", // em dash
		"\u2013", "-", // en dash
		"\u2026", "...", // ellipsis
		"\u00a0", " ", // nbsp
		"\u2018", "'", "\u2019", "'",
		"\u201c", "\"", "\u201d", "\"",
	)
	s = replacer.Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 256 {
			b.WriteRune(r)
			continue
		}
		if r == '\u00b7' { // middle dot in filter summary
			b.WriteRune(r)
			continue
		}
		// Drop emoji / rare symbols; keep letters if Latin-1 compatible after NFD is too heavy.
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune('?')
		}
	}
	return b.String()
}
