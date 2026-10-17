// Package salesbrain is the pure decision core of the LLM-first WhatsApp sales assistant:
// language lock, open-order reminders, prompt building, decision parsing and reply
// validation. It has no Encore, DB, Redis or network dependencies so it runs under plain
// `go test`; the ai package maps its own types into the input structs defined here.
package salesbrain

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Lang is the buyer reply language.
type Lang string

const (
	LangID Lang = "id"
	LangEN Lang = "en"
)

// ParseLang maps a stored code ("id"/"en", any case) to a Lang; anything else → "".
func ParseLang(s string) Lang {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "id":
		return LangID
	case "en":
		return LangEN
	}
	return ""
}

// Other returns the opposite language (only two are supported).
func (l Lang) Other() Lang {
	if l == LangEN {
		return LangID
	}
	return LangEN
}

// Intent is the model's classification of one buyer turn.
type Intent string

const (
	IntentGreeting      Intent = "greeting"
	IntentQuestion      Intent = "question"
	IntentBrowse        Intent = "browse"
	IntentRecommend     Intent = "recommend"
	IntentOrder         Intent = "order"
	IntentAmend         Intent = "amend"
	IntentCancel        Intent = "cancel"
	IntentFieldAnswer   Intent = "field_answer"
	IntentConfirm       Intent = "confirm"
	IntentContinueOrder Intent = "continue_order"
	IntentNewOrder      Intent = "new_order"
	IntentStatus        Intent = "status"
	IntentPaymentProof  Intent = "payment_proof"
	IntentComplaint     Intent = "complaint"
	IntentHumanRequest  Intent = "human_request"
	IntentOther         Intent = "other"
)

var knownIntents = map[Intent]bool{
	IntentGreeting: true, IntentQuestion: true, IntentBrowse: true, IntentRecommend: true,
	IntentOrder: true, IntentAmend: true, IntentCancel: true, IntentFieldAnswer: true,
	IntentConfirm: true, IntentContinueOrder: true, IntentNewOrder: true, IntentStatus: true,
	IntentPaymentProof: true, IntentComplaint: true, IntentHumanRequest: true, IntentOther: true,
}

// ItemOp is a cart change proposed by the model. No is the 1-based catalog row number shown
// in the prompt; Qty is the desired quantity for that product (0 = remove the line). Size and
// Color are the buyer's own words for the variant of a product that needs one; code accepts them
// only when they also appear in the buyer's messages (ResolveCartOps).
type ItemOp struct {
	No    int    `json:"no"`
	Qty   int    `json:"qty"`
	Size  string `json:"size,omitempty"`
	Color string `json:"color,omitempty"`
}

// UnmarshalJSON accepts numbers as well as strings for size and colour ("size": 38 is common for
// shoes); a number would otherwise make the whole decision unparseable.
func (o *ItemOp) UnmarshalJSON(b []byte) error {
	var raw struct {
		No    int `json:"no"`
		Qty   int `json:"qty"`
		Size  any `json:"size"`
		Color any `json:"color"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	text := func(v any) string {
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x)
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	*o = ItemOp{No: raw.No, Qty: raw.Qty, Size: text(raw.Size), Color: text(raw.Color)}
	return nil
}

// HandoffLevel: note keeps the AI active (owner is notified); takeover pauses it with auto-resume.
type HandoffLevel string

const (
	HandoffNote     HandoffLevel = "note"
	HandoffTakeover HandoffLevel = "takeover"
)

// Handoff is the model's request to involve the shop team.
type Handoff struct {
	Level  HandoffLevel `json:"level"`
	Reason string       `json:"reason"`
}

// FieldData is the checkout data the model extracted from a field_answer message. Values are
// the buyer's own words; the caller validates each one against the buyer text before storing.
type FieldData struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Street     string `json:"street"`
	District   string `json:"district"`
	City       string `json:"city"`
	Province   string `json:"province"`
	PostalCode string `json:"postal_code"`
}

// UnmarshalJSON accepts numbers as well as strings for every field: models sometimes emit
// "postal_code": 60177, which would otherwise make the whole decision unparseable.
func (f *FieldData) UnmarshalJSON(b []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	get := func(k string) string {
		switch v := raw[k].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	}
	*f = FieldData{
		Name: get("name"), Phone: get("phone"), Street: get("street"), District: get("district"),
		City: get("city"), Province: get("province"), PostalCode: get("postal_code"),
	}
	return nil
}

// trim strips surrounding whitespace from every field. A nil receiver is left alone.
func (f *FieldData) trim() {
	if f == nil {
		return
	}
	for _, p := range []*string{&f.Name, &f.Phone, &f.Street, &f.District, &f.City, &f.Province, &f.PostalCode} {
		*p = strings.TrimSpace(*p)
	}
}

// Decision is the parsed model output for one buyer turn.
type Decision struct {
	Reply          string     `json:"reply"`
	Intent         Intent     `json:"intent"`
	Items          []ItemOp   `json:"items"`
	Fields         *FieldData `json:"fields"`
	LanguageSwitch Lang       `json:"language_switch"`
	Handoff        *Handoff   `json:"handoff"`
}
