package buyerflow

import (
	"regexp"
	"strings"
)

// 500gram / 500grams / 128gb / 193g → pecah jadi angka + satuan supaya SKU unik ketemu.
var gluedMeasureRe = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)(gram|grams|gramm|grm|grs|gr|kgs|kg|ml|gb|pcs|g)$`)

// canonicalUnitToken maps the unit spellings buyers type ("500grams", "100gr", "2kgs") to the
// spelling catalogs use, so "salmon 250 grams" overlaps "Salmon Steak 250 Gram".
func canonicalUnitToken(tok string) string {
	switch tok {
	case "gr", "grs", "grm", "grams", "gramm":
		return "gram"
	case "kgs":
		return "kg"
	}
	return tok
}

// apparelSizeTokens are single-letter size suffixes dropped by the old len>=2 filter.
var apparelSizeTokens = map[string]struct{}{
	"s": {}, "m": {}, "l": {},
}

func isApparelSizeToken(w string) bool {
	if len(w) != 1 {
		return false
	}
	_, ok := apparelSizeTokens[w]
	return ok
}

func splitGluedMeasureToken(w string) []string {
	m := gluedMeasureRe.FindStringSubmatch(strings.ToLower(w))
	if len(m) != 3 {
		return []string{w}
	}
	return []string{m[1], m[2]}
}

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	cleaned := nonAlphaNum.ReplaceAllString(lower, " ")
	words := strings.Fields(cleaned)
	var out []string
	for _, w := range words {
		for _, part := range splitGluedMeasureToken(w) {
			if len(part) >= 2 || isApparelSizeToken(part) {
				out = append(out, canonicalUnitToken(part))
			}
		}
	}
	return out
}

func overlapScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, x := range a {
		setA[x] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, x := range b {
		setB[x] = struct{}{}
	}
	intersection := 0
	for x := range setA {
		if _, ok := setB[x]; ok {
			intersection++
		}
	}
	denom := len(setA) + len(setB)
	if denom == 0 {
		return 0
	}
	return float64(2*intersection) / float64(denom)
}
