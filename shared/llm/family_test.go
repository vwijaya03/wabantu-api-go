package llm

import "testing"

func TestModelFamily(t *testing.T) {
	for in, want := range map[string]string{
		"claude-haiku-4-5-20251001":      "haiku-4.5",
		"claude-haiku-4-5":               "haiku-4.5",
		"claude-sonnet-4-5":              "sonnet-4.5",
		"claude-opus-4-1-20250805":       "opus-4.1",
		"claude-sonnet-4-20250514":       "sonnet-4",
		"claude-3-5-haiku-20241022":      "haiku-3.5",
		"claude-3-haiku-20240307":        "haiku-3",
		"gemini-2.5-flash":               "gemini-2.5-flash",
		"models/gemini-2.5-flash":        "gemini-2.5-flash",
		"gemini-2.5-flash-preview-05-20": "gemini-2.5-flash",
		"gemini-2.5-flash-lite":          "gemini-2.5-flash-lite",
		"gemini-2.5-pro":                 "gemini-2.5-pro",
		"text-embedding-3-small":         "text-embedding-3-small",
		"":                               "",
	} {
		if got := ModelFamily(in); got != want {
			t.Errorf("ModelFamily(%q)=%q want %q", in, got, want)
		}
	}
}
