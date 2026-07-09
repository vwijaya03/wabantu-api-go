package events

import "testing"

func TestPublicDisplayNotes(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"Pendaftaran online (staf/relawan)", "Pendaftaran online (staf/relawan)"},
		{"Pendaftaran online — catatan user · Telp: 08123456789", "Pendaftaran online — catatan user"},
		{"Telp: 0812", ""},
		{"catatan saja", "catatan saja"},
	}
	for _, tc := range tests {
		got := publicDisplayNotes(tc.in)
		if got != tc.want {
			t.Errorf("publicDisplayNotes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCountsTowardMealsValue(t *testing.T) {
	falseVal := false
	if countsTowardMealsValue(nil) != true {
		t.Fatal("nil should default true")
	}
	if countsTowardMealsValue(&UpsertPersonParams{}) != true {
		t.Fatal("missing field should default true")
	}
	if countsTowardMealsValue(&UpsertPersonParams{CountsTowardMeals: &falseVal}) != false {
		t.Fatal("explicit false should be false")
	}
}
