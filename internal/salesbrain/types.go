// Package salesbrain is the pure decision core of the LLM-first WhatsApp sales assistant:
// language lock, open-order reminders, prompt building, decision parsing and reply
// validation. It has no Encore, DB, Redis or network dependencies so it runs under plain
// `go test`; the ai package maps its own types into the input structs defined here.
package salesbrain

import "strings"

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
// in the prompt; Qty is the desired quantity for that product (0 = remove the line).
type ItemOp struct {
	No  int `json:"no"`
	Qty int `json:"qty"`
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

// Decision is the parsed model output for one buyer turn.
type Decision struct {
	Reply          string   `json:"reply"`
	Intent         Intent   `json:"intent"`
	Items          []ItemOp `json:"items"`
	LanguageSwitch Lang     `json:"language_switch"`
	Handoff        *Handoff `json:"handoff"`
}
