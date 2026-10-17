package salesbrain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNoJSON     = errors.New("salesbrain: no JSON object in model output")
	ErrEmptyReply = errors.New("salesbrain: empty reply")
)

// ParseDecision extracts the JSON decision from raw model output (code fences and stray text
// tolerated), then normalises it: unknown intent → other, items kept only for order/amend and
// only with no ≥ 1 and 0 ≤ qty ≤ 999 (size and colour trimmed), fields kept (trimmed) only for field_answer, invalid
// handoff dropped, language_switch normalised.
func ParseDecision(raw string) (Decision, error) {
	s := strings.TrimSpace(raw)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return Decision{}, ErrNoJSON
	}
	var d Decision
	if err := json.Unmarshal([]byte(s[start:end+1]), &d); err != nil {
		return Decision{}, fmt.Errorf("salesbrain: decode decision: %w", err)
	}
	d.Reply = strings.TrimSpace(d.Reply)
	if d.Reply == "" {
		return Decision{}, ErrEmptyReply
	}
	if !knownIntents[d.Intent] {
		d.Intent = IntentOther
	}
	d.LanguageSwitch = ParseLang(string(d.LanguageSwitch))
	if d.Handoff != nil && d.Handoff.Level != HandoffNote && d.Handoff.Level != HandoffTakeover {
		d.Handoff = nil
	}
	if d.Intent == IntentFieldAnswer {
		d.Fields.trim()
	} else {
		d.Fields = nil
	}
	if d.Intent != IntentOrder && d.Intent != IntentAmend {
		d.Items = nil
		return d, nil
	}
	var items []ItemOp
	for _, it := range d.Items {
		if it.No >= 1 && it.Qty >= 0 && it.Qty <= 999 {
			it.Size, it.Color = strings.TrimSpace(it.Size), strings.TrimSpace(it.Color)
			items = append(items, it)
		}
	}
	d.Items = items
	return d, nil
}
