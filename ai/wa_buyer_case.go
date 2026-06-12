package ai

import "strings"

// ResponseBehavior — perilaku balasan yang diharapkan (bukan teks persis).
type ResponseBehavior string

const (
	BehaviorGreeting           ResponseBehavior = "greeting"
	BehaviorCatalogList        ResponseBehavior = "catalog_list"
	BehaviorCatalogProduct     ResponseBehavior = "catalog_product"
	BehaviorCatalogPrice       ResponseBehavior = "catalog_price"
	BehaviorConsulting         ResponseBehavior = "consulting"
	BehaviorOrderFlow          ResponseBehavior = "order_flow"
	BehaviorAskVariant         ResponseBehavior = "ask_variant"
	BehaviorAskQty             ResponseBehavior = "ask_qty"
	BehaviorAskRecipient       ResponseBehavior = "ask_recipient"
	BehaviorAskAddress         ResponseBehavior = "ask_address"
	BehaviorCheckoutComplete   ResponseBehavior = "checkout_complete"
	BehaviorOrderCancel        ResponseBehavior = "order_cancel"
	BehaviorBreakFlow          ResponseBehavior = "break_flow"
	BehaviorQtyUpdated         ResponseBehavior = "qty_updated"
	BehaviorRecipientUpdated   ResponseBehavior = "recipient_prompt"
	BehaviorOrderStatus        ResponseBehavior = "order_status"
	BehaviorPaymentInfo        ResponseBehavior = "payment_consulting"
	BehaviorComplaint          ResponseBehavior = "complaint"
	BehaviorHumanEscalation    ResponseBehavior = "human_escalation"
	BehaviorCorrection         ResponseBehavior = "correction"
	BehaviorSensitive          ResponseBehavior = "sensitive"
	BehaviorNoFalseCheckout    ResponseBehavior = "no_false_checkout"
	BehaviorNoFalseCancel      ResponseBehavior = "no_false_cancel"
)

// WABuyerCase — satu turn terstruktur untuk regresi perilaku pembeli WA.
type WABuyerCase struct {
	ID                       int
	Category                 string
	InputUser                string
	CurrentState             *orderState
	History                  []dbMessage
	ExpectedState            string // none|ask_*|cleared|completed
	ExpectedIntent           string
	ExpectedResponseBehavior ResponseBehavior
	Adversarial              bool
	LanguageStyle            string
	ExpectQty                int // 0 = skip qty check
}

// WABuyerActual — hasil evaluasi satu turn.
type WABuyerActual struct {
	State    string
	Intent   string
	IntentTopic string
	Path     string
	Behavior ResponseBehavior
	Qty      int
	Reply    string
	Outcome  TurnOutcome
}

func cloneOrderState(st *orderState) *orderState {
	if st == nil {
		return nil
	}
	c := *st
	return &c
}

func orderStateLabel(out TurnOutcome) string {
	if out.Completed {
		return "completed"
	}
	if out.Canceled || out.BrokeFlow {
		return "cleared"
	}
	if out.Order == nil {
		return "none"
	}
	return out.Order.Step
}

// EvaluateWABuyerCase menjalankan satu turn dengan state awal yang ditentukan.
func EvaluateWABuyerCase(c WABuyerCase) WABuyerActual {
	sim := newOmahSimulator()
	if c.CurrentState != nil {
		sim.Order = cloneOrderState(c.CurrentState)
	}
	if len(c.History) > 0 {
		sim.History = append([]dbMessage{}, c.History...)
	}
	out := sim.Turn(c.InputUser)

	intent := out.Intent
	if intent.State == "" {
		intent = ResolveSalesIntent(c.InputUser, sim.History, c.CurrentState != nil, sim.inScope(c.InputUser), sim.Profile, sim.Catalog)
		if out.Path == PathOrderFlow || out.Completed {
			intent = SalesIntent{State: SalesStateCheckout, Topic: SalesTopicGeneral, Confidence: 0.9}
		}
		if out.Canceled {
			intent = SalesIntent{State: SalesStateCheckout}
		}
		if out.Path == PathGreeting {
			intent = SalesIntent{State: SalesStateGreeting}
		}
	}

	qty := 0
	if out.Order != nil {
		qty = out.Order.Qty
	} else if c.CurrentState != nil {
		qty = c.CurrentState.Qty
	}

	return WABuyerActual{
		State:       orderStateLabel(out),
		Intent:      intent.State,
		IntentTopic: intent.Topic,
		Path:        out.Path,
		Behavior:    detectResponseBehavior(c, out),
		Qty:         qty,
		Reply:       out.Reply,
		Outcome:     out,
	}
}

func detectResponseBehavior(c WABuyerCase, out TurnOutcome) ResponseBehavior {
	reply := strings.ToLower(out.Reply)
	switch {
	case out.Path == PathGreeting:
		return BehaviorGreeting
	case out.Canceled:
		return BehaviorOrderCancel
	case out.Completed:
		return BehaviorCheckoutComplete
	case out.BrokeFlow:
		return BehaviorBreakFlow
	case IsUserSalesCorrection(c.InputUser):
		return BehaviorCorrection
	case IsComplaintLike(c.InputUser):
		return BehaviorComplaint
	case IsHumanEscalationRequest(c.InputUser):
		return BehaviorHumanEscalation
	}
	if out.Order != nil && c.CurrentState != nil && out.Order.Qty != c.CurrentState.Qty && out.Order.Qty > 0 {
		return BehaviorQtyUpdated
	}
	if out.Path == PathOrderFlow {
		if strings.Contains(reply, "penerima") || strings.Contains(reply, "nama") && strings.Contains(reply, "hp") {
			return BehaviorAskRecipient
		}
		if strings.Contains(reply, "alamat") || strings.Contains(reply, "kode pos") {
			return BehaviorAskAddress
		}
		if strings.Contains(reply, "ukuran") || strings.Contains(reply, "warna") {
			return BehaviorAskVariant
		}
		if strings.Contains(reply, "berapa pcs") || strings.Contains(reply, "jumlahnya berapa") {
			return BehaviorAskQty
		}
		return BehaviorOrderFlow
	}
	if out.Path == PathCatalogDB {
		if IsCatalogListQuestion(c.InputUser) || IsCatalogBrowsingIntent(c.InputUser) ||
			IsRecommendationRequest(c.InputUser) {
			return BehaviorCatalogList
		}
		if strings.Contains(strings.ToLower(c.InputUser), "harga") || strings.Contains(strings.ToLower(c.InputUser), "berapa") {
			return BehaviorCatalogPrice
		}
		return BehaviorCatalogProduct
	}
	if IsOrderStatusInquiry(c.InputUser) {
		return BehaviorOrderStatus
	}
	if IsPaymentQuestion(c.InputUser) {
		return BehaviorPaymentInfo
	}
	return BehaviorConsulting
}
