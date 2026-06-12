package strutil

import "unicode/utf8"

// TruncateUTF8 shortens s to at most maxBytes without splitting a UTF-8 code point.
func TruncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// TruncateUTF8Ellipsis truncates like TruncateUTF8 and appends "…" when shortened.
func TruncateUTF8Ellipsis(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	ellipsis := "…"
	if maxBytes <= len(ellipsis) {
		return TruncateUTF8(s, maxBytes)
	}
	return TruncateUTF8(s, maxBytes-len(ellipsis)) + ellipsis
}
