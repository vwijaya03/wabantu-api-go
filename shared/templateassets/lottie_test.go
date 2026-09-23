package templateassets

import "testing"

func TestValidateLottieJSONRejectsExternalURL(t *testing.T) {
	raw := []byte(`{"v":"5.7.4","layers":[],"assets":[{"u":"https://evil.com/","p":"x"}]}`)
	err := ValidateLottieJSON(raw)
	if err == nil {
		t.Fatal("expected rejection")
	}
}

func TestValidateLottieJSONAcceptsMinimal(t *testing.T) {
	raw := []byte(`{"v":"5.7.4","layers":[]}`)
	if err := ValidateLottieJSON(raw); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeManifestTokensRejectsCSSInjection(t *testing.T) {
	tokens := map[string]any{
		"color": map[string]any{"primary": "red; } body { display: none }"},
	}
	if err := SanitizeManifestTokens(tokens); err == nil {
		t.Fatal("expected rejection")
	}
}
