package salesbrain

import (
	"regexp"
	"strings"
)

// minRedactDigits is the shortest digit run treated as an identifier (phone, account, card).
const minRedactDigits = 8

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// numberRunRe finds a candidate identifier: digits that may be separated by spaces, dashes,
	// dots or parentheses, with an optional leading +. Runs with fewer than minRedactDigits
	// digits are put back by redactNumberRuns.
	numberRunRe = regexp.MustCompile(`\+?\d[\d ().\-]*\d`)
)

// RedactForLog masks emails ([EMAIL]) and identifier-like digit runs of at least 8 digits
// ([NUM]) in text that is logged or stored (raw model output, which may echo the buyer's phone,
// address or email), leaves Rp amounts alone, then cuts the result to maxRunes runes plus a
// trailing "…". Redaction runs before the cut so a cut can never leave half a phone number.
func RedactForLog(s string, maxRunes int) string {
	s = emailRe.ReplaceAllString(s, "[EMAIL]")
	s = redactNumbers(s)
	if maxRunes < 0 {
		maxRunes = 0
	}
	return truncateRunes(s, maxRunes)
}

// redactNumbers applies the digit-run rule everywhere except inside Rp amounts: an amount such
// as Rp12.500.000 has 8 digits but is money, not an identifier, so it is carved out first and
// the surrounding text is redacted on its own.
func redactNumbers(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range rpAmountRe.FindAllStringIndex(s, -1) {
		b.WriteString(redactNumberRuns(s[last:m[0]]))
		b.WriteString(s[m[0]:m[1]])
		last = m[1]
	}
	b.WriteString(redactNumberRuns(s[last:]))
	return b.String()
}

func redactNumberRuns(s string) string {
	return numberRunRe.ReplaceAllStringFunc(s, func(run string) string {
		digits := 0
		for _, r := range run {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits < minRedactDigits {
			return run
		}
		return "[NUM]"
	})
}
