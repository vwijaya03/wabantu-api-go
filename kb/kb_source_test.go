package kb

import "testing"

func TestResolveKBEntrySource(t *testing.T) {
	tests := []struct {
		name string
		in   *string
		want string
	}{
		{"nil defaults manual", nil, "manual"},
		{"empty string defaults manual", strPtr(""), "manual"},
		{"whitespace defaults manual", strPtr("   "), "manual"},
		{"explicit manual", strPtr("manual"), "manual"},
		{"pdf import", strPtr("pdf"), "pdf"},
		{"trimmed value", strPtr("  ai_interview  "), "ai_interview"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveKBEntrySource(tt.in); got != tt.want {
				t.Fatalf("resolveKBEntrySource() = %q, want %q", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
