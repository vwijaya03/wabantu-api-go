package ai

import "testing"

func TestResolveAnthropicModel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", APIIDSonnet46},
		{"claude-3-5-haiku-20241022", APIIDHaiku45},
		{AliasHaiku45, APIIDHaiku45},
		{APIIDHaiku45, APIIDHaiku45},
		{AliasSonnet46, APIIDSonnet46},
		{"claude-sonnet-4-5-20250514", APIIDSonnet46},
	}
	for _, tc := range tests {
		if got := ResolveAnthropicModel(tc.in); got != tc.want {
			t.Fatalf("ResolveAnthropicModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFallbackModelsHaiku(t *testing.T) {
	chain := FallbackModels("claude-3-5-haiku-20241022")
	if len(chain) < 1 || chain[0] != APIIDHaiku45 {
		t.Fatalf("expected haiku fallback to start with %s, got %v", APIIDHaiku45, chain)
	}
}
