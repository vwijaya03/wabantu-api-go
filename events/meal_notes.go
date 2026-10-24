package events

import (
	"fmt"
	"strings"
	"unicode/utf8"

	appErrs "encore.app/wabantu/shared/errs"
)

// maxMealNotesLen is the maximum length of evt_event_person.meal_notes, in runes.
const maxMealNotesLen = 200

// normalizeMealNotes trims per-person meal notes ("nasi setengah", "alergi udang").
// Returns nil (stored as NULL) when empty or when the person does not count toward meals.
// Longer than maxMealNotesLen runes → InvalidArgument (400).
func normalizeMealNotes(s *string, countsTowardMeals bool) (*string, error) {
	if !countsTowardMeals || s == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(v) > maxMealNotesLen {
		return nil, appErrs.BadRequest(fmt.Sprintf("catatan konsumsi maksimal %d karakter", maxMealNotesLen))
	}
	return &v, nil
}

// mealNotesValue is the DB value for meal_notes; enforces NULL when countsTowardMeals=false
// even on paths that skip validatePerson (e.g. roster import).
func mealNotesValue(p *UpsertPersonParams) *string {
	if p == nil || !countsTowardMealsValue(p) || p.MealNotes == nil {
		return nil
	}
	v := strings.TrimSpace(*p.MealNotes)
	if v == "" {
		return nil
	}
	return &v
}
