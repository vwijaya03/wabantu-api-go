package templateassets

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const MaxLottieBytes = 512 << 10 // 512 KiB

var forbiddenLottiePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://`),
	regexp.MustCompile(`(?i)javascript:`),
	regexp.MustCompile(`(?i)data:`),
	regexp.MustCompile(`(?i)<script`),
	regexp.MustCompile(`(?i)@import`),
	regexp.MustCompile(`(?i)url\(`),
}

// ValidateLottieJSON rejects external refs and oversized payloads before S3 upload.
func ValidateLottieJSON(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("lottie kosong")
	}
	if len(raw) > MaxLottieBytes {
		return fmt.Errorf("lottie melebihi %d bytes", MaxLottieBytes)
	}
	text := string(raw)
	for _, re := range forbiddenLottiePatterns {
		if re.MatchString(text) {
			return fmt.Errorf("lottie mengandung konten tidak diizinkan")
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("lottie JSON tidak valid: %w", err)
	}
	if _, ok := doc["v"]; !ok {
		if _, ok2 := doc["layers"]; !ok2 {
			return fmt.Errorf("lottie harus memiliki field v atau layers")
		}
	}
	return scanJSONStrings(doc)
}

func scanJSONStrings(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if strings.EqualFold(k, "u") && val != nil && val != "" {
				return fmt.Errorf("lottie asset path (u) tidak diizinkan di v1")
			}
			if err := scanJSONStrings(val); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range x {
			if err := scanJSONStrings(item); err != nil {
				return err
			}
		}
	case string:
		low := strings.ToLower(x)
		if strings.Contains(low, "http://") || strings.Contains(low, "https://") {
			return fmt.Errorf("string eksternal tidak diizinkan dalam lottie")
		}
	}
	return nil
}
