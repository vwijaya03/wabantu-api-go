package ai

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/order"
)

// lfFakeFX records the writes of a turn in the order they happen.
type lfFakeFX struct {
	calls      []string
	states     []orderState
	persisted  []orderState
	cancelled  []string
	paused     []string
	notes      []lfNote
	persistID  string
	persistErr error
	early      []orderState // states handed to persistEarlyDraft
	earlyID    string       // id the early draft gets ("" = a default id)
	earlyErr   error
	earlyPick  []bool // needPick answers, consumed one per call (empty: false)
	cancelErr  error
	pauseErr   error
}

type lfNote struct{ kind, title, body string }

func (f *lfFakeFX) setState(_ context.Context, st orderState) {
	f.calls = append(f.calls, "set")
	f.states = append(f.states, st)
}
func (f *lfFakeFX) clearState(context.Context) { f.calls = append(f.calls, "clear") }
func (f *lfFakeFX) persistOrder(_ context.Context, st orderState) (string, error) {
	f.calls = append(f.calls, "persist")
	f.persisted = append(f.persisted, st)
	if f.persistErr != nil {
		return "", f.persistErr
	}
	if f.persistID == "" {
		return "9f1c2d3e-0000-0000-0000-000000000000", nil
	}
	return f.persistID, nil
}
func (f *lfFakeFX) persistEarlyDraft(_ context.Context, st orderState) (string, bool, error) {
	f.calls = append(f.calls, "early")
	f.early = append(f.early, st)
	if len(f.earlyPick) > 0 {
		pick := f.earlyPick[0]
		f.earlyPick = f.earlyPick[1:]
		if pick {
			return "", true, nil
		}
	}
	if f.earlyErr != nil {
		return "", false, f.earlyErr
	}
	if f.earlyID == "" {
		return "7a0b1c2d-0000-0000-0000-000000000000", false, nil
	}
	return f.earlyID, false, nil
}
func (f *lfFakeFX) cancelOrder(_ context.Context, id string) error {
	f.calls = append(f.calls, "cancel")
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}
func (f *lfFakeFX) pause(_ context.Context, reason string) error {
	f.calls = append(f.calls, "pause")
	f.paused = append(f.paused, reason)
	return f.pauseErr
}
func (f *lfFakeFX) notify(_ context.Context, kind, title, body string) {
	f.calls = append(f.calls, "notify")
	f.notes = append(f.notes, lfNote{kind, title, body})
}
func (f *lfFakeFX) did(call string) int {
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

// lfTestPlan builds the turnPlan planTurn would have produced for decision d.
func lfTestPlan(d salesbrain.Decision, lang salesbrain.Lang, cart []salesbrain.OrderLine, changed bool, pendingField string) turnPlan {
	return turnPlan{
		Decision: d, Lang: lang, Cart: cart, CartChanged: changed,
		Reply: lfComposeReply(d.Reply, cart, changed, pendingField, lang),
	}
}

func lfApplyBase(plan turnPlan, st *orderState) lfApplyIn {
	return lfApplyIn{
		Plan: plan, UserText: "ok", Shop: "Omah Apparel", OpeningHours: "08:00 - 19:00 GMT +7",
		State: st, Catalog: lfOrdersCatalog(), Facts: lfOmahCatalog(),
	}
}

func lfConfirmState() *orderState {
	st := lfTunaState()
	st.RecipientName, st.RecipientPhone = "Ana", "+62872839472828"
	st.Street, st.City, st.Province, st.PostalCode = "Villa Kalijudan Indah A9", "Surabaya", "Jawa Timur", "60177"
	st.Step = lfStepConfirm
	return st
}

func lfRunAfter(t *testing.T, out lfOutcome) error {
	t.Helper()
	if out.After == nil {
		return nil
	}
	return out.After(context.Background())
}

// ── order / amend ────────────────────────────────────────────────────────────

func TestLLMFirstApplyOrderSavesCartState(t *testing.T) {
	fx := &lfFakeFX{}
	cart := []salesbrain.OrderLine{lfTunaLine()}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Sure, added 1x Yellow Tail Tuna Portion 100 Gram."}, salesbrain.LangEN, cart, true, "")

	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || fx.did("clear") != 0 {
		t.Fatalf("one state write expected, calls = %v", fx.calls)
	}
	st := fx.states[0]
	if st.Step != lfStepAskRecipient || len(st.Items) != 1 || st.Items[0].CatalogItemID != "tuna-100" || st.Items[0].ExternalCode != "YELLOW_TAIL_TUNA_PORTION_100_GRAM" {
		t.Fatalf("state = %+v", st)
	}
	if out.Reply != plan.Reply || !strings.Contains(out.Reply, "Order summary") || !strings.Contains(out.Reply, "Rp350.000") {
		t.Fatalf("reply = %q", out.Reply)
	}
	if out.Author != "ai" || out.ReplacedReason != "" {
		t.Fatalf("outcome = %+v", out)
	}
	if fx.did("early") != 0 {
		t.Fatalf("the draft is written after the send, not before: %v", fx.calls)
	}
}

func TestLLMFirstApplyAmendToEmptyCartClearsState(t *testing.T) {
	fx := &lfFakeFX{}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentAmend, Reply: "Done, I removed it."}, salesbrain.LangEN, nil, true, "recipient")
	if _, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, lfTunaState())); err != nil {
		t.Fatal(err)
	}
	if fx.did("clear") != 1 || fx.did("set") != 0 {
		t.Fatalf("an empty cart clears the state, calls = %v", fx.calls)
	}
}

func TestLLMFirstApplyOrderWithoutChangeLeavesStateAlone(t *testing.T) {
	fx := &lfFakeFX{}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Which salmon, 100 or 250 gram?"}, salesbrain.LangEN, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	if _, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, lfTunaState())); err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("no cart change, no write: %v", fx.calls)
	}
}

func TestLLMFirstApplyMarksUpsellWhenReplyOffersAnotherProduct(t *testing.T) {
	fx := &lfFakeFX{}
	cart := []salesbrain.OrderLine{lfTunaLine()}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added the tuna. Would you like Salmon Steak 100 Gram with it?"}, salesbrain.LangEN, cart, true, "")
	if _, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil)); err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || !fx.states[0].UpsellShown {
		t.Fatalf("the offer must be remembered so it is not repeated: %+v", fx.states)
	}

	fx = &lfFakeFX{}
	plain := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added 1x Yellow Tail Tuna Portion 100 Gram."}, salesbrain.LangEN, cart, true, "")
	if _, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plain, nil)); err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || fx.states[0].UpsellShown {
		t.Fatalf("naming the cart item is not an upsell: %+v", fx.states)
	}
}

// ── field answers ────────────────────────────────────────────────────────────

func TestLLMFirstApplyFieldAnswerWithProblemsNamesThemAndAsksAgain(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentFieldAnswer, Reply: "Thanks Ana.", Fields: &salesbrain.FieldData{Name: "Ana", Phone: "98822123"}}
	plan := lfTestPlan(d, salesbrain.LangEN, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	in := lfApplyBase(plan, lfTunaState())
	in.UserText = "Ana 98822123"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || fx.states[0].RecipientName != "Ana" || fx.states[0].RecipientPhone != "" {
		t.Fatalf("the valid name is kept and the bad phone is not written: %+v", fx.states)
	}
	for _, want := range []string{"Noted: name Ana.", "Sorry, the phone number 98822123 has 8 digits", salesbrain.ResumeLineFor("recipient", salesbrain.LangEN)} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, out.Reply)
		}
	}
	if strings.Index(out.Reply, "Noted:") > strings.Index(out.Reply, "Sorry,") || strings.Index(out.Reply, "Sorry,") > strings.Index(out.Reply, "Back to your order") {
		t.Errorf("order must be what was saved, problems, resume line:\n%s", out.Reply)
	}
}

func TestLLMFirstApplyFieldAnswerCompleteShowsFullSummary(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentFieldAnswer, Reply: "Thanks Ana, I have everything.", Fields: lfFullAddressFields()}
	plan := lfTestPlan(d, salesbrain.LangEN, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	in := lfApplyBase(plan, lfTunaState())
	in.UserText = lfFullAddressText

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || fx.states[0].Step != lfStepConfirm {
		t.Fatalf("all fields given: the step must be confirm, got %+v", fx.states)
	}
	for _, want := range []string{
		"Thanks Ana, I have everything.", "Order summary", "1x Yellow Tail Tuna Portion 100 Gram", "Subtotal: Rp350.000",
		"Shipping to:", "Ana (+62872839472828)", "Villa Kalijudan Indah A9, Surabaya, Jawa Timur, 60177",
		salesbrain.ResumeLineFor("confirm", salesbrain.LangEN),
	} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, out.Reply)
		}
	}
	if strings.Contains(out.Reply, "Back to your order") {
		t.Errorf("nothing is pending any more:\n%s", out.Reply)
	}
}

func TestLLMFirstApplyFieldAnswerStageResumesNextField(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentFieldAnswer, Reply: "Got it, thanks Ana.", Fields: &salesbrain.FieldData{Name: "Ana", Phone: "0872839472828"}}
	plan := lfTestPlan(d, salesbrain.LangID, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	in := lfApplyBase(plan, lfTunaState())
	in.UserText = "Ana 0872839472828"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || fx.states[0].Step != lfStepAskAddressFull {
		t.Fatalf("name and phone done: next is the address, got %+v", fx.states)
	}
	if !strings.HasSuffix(out.Reply, salesbrain.ResumeLineFor("address", salesbrain.LangID)) {
		t.Fatalf("reply must end with the address question:\n%s", out.Reply)
	}
}

func TestLLMFirstApplyFieldAnswerWithoutCheckoutStateWritesNothing(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentFieldAnswer, Reply: "Thanks!", Fields: lfFullAddressFields()}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 || out.Reply != plan.Reply {
		t.Fatalf("with no cart there is nothing to fill in: calls=%v reply=%q", fx.calls, out.Reply)
	}
}

// ── confirm ──────────────────────────────────────────────────────────────────

func TestLLMFirstApplyConfirmRequiresExplicitBuyerText(t *testing.T) {
	fx := &lfFakeFX{persistID: "draft-should-not-save"}
	st := lfConfirmState()
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Pesanan Kakak sudah kami proses ya."}
	plan := lfTestPlan(d, salesbrain.LangID, cartFromState(st), false, "confirm")
	in := lfApplyBase(plan, st)
	in.UserText = "ok tapi ganti alamat"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("persist") != 0 {
		t.Fatalf("non-explicit confirm must not persist: %v", fx.calls)
	}
	if out.ReplacedReason != lfReplacedNotExplicitConfirm || out.Author != "system" {
		t.Fatalf("outcome = %+v", out)
	}
	if !strings.Contains(out.Reply, "OK") || !strings.Contains(out.Reply, "Yellow Tail Tuna") {
		t.Fatalf("must re-show confirm block, got %q", out.Reply)
	}
}

func TestLLMFirstApplyConfirmPersistsAndAppendsOrderRef(t *testing.T) {
	fx := &lfFakeFX{persistID: "f4bfce06-1111-2222-3333-444455556666"}
	st := lfConfirmState()
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Thank you Ana! Please pay by BCA transfer."}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "confirm")
	if !strings.Contains(plan.Reply, "reply OK") {
		t.Fatalf("test setup: planTurn appends the resume line here, got %q", plan.Reply)
	}

	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("persist") != 1 {
		t.Fatalf("the order is saved exactly once, calls = %v", fx.calls)
	}
	if !fx.persisted[0].ForceNewOrder {
		t.Errorf("an order without a pinned draft is a new draft, not a merge into a leftover: %+v", fx.persisted[0])
	}
	want := "Thank you Ana! Please pay by BCA transfer.\n\nOrder number: WB-F4BFCE06"
	if out.Reply != want {
		t.Fatalf("reply = %q, want %q (no stale 'reply OK' line)", out.Reply, want)
	}
	if out.OrderID != "f4bfce06-1111-2222-3333-444455556666" || out.OrderAction != "create" {
		t.Errorf("outcome = %+v", out)
	}
	// The draft id is pinned in the state before the send so a retried turn updates the same
	// draft; the state itself is only cleared after the send went out.
	if len(fx.states) != 1 || fx.states[0].PersistedOrderID != "f4bfce06-1111-2222-3333-444455556666" {
		t.Fatalf("state must carry the saved draft id: %+v", fx.states)
	}
	if fx.did("clear") != 0 || out.After == nil {
		t.Fatalf("clear is a post-send action: calls=%v after=%v", fx.calls, out.After != nil)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if fx.did("clear") != 1 {
		t.Fatalf("calls after send = %v", fx.calls)
	}
}

func TestLLMFirstApplyConfirmUpdatesPinnedDraft(t *testing.T) {
	fx := &lfFakeFX{persistID: "aaaaaaaa-1111-2222-3333-444455556666"}
	st := lfConfirmState()
	st.PersistedOrderID = "aaaaaaaa-1111-2222-3333-444455556666"
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Thank you Ana!"}
	plan := lfTestPlan(d, salesbrain.LangID, cartFromState(st), false, "confirm")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if fx.persisted[0].ForceNewOrder || fx.persisted[0].PersistedOrderID != st.PersistedOrderID {
		t.Fatalf("a pinned draft is updated in place: %+v", fx.persisted[0])
	}
	if !strings.HasSuffix(out.Reply, "Nomor pesanan: WB-AAAAAAAA") {
		t.Fatalf("reply = %q", out.Reply)
	}
}

func TestLLMFirstApplyConfirmRetriesWithFreshDraftWhenPinnedOneIsGone(t *testing.T) {
	// The first attempt finds the pinned draft gone and leftovers around (needPick); the second
	// one, a forced new draft, works.
	fx2 := &lfPersistSeq{results: []lfPersistResult{{err: errMultiDraftDisambiguation}, {id: "bbbbbbbb-1111-2222-3333-444455556666"}}}
	st := lfConfirmState()
	st.PersistedOrderID = "gone-draft"
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Thank you!"}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "confirm")
	out, err := lfApplyIntent(context.Background(), fx2, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if len(fx2.seen) != 2 || fx2.seen[0].PersistedOrderID != "gone-draft" || fx2.seen[1].PersistedOrderID != "" || !fx2.seen[1].ForceNewOrder {
		t.Fatalf("persist attempts = %+v", fx2.seen)
	}
	if !strings.HasSuffix(out.Reply, "Order number: WB-BBBBBBBB") {
		t.Fatalf("reply = %q", out.Reply)
	}
}

// lfPersistSeq is an lfFakeFX whose persist answers come from a script.
type lfPersistResult struct {
	id  string
	err error
}
type lfPersistSeq struct {
	lfFakeFX
	results []lfPersistResult
	seen    []orderState
}

func (p *lfPersistSeq) persistOrder(_ context.Context, st orderState) (string, error) {
	p.seen = append(p.seen, st)
	r := p.results[len(p.seen)-1]
	return r.id, r.err
}

func TestLLMFirstApplyConfirmFailureIsHonestAndKeepsTheCart(t *testing.T) {
	fx := &lfFakeFX{persistErr: errors.New("order qty exceeds available stock for line 1")}
	st := lfConfirmState()
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Thank you Ana, your order is confirmed!"}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "confirm")

	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatalf("a failed save is answered, not retried through the job: %v", err)
	}
	if out.Reply != salesbrain.T(salesbrain.TplSystemBusy, salesbrain.LangEN, "Omah Apparel") {
		t.Fatalf("the buyer must not be told the order is confirmed: %q", out.Reply)
	}
	if out.Author != "system" || out.ReplacedReason != "order_persist_failed" || out.OrderID != "" {
		t.Fatalf("outcome = %+v", out)
	}
	if fx.did("clear") != 0 || fx.did("set") != 0 {
		t.Fatalf("the checkout state stays as it was: %v", fx.calls)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.notes) != 1 || fx.notes[0].kind != notifyKindSystemError || !strings.Contains(fx.notes[0].body, notifyReasonOrderSaveFailed) {
		t.Fatalf("the owner is told: %+v", fx.notes)
	}
	if fx.did("pause") != 0 {
		t.Fatal("a failure of ours never pauses the AI")
	}
}

func TestLLMFirstApplyConfirmOnlyWhenCheckoutIsAtConfirm(t *testing.T) {
	for name, st := range map[string]*orderState{
		"no state":                nil,
		"still asking recipient":  lfTunaState(),
		"confirm step, no phone":  func() *orderState { s := lfConfirmState(); s.RecipientPhone = ""; return s }(),
		"complete but wrong step": func() *orderState { s := lfConfirmState(); s.Step = lfStepAskAddressFull; return s }(),
	} {
		fx := &lfFakeFX{}
		d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Great, noted."}
		plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, pendingFieldFromState(st))
		out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fx.did("persist") != 0 || out.OrderID != "" || strings.Contains(out.Reply, "Order number") {
			t.Errorf("%s: nothing may be saved or claimed: calls=%v reply=%q", name, fx.calls, out.Reply)
		}
	}
}

// ── cancel ───────────────────────────────────────────────────────────────────

func lfOpenDraftOrders(t *testing.T) ([]persistedOrder, []salesbrain.OrderSnapshot) {
	t.Helper()
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	orders := []persistedOrder{
		lfTestOrder(t, "f4bfce06-0000-0000-0000-000000000001", "draft", "unpaid", now.Add(-time.Hour), []order.OrderItem{lfTunaItem}, order.ShippingAddress{Name: "Ana", Phone: "+62872839472828"}),
		lfTestOrder(t, "aaaaaaaa-0000-0000-0000-000000000002", "draft", "unpaid", now.Add(-5*time.Hour), []order.OrderItem{lfSalmonItem}, order.ShippingAddress{}),
	}
	return orders, mapOpenOrders(orders, now)
}

func TestLLMFirstApplyCancelDraftUsesTemplateAndClearsState(t *testing.T) {
	fx := &lfFakeFX{}
	all, _ := lfOpenDraftOrders(t)
	orders := all[:1] // the buyer has exactly one open order
	open := mapOpenOrders(orders, time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC))
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "OK, cancelled it for you!"}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open, in.UserText = orders, open, "cancel my order please"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 1 || fx.cancelled[0] != "f4bfce06-0000-0000-0000-000000000001" {
		t.Fatalf("the one open order is cancelled, got %v", fx.cancelled)
	}
	if want := salesbrain.T(salesbrain.TplOrderCancelled, salesbrain.LangEN, "WB-F4BFCE06"); out.Reply != want {
		t.Fatalf("reply = %q, want the deterministic template %q", out.Reply, want)
	}
	if out.ReplacedReason != "cancel_template" || out.OrderID != "f4bfce06-0000-0000-0000-000000000001" || out.OrderAction != "cancel" {
		t.Fatalf("outcome = %+v", out)
	}
	if fx.did("clear") != 1 {
		t.Fatalf("calls = %v", fx.calls)
	}
}

// Two open orders and nothing that names one: the model is asking which one, so nothing is
// cancelled on a guess. The model's own answer stands and the live state is not touched.
func TestLLMFirstApplyCancelWithSeveralOpenOrdersDoesNotGuess(t *testing.T) {
	fx := &lfFakeFX{}
	orders, open := lfOpenDraftOrders(t)
	if len(open) != 2 {
		t.Fatalf("test setup: want two open orders, got %d", len(open))
	}
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "You have two open orders, WB-F4BFCE06 and WB-AAAAAAAA. Which one should I cancel?"}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open, in.UserText = orders, open, "cancel my order"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 || out.Reply != d.Reply || out.OrderID != "" || out.ReplacedReason != "" {
		t.Fatalf("nothing may be cancelled or replaced: calls=%v out=%+v", fx.calls, out)
	}
}

func TestLLMFirstApplyCancelByExplicitRefKeepsTheCart(t *testing.T) {
	fx := &lfFakeFX{}
	orders, open := lfOpenDraftOrders(t)
	st := lfTunaState() // a live cart that is not one of the saved drafts
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "Cancelled."}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "recipient")
	in := lfApplyBase(plan, st)
	in.Orders, in.Open, in.UserText = orders, open, "please cancel wb-aaaaaaaa"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 1 || fx.cancelled[0] != "aaaaaaaa-0000-0000-0000-000000000002" {
		t.Fatalf("the order named in the message is cancelled, got %v", fx.cancelled)
	}
	if fx.did("clear") != 0 {
		t.Fatalf("the buyer's live cart is a different order and must survive: %v", fx.calls)
	}
	if !strings.Contains(out.Reply, "WB-AAAAAAAA") {
		t.Fatalf("reply = %q", out.Reply)
	}
}

func TestLLMFirstApplyCancelUnknownRefDoesNotGuess(t *testing.T) {
	fx := &lfFakeFX{}
	orders, open := lfOpenDraftOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "Cancelled."}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open, in.UserText = orders, open, "cancel WB-DEADBEEF"
	if _, err := lfApplyIntent(context.Background(), fx, in); err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 0 {
		t.Fatalf("an unknown ref must not cancel some other order: %v", fx.cancelled)
	}
}

func TestLLMFirstApplyCancelPaidOrderGoesToTheOwner(t *testing.T) {
	fx := &lfFakeFX{}
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	paid := lfTestOrder(t, "cccccccc-0000-0000-0000-000000000004", "processing", "verified", now.Add(-2*time.Hour), []order.OrderItem{lfTunaItem}, lfFullShip)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "Cancelled!"}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.UserText = []persistedOrder{paid}, "cancel WB-CCCCCCCC"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("cancel") != 0 {
		t.Fatal("a paid order is never cancelled by the bot")
	}
	if want := salesbrain.T(salesbrain.TplCancelPaidOrder, salesbrain.LangEN, "WB-CCCCCCCC", "Omah Apparel"); out.Reply != want {
		t.Fatalf("reply = %q, want %q", out.Reply, want)
	}
	if len(fx.notes) != 1 || fx.notes[0].kind != notifyKindHandoff || !strings.Contains(fx.notes[0].body, "WB-CCCCCCCC") {
		t.Fatalf("the owner must hear about it before the buyer is told: %+v", fx.notes)
	}
	if fx.did("pause") != 0 {
		t.Fatal("handing a cancellation to the owner does not pause the AI")
	}
}

func TestLLMFirstApplyCancelCartOnlyClearsState(t *testing.T) {
	fx := &lfFakeFX{}
	st := lfTunaState()
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "No problem, your cart is empty again."}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "recipient")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("cancel") != 0 || fx.did("clear") != 1 {
		t.Fatalf("calls = %v", fx.calls)
	}
	if out.Reply != "No problem, your cart is empty again." {
		t.Fatalf("with no saved order the model reply is used as is (no stale resume line): %q", out.Reply)
	}
}

func TestLLMFirstApplyCancelAlreadyCancelledIsFine(t *testing.T) {
	fx := &lfFakeFX{cancelErr: sql.ErrNoRows}
	all, _ := lfOpenDraftOrders(t)
	orders := all[:1] // exactly one open order, so the cancel target is unambiguous
	open := mapOpenOrders(orders, time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC))
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "Cancelled."}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open = orders, open
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatalf("a row that is already cancelled is not an error: %v", err)
	}
	if !strings.Contains(out.Reply, "WB-F4BFCE06") {
		t.Fatalf("reply = %q", out.Reply)
	}

	fx = &lfFakeFX{cancelErr: errors.New("db down")}
	if _, err := lfApplyIntent(context.Background(), fx, in); err == nil {
		t.Fatal("a real database error must be returned (the job retries)")
	}
}

// ── continue / new order ─────────────────────────────────────────────────────

func TestLLMFirstApplyContinueOrderRebuildsTheCheckout(t *testing.T) {
	fx := &lfFakeFX{}
	orders, open := lfOpenDraftOrders(t) // newest: tuna draft with recipient but no address
	d := salesbrain.Decision{Intent: salesbrain.IntentContinueOrder, Reply: "Great, let's finish your order."}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open, in.UserText = orders, open, "yes please continue"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 {
		t.Fatalf("the checkout is rebuilt in Redis: %v", fx.calls)
	}
	st := fx.states[0]
	if st.PersistedOrderID != "f4bfce06-0000-0000-0000-000000000001" || st.Step != lfStepAskAddressFull || st.RecipientName != "Ana" || len(st.Items) != 1 {
		t.Fatalf("rebuilt state = %+v", st)
	}
	for _, want := range []string{"Great, let's finish your order.", "Order summary", "Yellow Tail Tuna Portion 100 Gram", salesbrain.ResumeLineFor("address", salesbrain.LangEN)} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, out.Reply)
		}
	}
}

func TestLLMFirstApplyContinueOrderByExplicitRef(t *testing.T) {
	fx := &lfFakeFX{}
	orders, open := lfOpenDraftOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentContinueOrder, Reply: "Sure."}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open, in.UserText = orders, open, "continue WB-AAAAAAAA"
	if _, err := lfApplyIntent(context.Background(), fx, in); err != nil {
		t.Fatal(err)
	}
	if len(fx.states) != 1 || fx.states[0].PersistedOrderID != "aaaaaaaa-0000-0000-0000-000000000002" || fx.states[0].Step != lfStepAskRecipient {
		t.Fatalf("the order named by the buyer is restored: %+v", fx.states)
	}
}

func TestLLMFirstApplyContinueOrderLeavesPaymentStageAndLiveCartAlone(t *testing.T) {
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	waiting := lfTestOrder(t, "bbbbbbbb-0000-0000-0000-000000000003", "draft", "unpaid", now.Add(-time.Hour), []order.OrderItem{lfSalmonItem}, lfFullShip)
	orders := []persistedOrder{waiting}
	open := mapOpenOrders(orders, now)
	d := salesbrain.Decision{Intent: salesbrain.IntentContinueOrder, Reply: "Please pay by BCA transfer to complete it."}

	fx := &lfFakeFX{}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open = orders, open
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 || out.Reply != plan.Reply {
		t.Fatalf("nothing is left to collect for an order that waits for payment: calls=%v reply=%q", fx.calls, out.Reply)
	}

	// A buyer with a live cart who says "continue" means that cart.
	fx = &lfFakeFX{}
	drafts, draftOpen := lfOpenDraftOrders(t)
	st := lfTunaState()
	plan = lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "recipient")
	in = lfApplyBase(plan, st)
	in.Orders, in.Open = drafts, draftOpen
	if _, err := lfApplyIntent(context.Background(), fx, in); err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("a live cart must not be replaced by an old draft: %v", fx.calls)
	}
}

func TestLLMFirstApplyNewOrderClearsStateAndMentionsParkedOrder(t *testing.T) {
	orders, open := lfOpenDraftOrders(t)
	st := lfTunaState()
	d := salesbrain.Decision{Intent: salesbrain.IntentNewOrder, Reply: "Sure, starting a new order."}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "recipient")
	in := lfApplyBase(plan, st)
	in.Orders, in.Open = orders, open

	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("clear") != 1 || fx.did("cancel") != 0 {
		t.Fatalf("the saved draft stays, only the live cart goes: %v", fx.calls)
	}
	wantTail := salesbrain.T(salesbrain.TplNewOrderParked, salesbrain.LangEN, "WB-F4BFCE06")
	if out.Reply != "Sure, starting a new order.\n\n"+wantTail {
		t.Fatalf("reply = %q", out.Reply)
	}
	if strings.Contains(out.Reply, "Back to your order") {
		t.Fatal("the old cart's resume line must not be appended to a new order")
	}

	// The model already named the order: nothing is appended.
	d.Reply = "Sure! Order WB-F4BFCE06 stays saved. What would you like?"
	plan = lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in.Plan = plan
	out, err = lfApplyIntent(context.Background(), &lfFakeFX{}, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reply != d.Reply {
		t.Fatalf("reply = %q", out.Reply)
	}

	// Nothing saved to park: the model reply is enough.
	in.Orders, in.Open = nil, nil
	out, err = lfApplyIntent(context.Background(), &lfFakeFX{}, in)
	if err != nil || out.Reply != d.Reply {
		t.Fatalf("reply = %q err=%v", out.Reply, err)
	}
}

// ── handoff ──────────────────────────────────────────────────────────────────

func TestLLMFirstApplyTakeoverPausesOnlyAfterTheSend(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentComplaint, Reply: "I'm sorry about that. I'm passing this to the team now.",
		Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffTakeover, Reason: "item arrived broken"}}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.UserText = "my steak arrived broken, I want a refund"

	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("pause and notification happen after the reply went out: %v", fx.calls)
	}
	if !strings.Contains(out.Reply, "I'm passing this to the team now.") || !strings.Contains(out.Reply, "They usually reply during opening hours (08:00 - 19:00 GMT +7).") {
		t.Fatalf("the buyer is told honestly, with the opening hours from the profile: %q", out.Reply)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.paused) != 1 || fx.paused[0] != HandoffReasonHumanRequest {
		t.Fatalf("paused = %v", fx.paused)
	}
	if len(fx.notes) != 1 || fx.notes[0].kind != notifyKindHandoff || fx.notes[0].title != notifyTitleHumanRequest ||
		!strings.Contains(fx.notes[0].body, "item arrived broken") || !strings.Contains(fx.notes[0].body, "refund") {
		t.Fatalf("notes = %+v", fx.notes)
	}
}

func TestLLMFirstApplyTakeoverSkipsHoursWhenThereAreNone(t *testing.T) {
	d := salesbrain.Decision{Intent: salesbrain.IntentHumanRequest, Reply: "Sure, connecting you to the team."}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.OpeningHours = ""
	out, err := lfApplyIntent(context.Background(), &lfFakeFX{}, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reply != "Sure, connecting you to the team." {
		t.Fatalf("no hours configured, nothing is promised: %q", out.Reply)
	}
}

func TestLLMFirstApplyHumanRequestIsATakeoverEvenWithoutHandoffField(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentHumanRequest, Reply: "Sure, connecting you to the team."}
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.paused) != 1 || fx.did("notify") != 1 {
		t.Fatalf("a buyer who asked for a person must really be handed over: calls=%v", fx.calls)
	}
}

func TestLLMFirstApplyTakeoverStillNotifiesWhenPauseFails(t *testing.T) {
	fx := &lfFakeFX{pauseErr: errors.New("db down")}
	d := salesbrain.Decision{Intent: salesbrain.IntentHumanRequest, Reply: "Sure.", Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffTakeover}}
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(lfTestPlan(d, salesbrain.LangEN, nil, false, ""), nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := lfRunAfter(t, out); err == nil {
		t.Fatal("the pause error is reported to the job")
	}
	if fx.did("notify") != 1 {
		t.Fatal("the owner is told even when the pause itself failed")
	}
}

func TestLLMFirstApplyNoteHandoffNotifiesOnly(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentQuestion, Reply: "I don't have that data yet; I'll ask the team.",
		Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffNote, Reason: "buyer asks for a bulk discount"}}
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(lfTestPlan(d, salesbrain.LangEN, nil, false, ""), nil))
	if err != nil {
		t.Fatal(err)
	}
	if out.Reply != d.Reply {
		t.Fatalf("a soft handoff keeps the model reply: %q", out.Reply)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if fx.did("pause") != 0 || len(fx.notes) != 1 || fx.notes[0].title != notifyTitleNoAnswer || !strings.Contains(fx.notes[0].body, "bulk discount") {
		t.Fatalf("calls=%v notes=%+v", fx.calls, fx.notes)
	}
}

func TestLLMFirstApplyHandoffReasonIsRedactedAndShort(t *testing.T) {
	fx := &lfFakeFX{}
	long := "buyer wants a refund, call 0812 3456 7890 or mail ana@example.com " + strings.Repeat("blah ", 100)
	d := salesbrain.Decision{Intent: salesbrain.IntentComplaint, Reply: "Sorry.", Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffNote, Reason: long}}
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(lfTestPlan(d, salesbrain.LangEN, nil, false, ""), nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = lfRunAfter(t, out)
	if len(fx.notes) != 1 {
		t.Fatalf("notes = %+v", fx.notes)
	}
	body := fx.notes[0].body
	for _, leak := range []string{"0812 3456 7890", "ana@example.com"} {
		if strings.Contains(body, leak) {
			t.Errorf("model reason leaked %q into the owner notification: %s", leak, body)
		}
	}
	if len([]rune(body)) > 600 {
		t.Errorf("owner notification body is %d runes, expected a short summary", len([]rune(body)))
	}
}

// ── plain answers ────────────────────────────────────────────────────────────

func TestLLMFirstApplyPlainIntentsSendThePlanReplyUntouched(t *testing.T) {
	for _, intent := range []salesbrain.Intent{salesbrain.IntentGreeting, salesbrain.IntentQuestion, salesbrain.IntentBrowse, salesbrain.IntentRecommend, salesbrain.IntentPaymentProof, salesbrain.IntentOther} {
		fx := &lfFakeFX{}
		d := salesbrain.Decision{Intent: intent, Reply: "Here is what we have."}
		plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(lfTunaState()), false, "recipient")
		out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, lfTunaState()))
		if err != nil {
			t.Fatalf("%s: %v", intent, err)
		}
		if len(fx.calls) != 0 || out.Reply != plan.Reply || out.After != nil {
			t.Errorf("%s: calls=%v reply=%q", intent, fx.calls, out.Reply)
		}
		if !strings.HasSuffix(out.Reply, salesbrain.ResumeLineFor("recipient", salesbrain.LangEN)) {
			t.Errorf("%s: a question mid-checkout resumes the pending field: %q", intent, out.Reply)
		}
	}
}

// ── pure helpers ─────────────────────────────────────────────────────────────

func TestLLMFirstConfusionCounter(t *testing.T) {
	if n, reset := lfNextConfused(0, salesbrain.IntentOther); n != 1 || reset {
		t.Fatalf("other bumps the counter, got %d reset=%v", n, reset)
	}
	if n, reset := lfNextConfused(1, salesbrain.IntentOther); n != 2 || reset {
		t.Fatalf("got %d reset=%v", n, reset)
	}
	if _, reset := lfNextConfused(2, salesbrain.IntentQuestion); !reset {
		t.Fatal("any understood intent resets the counter")
	}
}

func TestLLMFirstHandoffLevel(t *testing.T) {
	cases := []struct {
		d    salesbrain.Decision
		want salesbrain.HandoffLevel
	}{
		{salesbrain.Decision{Intent: salesbrain.IntentQuestion}, ""},
		{salesbrain.Decision{Intent: salesbrain.IntentQuestion, Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffNote}}, salesbrain.HandoffNote},
		{salesbrain.Decision{Intent: salesbrain.IntentComplaint, Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffTakeover}}, salesbrain.HandoffTakeover},
		{salesbrain.Decision{Intent: salesbrain.IntentHumanRequest}, salesbrain.HandoffTakeover},
		{salesbrain.Decision{Intent: salesbrain.IntentHumanRequest, Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffNote}}, salesbrain.HandoffTakeover},
	}
	for i, c := range cases {
		if got := lfHandoffLevel(c.d); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

// ── early draft (spec §4.2) ──────────────────────────────────────────────────

func TestLLMFirstEarlyDraftIsWrittenAfterTheSendAndPinnedInTheState(t *testing.T) {
	fx := &lfFakeFX{earlyID: "d1000000-0000-0000-0000-000000000001"}
	cart := []salesbrain.OrderLine{lfTunaLine()}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added 1x tuna."}, salesbrain.LangEN, cart, true, "")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil))
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("early") != 0 {
		t.Fatalf("no DB write before the reply is sent: %v", fx.calls)
	}
	if out.After == nil {
		t.Fatal("the draft write must run in After")
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatalf("After must not fail the turn: %v", err)
	}
	if len(fx.early) != 1 || !fx.early[0].ForceNewOrder || fx.early[0].PersistedOrderID != "" {
		t.Fatalf("a fresh cart is a new draft (ForceNewOrder, nothing pinned): %+v", fx.early)
	}
	last := fx.states[len(fx.states)-1]
	if last.PersistedOrderID != "d1000000-0000-0000-0000-000000000001" || last.ForceNewOrder {
		t.Fatalf("the draft id must be pinned in the state: %+v", last)
	}
	if len(last.Items) != 1 {
		t.Fatalf("the pinned state keeps the cart: %+v", last)
	}
}

func TestLLMFirstEarlyDraftAmendUpdatesThePinnedDraft(t *testing.T) {
	fx := &lfFakeFX{earlyID: "d1000000-0000-0000-0000-000000000001"}
	st := lfTunaState()
	st.PersistedOrderID = "d1000000-0000-0000-0000-000000000001"
	cart := []salesbrain.OrderLine{lfTunaLine()}
	cart[0].Qty = 3
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentAmend, Reply: "Now 3."}, salesbrain.LangEN, cart, true, "recipient")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.early) != 1 || fx.early[0].PersistedOrderID != st.PersistedOrderID || fx.early[0].ForceNewOrder {
		t.Fatalf("an amend updates the pinned draft: %+v", fx.early)
	}
	if fx.early[0].Items[0].Qty != 3 {
		t.Fatalf("the draft gets the new quantity: %+v", fx.early[0].Items)
	}
	if last := fx.states[len(fx.states)-1]; last.PersistedOrderID != st.PersistedOrderID {
		t.Fatalf("pin kept: %+v", last)
	}
}

func TestLLMFirstEarlyDraftNewOrderKeepsTheParkedDraft(t *testing.T) {
	orders, open := lfOpenDraftOrders(t)
	fx := &lfFakeFX{earlyID: "d2000000-0000-0000-0000-000000000002"}
	cart := []salesbrain.OrderLine{lfTunaLine()}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added 1x tuna."}, salesbrain.LangEN, cart, true, "")
	in := lfApplyBase(plan, nil) // the cart was cleared by new_order: no live state
	in.Orders, in.Open = orders, open
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if st := fx.states[0]; !st.ForceNewOrder || st.PersistedOrderID != "" {
		t.Fatalf("the state of a fresh cart says ForceNewOrder: %+v", st)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.early) != 1 || !fx.early[0].ForceNewOrder || fx.early[0].PersistedOrderID != "" {
		t.Fatalf("the parked draft must not be merged: %+v", fx.early)
	}
	if fx.did("cancel") != 0 {
		t.Fatalf("a parked draft is never cancelled: %v", fx.calls)
	}
}

func TestLLMFirstEarlyDraftFailureNotifiesTheOwnerAndNeverBlocks(t *testing.T) {
	fx := &lfFakeFX{earlyErr: errors.New("db down")}
	cart := []salesbrain.OrderLine{lfTunaLine()}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added 1x tuna."}, salesbrain.LangEN, cart, true, "")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil))
	if err != nil {
		t.Fatal(err)
	}
	if out.Reply != plan.Reply || out.Author != "ai" {
		t.Fatalf("a failing draft write must not change the reply: %+v", out)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatalf("a failing draft write must not fail the turn: %v", err)
	}
	if fx.did("notify") != 1 || fx.notes[0].kind != notifyKindSystemError {
		t.Fatalf("the owner must hear about it: %+v", fx.notes)
	}
	if len(fx.states) != 1 {
		t.Fatalf("no pin without a draft: %+v", fx.states)
	}
}

func TestLLMFirstEarlyDraftPinnedDraftGoneIsSavedAsFreshDraft(t *testing.T) {
	fx := &lfFakeFX{earlyPick: []bool{true}, earlyID: "d3000000-0000-0000-0000-000000000003"}
	st := lfTunaState()
	st.PersistedOrderID = "gone"
	cart := []salesbrain.OrderLine{lfTunaLine()}
	cart[0].Qty = 2
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentAmend, Reply: "Now 2."}, salesbrain.LangEN, cart, true, "recipient")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.early) != 2 || !fx.early[1].ForceNewOrder || fx.early[1].PersistedOrderID != "" {
		t.Fatalf("a pick-list answer retries as a fresh draft: %+v", fx.early)
	}
	if last := fx.states[len(fx.states)-1]; last.PersistedOrderID != "d3000000-0000-0000-0000-000000000003" {
		t.Fatalf("pin = %+v", last)
	}
}

func TestLLMFirstEarlyDraftSkippedWhenNothingChanged(t *testing.T) {
	fx := &lfFakeFX{}
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Which salmon?"}, salesbrain.LangEN, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, lfTunaState()))
	if err != nil {
		t.Fatal(err)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if fx.did("early") != 0 {
		t.Fatalf("no cart change, no draft write: %v", fx.calls)
	}
}

func TestLLMFirstEarlyDraftKeepsTheHandoffAfterStep(t *testing.T) {
	fx := &lfFakeFX{}
	cart := []salesbrain.OrderLine{lfTunaLine()}
	d := salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added.", Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffNote, Reason: "x"}}
	plan := lfTestPlan(d, salesbrain.LangEN, cart, true, "")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if fx.did("early") != 1 || fx.did("notify") != 1 {
		t.Fatalf("both the draft write and the handoff note must run: %v", fx.calls)
	}
}
