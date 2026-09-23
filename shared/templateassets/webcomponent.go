package templateassets

import (
	"fmt"
	"regexp"
	"strings"
)

const MaxWebComponentBytes = 200 << 10 // 200 KiB

var forbiddenWCPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)import\s+.*from\s+['"]https?://`),
	regexp.MustCompile(`(?i)fetch\s*\(\s*['"]https?://`),
	regexp.MustCompile(`(?i)eval\s*\(`),
	regexp.MustCompile(`(?i)new\s+Function\s*\(`),
	regexp.MustCompile(`(?i)document\.cookie`),
	regexp.MustCompile(`(?i)localStorage`),
	regexp.MustCompile(`(?i)sessionStorage`),
	regexp.MustCompile(`(?i)postMessage\s*\([^)]*\*`),
	regexp.MustCompile(`(?i)<script`),
}

// ValidateWebComponentSource scans uploaded ESM bundle text before sandbox hosting.
func ValidateWebComponentSource(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("bundle kosong")
	}
	if len(raw) > MaxWebComponentBytes {
		return fmt.Errorf("bundle melebihi %d bytes", MaxWebComponentBytes)
	}
	text := string(raw)
	for _, re := range forbiddenWCPatterns {
		if re.MatchString(text) {
			return fmt.Errorf("bundle mengandung pola tidak diizinkan")
		}
	}
	if !strings.Contains(text, "customElements.define") {
		return fmt.Errorf("bundle harus mendefinisikan custom element")
	}
	return nil
}
