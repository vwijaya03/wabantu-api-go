package templateassets

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	colorTokenRe  = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)
	sizeTokenRe   = regexp.MustCompile(`^(\d+(\.\d+)?)(px|rem|em|%)$`)
	animPresetRe  = regexp.MustCompile(`^(minimal|smooth|playful|glass|bold)$`)
	forbiddenCSS  = regexp.MustCompile(`(?i)(url\(|@import|javascript:|expression\(|<|>)`)
)

// SanitizeManifestTokens validates token map server-side (§10.5).
func SanitizeManifestTokens(tokens map[string]any) error {
	return walkTokens("", tokens)
}

func walkTokens(prefix string, v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			key := strings.ToLower(k)
			path := prefix + "." + key
			if err := walkTokens(path, val); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range x {
			if err := walkTokens(prefix, item); err != nil {
				return err
			}
		}
	case string:
		return validateTokenString(prefix, x)
	}
	return nil
}

func validateTokenString(path, val string) error {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	if forbiddenCSS.MatchString(val) {
		return fmt.Errorf("token %s mengandung pola tidak aman", path)
	}
	low := strings.ToLower(path)
	if strings.Contains(low, "color") || strings.HasSuffix(low, "foreground") || strings.HasSuffix(low, "background") {
		if !colorTokenRe.MatchString(val) {
			return fmt.Errorf("token warna %s tidak valid", path)
		}
	}
	if strings.Contains(low, "radius") || strings.HasSuffix(low, "size") ||
		strings.HasSuffix(low, "width") || strings.HasSuffix(low, "height") {
		if !sizeTokenRe.MatchString(val) && val != "9999px" {
			return fmt.Errorf("token ukuran %s tidak valid", path)
		}
	}
	if strings.Contains(low, "animation") && strings.Contains(low, "preset") {
		if !animPresetRe.MatchString(val) {
			return fmt.Errorf("animation preset tidak valid")
		}
	}
	return nil
}
