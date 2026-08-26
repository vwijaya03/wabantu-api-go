package buyerflow

import (
	"fmt"
	"strings"
)

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string { return &s }

func outOfScopeReply(profile *BusinessProfile) string {
	scope := strings.TrimSpace(strOrEmpty(profile.ProductsServices))
	if scope == "" {
		return "Maaf kak, itu di luar topik bisnis kami ya. Tim CS kami akan bantu follow-up jika diperlukan."
	}
	return fmt.Sprintf("Maaf kak, itu di luar topik bisnis kami ya. Kami fokus pada: %s.", scope)
}
