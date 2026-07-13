package ai

import "testing"

func TestTriageScanLimitForTenant(t *testing.T) {
	tests := []struct {
		remaining int
		want      int
	}{
		{0, 0},
		{10, 10},
		{50, 50},
		{100, 50},
		{200, 50},
	}
	for _, tc := range tests {
		if got := triageScanLimitForTenant(tc.remaining); got != tc.want {
			t.Fatalf("remaining=%d got=%d want=%d", tc.remaining, got, tc.want)
		}
	}
}

func TestFormatPGInterval(t *testing.T) {
	if got := formatPGInterval(TriageAnomalyWindow); got != "1 hour" {
		t.Fatalf("TriageAnomalyWindow interval = %q want 1 hour", got)
	}
}
