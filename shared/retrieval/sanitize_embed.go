package retrieval

import "regexp"

var (
	embedPhoneRE = regexp.MustCompile(`(?i)(?:\+62|62|0)\d{8,14}`)
	embedEmailRE = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	embedBankRE  = regexp.MustCompile(`\b\d{10,16}\b`)
)

// SanitizeForEmbed redacts common PII before text is sent to external embedding APIs.
func SanitizeForEmbed(text string) string {
	if text == "" {
		return ""
	}
	s := text
	s = embedPhoneRE.ReplaceAllString(s, "[PHONE]")
	s = embedEmailRE.ReplaceAllString(s, "[EMAIL]")
	s = embedBankRE.ReplaceAllString(s, "[ACCOUNT]")
	return s
}

// RedactPII redacts common PII patterns for production logs.
func RedactPII(text string) string {
	return SanitizeForEmbed(text)
}
