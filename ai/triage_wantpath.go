package ai

import (
	"strings"
	"unicode"
)

// SuggestWantPath returns the golden path for a forensic mismatch.
// Order-like qty lists must not lock PathConsulting — those cases are untrusted.
func SuggestWantPath(m TriageMismatch) (path string, untrusted bool) {
	want := strings.TrimSpace(m.ExpectedPath)
	if want == "" {
		return "", true
	}
	if want == PathConsulting && looksLikeOrderList(m.UserText) {
		return "", true
	}
	return want, false
}

func looksLikeOrderList(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "\n") {
		return false
	}
	nonEmpty := 0
	qtyLines := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		if orderListLineHasQty(line) {
			qtyLines++
		}
	}
	return nonEmpty >= 2 && qtyLines >= 1
}

func orderListLineHasQty(line string) bool {
	hasLetter := false
	hasDigit := false
	for _, r := range line {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return true
		}
	}
	return false
}
