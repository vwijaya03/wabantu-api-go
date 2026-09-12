package triageassert

import (
	"fmt"
	"math"
	"strings"

	"encore.app/wabantu/shared/interactionevidence"
)

// PayloadSpec asserts DB-authoritative rich payload fields.
type PayloadSpec struct {
	RequiredFacts    []string
	ForbiddenClaims  []string
	CardIDs          []string
	CardSlugs        []string
	WantQuickReplies []string
	DegradedMode     string
}

// CheckPayload validates final assembled message + cards (never SSE deltas).
func CheckPayload(ev interactionevidence.TurnEvidence, spec PayloadSpec) error {
	if spec.DegradedMode != "" && string(ev.DegradedMode) != spec.DegradedMode {
		return fmt.Errorf("degradedMode=%q want %q", ev.DegradedMode, spec.DegradedMode)
	}
	text := strings.ToLower(ev.FinalText)
	for _, f := range spec.RequiredFacts {
		if f != "" && !strings.Contains(text, strings.ToLower(f)) {
			return fmt.Errorf("missing fact %q", f)
		}
	}
	for _, f := range spec.ForbiddenClaims {
		if f != "" && strings.Contains(text, strings.ToLower(f)) {
			return fmt.Errorf("forbidden claim %q", f)
		}
	}
	gotIDs := map[string]interactionevidence.ProductCard{}
	for _, c := range ev.ProductCards {
		gotIDs[c.CatalogItemID] = c
	}
	for _, id := range spec.CardIDs {
		card, ok := gotIDs[id]
		if !ok {
			return fmt.Errorf("missing product_card %s", id)
		}
		if card.Name == "" || card.Price < 0 || math.IsNaN(card.Price) {
			return fmt.Errorf("card %s missing DB fields", id)
		}
	}
	if len(spec.WantQuickReplies) > 0 {
		have := map[string]struct{}{}
		for _, q := range ev.QuickReplies {
			have[strings.ToLower(strings.TrimSpace(q))] = struct{}{}
		}
		for _, want := range spec.WantQuickReplies {
			if _, ok := have[strings.ToLower(want)]; !ok {
				return fmt.Errorf("missing quick reply %q", want)
			}
		}
	}
	return nil
}
