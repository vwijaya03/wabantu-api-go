package events

import (
	"errors"
	"strings"
	"testing"

	encoreerrs "encore.dev/beta/errs"
)

func strp(s string) *string { return &s }

func TestNormalizeMealNotes(t *testing.T) {
	tests := []struct {
		name   string
		in     *string
		counts bool
		want   *string
		err    bool
	}{
		{"nil", nil, true, nil, false},
		{"trim", strp("  nasi setengah \n"), true, strp("nasi setengah"), false},
		{"empty after trim → NULL", strp("   \t "), true, nil, false},
		{"exactly 200 runes ok", strp(strings.Repeat("a", 200)), true, strp(strings.Repeat("a", 200)), false},
		{"201 runes error", strp(strings.Repeat("a", 201)), true, nil, true},
		{"countsTowardMeals=false → NULL", strp("alergi udang"), false, nil, false},
		{"countsTowardMeals=false ignores too long", strp(strings.Repeat("a", 500)), false, nil, false},
		{"200 multibyte runes ok", strp(strings.Repeat("🍚", 200)), true, strp(strings.Repeat("🍚", 200)), false},
		{"201 multibyte runes error", strp(strings.Repeat("é", 201)), true, nil, true},
		{"trim does not count toward limit", strp("  " + strings.Repeat("x", 200) + "  "), true, strp(strings.Repeat("x", 200)), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeMealNotes(tc.in, tc.counts)
			if tc.err {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				var e *encoreerrs.Error
				if !errors.As(err, &e) || e.Code != encoreerrs.InvalidArgument {
					t.Fatalf("want InvalidArgument, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("got %v, want %v", deref(got), deref(tc.want))
			}
		})
	}
}

func TestMealNotesValue(t *testing.T) {
	f := false
	if mealNotesValue(nil) != nil {
		t.Fatal("nil params → nil")
	}
	if mealNotesValue(&UpsertPersonParams{MealNotes: strp("tanpa nasi"), CountsTowardMeals: &f}) != nil {
		t.Fatal("countsTowardMeals=false → nil")
	}
	if v := mealNotesValue(&UpsertPersonParams{MealNotes: strp(" tanpa nasi ")}); v == nil || *v != "tanpa nasi" {
		t.Fatalf("default counts=true should keep trimmed notes, got %v", deref(v))
	}
}

func TestValidatePersonMealNotes(t *testing.T) {
	f := false
	vol := "role-1"
	p := &UpsertPersonParams{FullName: "Budi", Role: "relawan", VolunteerRoleID: &vol, MealNotes: strp(" alergi udang "), CountsTowardMeals: &f}
	if err := validatePerson(p); err != nil {
		t.Fatal(err)
	}
	if p.MealNotes != nil {
		t.Fatal("countsTowardMeals=false must clear mealNotes")
	}
	p = &UpsertPersonParams{FullName: "Budi", Role: "relawan", VolunteerRoleID: &vol, MealNotes: strp(strings.Repeat("a", 201))}
	if err := validatePerson(p); err == nil {
		t.Fatal("201 runes must be rejected")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
