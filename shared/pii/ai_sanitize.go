package pii

import (
	"regexp"
	"strings"
)

var (
	aiPhoneRE = regexp.MustCompile(`(?i)(\+62|62|08)[\s\-]?[0-9]{8,12}`)
	aiEmailRE = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	aiBankRE  = regexp.MustCompile(`\b[0-9]{10,16}\b`)
	aiOrderRE = regexp.MustCompile(`(?i)\bWB-[A-Z0-9]+\b`)
	aiUUIDRE  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

// SanitizeForExternalAI redacts PII before payloads leave the server (Cursor/Composer).
func SanitizeForExternalAI(raw string) string {
	if raw == "" {
		return raw
	}
	out := strings.ReplaceAll(raw, "\x00", "")
	out = aiEmailRE.ReplaceAllString(out, "[EMAIL]")
	out = aiPhoneRE.ReplaceAllString(out, "[PHONE]")
	out = aiOrderRE.ReplaceAllString(out, "[ORDER_REF]")
	out = aiUUIDRE.ReplaceAllString(out, "[ID]")
	out = aiBankRE.ReplaceAllString(out, "[ACCOUNT]")
	return out
}
