package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/order"
)

// Incident 21 Oct 2026 (omah_apparel): see docs/LLM_FIRST_PIPELINE.md.

const (
	incOldDraft  = "bbdb5fd4-0000-0000-0000-000000000001" // draft, unpaid, last touched 3 days+ ago
	incOldUnpaid = "7d23ae49-0000-0000-0000-000000000002" // unpaid, from 18 Oct
	incNewOrder  = "79d3e7e5-0000-0000-0000-000000000003" // created minutes ago
)

func lfIncidentOrders(t *testing.T) (orders []persistedOrder, now time.Time) {
	t.Helper()
	now = time.Date(2026, 10, 21, 12, 40, 0, 0, time.UTC) // 19:40 WIB
	orders = []persistedOrder{
		lfTestOrder(t, incNewOrder, "draft", "unpaid", now.Add(-20*time.Minute), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, incOldUnpaid, "draft", "unpaid", now.Add(-3*24*time.Hour-time.Hour), []order.OrderItem{lfSalmonItem}, lfFullShip),
		lfTestOrder(t, incOldDraft, "draft", "unpaid", now.Add(-3*24*time.Hour-2*time.Hour), []order.OrderItem{lfTunaItem}, order.ShippingAddress{}),
	}
	return orders, now
}

// ── Bug 1: the list that answers the buyer has no age limit ──────────────────

func TestLLMFirstUnfinishedOrdersIncludeOldDraftsAndPaidOnes(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 40, 0, 0, time.UTC)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	orders := []persistedOrder{
		lfTestOrder(t, "11111111-0000-0000-0000-000000000001", "draft", "unpaid", ago(1), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "22222222-0000-0000-0000-000000000002", "draft", "unpaid", ago(100), []order.OrderItem{lfSalmonItem}, order.ShippingAddress{}),
		lfTestOrder(t, "33333333-0000-0000-0000-000000000003", "draft", "proof_submitted", ago(5), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "44444444-0000-0000-0000-000000000004", "processing", "verified", ago(6), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "55555555-0000-0000-0000-000000000005", "paid", "verified", ago(7), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "66666666-0000-0000-0000-000000000006", "shipped", "verified", ago(8), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "77777777-0000-0000-0000-000000000007", "completed", "verified", ago(9), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "88888888-0000-0000-0000-000000000008", "cancelled", "unpaid", ago(10), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "99999999-0000-0000-0000-000000000009", "draft", "unpaid", ago(2), nil, order.ShippingAddress{}), // no items yet
	}
	got, more := mapUnfinishedOrders(orders)
	if more != 0 || len(got) != 6 {
		t.Fatalf("want 6 unfinished (shipped, completed, cancelled are not pending), got %d more=%d: %+v", len(got), more, got)
	}
	byRef := map[string]salesbrain.OrderSnapshot{}
	for _, o := range got {
		byRef[o.Ref] = o
	}
	if o, ok := byRef["WB-22222222"]; !ok || o.Paid {
		t.Errorf("a 100-hour-old unpaid draft is still pending and unpaid: %+v", o)
	}
	if o := byRef["WB-33333333"]; o.Status != salesbrain.StatusAwaitingReview || !o.Paid {
		t.Errorf("proof submitted = awaiting review, paid: %+v", o)
	}
	for _, ref := range []string{"WB-44444444", "WB-55555555"} {
		if o := byRef[ref]; o.Status != salesbrain.StatusProcessing || !o.Paid {
			t.Errorf("%s should be processing and paid: %+v", ref, o)
		}
	}
	if _, ok := byRef["WB-99999999"]; !ok {
		t.Error("a draft without items is still an unfinished order")
	}
	if got[0].Ref != "WB-11111111" {
		t.Errorf("newest first, got %s", got[0].Ref)
	}

	// The reminder keeps its own rule: unpaid drafts with items of the last 72 hours only.
	remind := mapRemindOrders(orders, now)
	if len(remind) != 1 || remind[0].Ref != "WB-11111111" {
		t.Errorf("reminder selection = %+v", remind)
	}
}

func TestLLMFirstUnfinishedOrdersCapAndMoreCount(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 40, 0, 0, time.UTC)
	var orders []persistedOrder
	for i := 0; i < 12; i++ {
		id := strings.Repeat(string(rune('a'+i)), 8) + "-0000-0000-0000-000000000000"
		orders = append(orders, lfTestOrder(t, id, "draft", "unpaid", now.Add(-time.Duration(i+1)*24*time.Hour), []order.OrderItem{lfTunaItem}, order.ShippingAddress{}))
	}
	got, more := mapUnfinishedOrders(orders)
	if len(got) != salesbrain.MaxUnfinishedListed || more != 2 {
		t.Fatalf("len=%d more=%d", len(got), more)
	}
}

// The 19:41 case: the buyer has three unfinished orders, two of them older than 72 hours. The
// STATE the model reads lists all three (it used to say "none"); only the reminder stays at 72h.
func TestLLMFirstPlanInputListsOldOrdersAndKeepsReminderRule(t *testing.T) {
	orders, now := lfIncidentOrders(t)
	in := lfShellInputs(t)
	in.Now = now
	in.Orders = orders[1:] // the new order was cancelled at 19:40; two old ones remain
	got := lfBuildPlanInput(in)
	if len(got.State.OpenOrders) != 2 || got.State.OpenOrders[0].Ref != "WB-7D23AE49" || got.State.OpenOrders[1].Ref != "WB-BBDB5FD4" {
		t.Fatalf("OpenOrders must list both old orders: %+v", got.State.OpenOrders)
	}
	if len(got.State.RemindOrders) != 0 || got.State.RemindAllowed {
		t.Fatalf("the 72-hour reminder rule is unchanged: remind=%+v allowed=%v", got.State.RemindOrders, got.State.RemindAllowed)
	}
	_, user := salesbrain.BuildPrompt(salesbrain.PromptInput{State: got.State})
	if !strings.Contains(user, "pesanan_belum_selesai: ADA 2 pesanan") || !strings.Contains(user, "WB-BBDB5FD4") {
		t.Fatalf("the prompt must show the orders:\n%s", user)
	}
}

// ── Bug 2: cancel all ────────────────────────────────────────────────────────

func lfCancelAllIn(t *testing.T, orders []persistedOrder, text string, d salesbrain.Decision) lfApplyIn {
	t.Helper()
	plan := lfTestPlan(d, salesbrain.LangEN, nil, false, "")
	in := lfApplyBase(plan, nil)
	in.Orders, in.Open, in.UserText = orders, lfTestOpen(orders), text
	return in
}

func TestLLMFirstCancelAllAsksForConfirmationAndCancelsNothing(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, CancelScope: salesbrain.CancelAll, Reply: "Done, all cancelled!"}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders, "i want cancel all my pending orders", d))
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("cancel") != 0 || len(fx.pending) != 1 || len(fx.pending[0]) != 3 {
		t.Fatalf("nothing is cancelled yet, the three ids are stored: calls=%v pending=%v", fx.calls, fx.pending)
	}
	for _, want := range []string{"WB-79D3E7E5", "WB-7D23AE49", "WB-BBDB5FD4", "cancel all of these", "Reply OK"} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("confirmation missing %q:\n%s", want, out.Reply)
		}
	}
	if strings.Contains(out.Reply, "Done") || out.ReplacedReason != lfReplacedCancelAllAsk || out.Author != "system" {
		t.Errorf("the model's claim must be replaced: %+v", out)
	}
}

func TestLLMFirstCancelAllStorageFailureFailsClosed(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, CancelScope: salesbrain.CancelAll, Reply: "Done!"}
	fx := &lfFakeFX{pendingErr: errors.New("redis down")}
	out, err := lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders, "cancel all my orders", d))
	if err != nil || fx.did("cancel") != 0 {
		t.Fatalf("err=%v calls=%v", err, fx.calls)
	}
	if !strings.Contains(out.Reply, "problem") || strings.Contains(out.Reply, "Done") {
		t.Fatalf("the buyer is told honestly: %q", out.Reply)
	}
}

func TestLLMFirstCancelAllConfirmedCancelsEveryUnpaidOrder(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Great."}
	in := lfCancelAllIn(t, orders, "yes", d)
	in.PendingCancel = []string{incNewOrder, incOldUnpaid, incOldDraft}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 3 {
		t.Fatalf("all three unpaid orders are cancelled, got %v", fx.cancelled)
	}
	for _, want := range []string{"WB-79D3E7E5", "WB-7D23AE49", "WB-BBDB5FD4", "are cancelled"} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q: %s", want, out.Reply)
		}
	}
	if out.ReplacedReason != lfReplacedCancelAllDone || out.OrderAction != "cancel_all" || !out.ActionRan {
		t.Errorf("outcome = %+v", out)
	}
	if err := lfRunAfter(t, out); err != nil || fx.did("clearpending") != 1 {
		t.Fatalf("the pending question is dropped after the send: calls=%v err=%v", fx.calls, err)
	}
}

func TestLLMFirstCancelAllConfirmedSendsPaidOrdersToTheTeam(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 40, 0, 0, time.UTC)
	orders := []persistedOrder{
		lfTestOrder(t, "11111111-0000-0000-0000-000000000001", "draft", "unpaid", now.Add(-time.Hour), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "22222222-0000-0000-0000-000000000002", "paid", "verified", now.Add(-2*time.Hour), []order.OrderItem{lfTunaItem}, lfFullShip),
		lfTestOrder(t, "33333333-0000-0000-0000-000000000003", "shipped", "verified", now.Add(-3*time.Hour), []order.OrderItem{lfTunaItem}, lfFullShip),
	}
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Great."}
	in := lfCancelAllIn(t, orders, "ok", d)
	in.PendingCancel = []string{orders[0].ID, orders[1].ID, orders[2].ID}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 1 || fx.cancelled[0] != orders[0].ID {
		t.Fatalf("only the unpaid order is cancelled by code (not the paid, not the shipped): %v", fx.cancelled)
	}
	if len(fx.notes) != 1 || fx.notes[0].title != notifyTitleCancelPaid || !strings.Contains(fx.notes[0].body, "WB-22222222") {
		t.Fatalf("the paid order goes to the team: %+v", fx.notes)
	}
	if !strings.Contains(out.Reply, "WB-11111111") || !strings.Contains(out.Reply, "has already been paid") || strings.Contains(out.Reply, "WB-33333333") {
		t.Fatalf("reply = %q", out.Reply)
	}
}

func TestLLMFirstCancelAllRetryReportsAlreadyCancelledIds(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Great."}
	in := lfCancelAllIn(t, orders[2:], "yes", d) // the other two are gone from the active list: an earlier attempt cancelled them
	in.PendingCancel = []string{incNewOrder, incOldUnpaid, incOldDraft}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 1 || !strings.Contains(out.Reply, "WB-79D3E7E5") || !strings.Contains(out.Reply, "WB-BBDB5FD4") {
		t.Fatalf("cancelled=%v reply=%q", fx.cancelled, out.Reply)
	}
}

func TestLLMFirstCancelAllErrorIsReturnedForRetry(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	in := lfCancelAllIn(t, orders, "yes", salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Great."})
	in.PendingCancel = []string{incNewOrder}
	if _, err := lfApplyIntent(context.Background(), &lfFakeFX{cancelErr: errors.New("db down")}, in); err == nil {
		t.Fatal("a failed cancel must be returned so the job retries")
	}
}

func TestLLMFirstPendingCancelNotConfirmedIsDroppedAndCancelsNothing(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	for name, c := range map[string]struct {
		d    salesbrain.Decision
		text string
	}{
		"declined":            {salesbrain.Decision{Intent: salesbrain.IntentOther, Reply: "No problem, I keep them."}, "no keep them"},
		"moved on":            {salesbrain.Decision{Intent: salesbrain.IntentQuestion, Reply: "Yes, it is frozen."}, "is it frozen?"},
		"confirm but a hedge": {salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Ok."}, "ok but only the first one"},
	} {
		in := lfCancelAllIn(t, orders, c.text, c.d)
		in.PendingCancel = []string{incNewOrder, incOldUnpaid, incOldDraft}
		fx := &lfFakeFX{}
		if _, err := lfApplyIntent(context.Background(), fx, in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fx.did("cancel") != 0 || fx.did("clearpending") != 1 {
			t.Errorf("%s: nothing may be cancelled and the question is dropped: %v", name, fx.calls)
		}
	}
}

func TestLLMFirstCancelAllWithOneOrderCancelsItLikeASingleCancel(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, CancelScope: salesbrain.CancelAll, Reply: "ok"}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders[:1], "cancel all my orders", d))
	if err != nil || len(fx.cancelled) != 1 || len(fx.pending) != 0 || out.ReplacedReason != lfReplacedCancel {
		t.Fatalf("err=%v calls=%v out=%+v", err, fx.calls, out)
	}
}

func TestLLMFirstCancelWithRefStillCancelsOnlyThatOrder(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, CancelScope: salesbrain.CancelOne, Reply: "ok"}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders, "cancel WB-7D23AE49", d))
	if err != nil || len(fx.cancelled) != 1 || fx.cancelled[0] != incOldUnpaid || !strings.Contains(out.Reply, "WB-7D23AE49") {
		t.Fatalf("err=%v cancelled=%v reply=%q", err, fx.cancelled, out.Reply)
	}
}

func TestLLMFirstCancelPickListNeverCancelsTheNewest(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "Cancelled."}
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders, "cancel my order", d))
	if err != nil || fx.did("cancel") != 0 || out.ReplacedReason != lfReplacedCancelPick {
		t.Fatalf("err=%v calls=%v out=%+v", err, fx.calls, out)
	}
}

// ── Bug 3: takeover after a successful action ────────────────────────────────

// The 19:43 case: the cancel succeeded, the model asked for a takeover anyway. The AI must stay
// active, the reply must not carry the opening-hours sentence, the owner gets a note.
func TestLLMFirstTakeoverAfterSuccessfulCancelIsDowngradedToNote(t *testing.T) {
	orders, _ := lfIncidentOrders(t)
	d := salesbrain.Decision{Intent: salesbrain.IntentCancel, Reply: "Cancelling now.",
		Handoff: &salesbrain.Handoff{Level: salesbrain.HandoffTakeover, Reason: "Customer repeatedly requested to cancel orders that are not present in the current system state."}}
	fx := &lfFakeFX{}
	in := lfCancelAllIn(t, orders, "i want cancel WB-7D23AE49", d)
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.cancelled) != 1 || fx.cancelled[0] != incOldUnpaid {
		t.Fatalf("cancelled = %v", fx.cancelled)
	}
	want := salesbrain.T(salesbrain.TplOrderCancelled, salesbrain.LangEN, "WB-7D23AE49")
	if out.Reply != want || strings.Contains(out.Reply, "opening hours") {
		t.Fatalf("reply must be the cancel template only, got %q", out.Reply)
	}
	if err := lfRunAfter(t, out); err != nil {
		t.Fatal(err)
	}
	if len(fx.paused) != 0 || fx.did("pause") != 0 {
		t.Fatalf("the AI must stay active (ai_handled stays true): %v", fx.calls)
	}
	if len(fx.notes) != 1 || fx.notes[0].title != notifyTitleNoAnswer || !strings.Contains(fx.notes[0].body, "AI tetap aktif") ||
		!strings.Contains(fx.notes[0].body, "Customer repeatedly requested") {
		t.Fatalf("the owner gets one note carrying the model's reason: %+v", fx.notes)
	}
}

func TestLLMFirstTakeoverAfterOtherSuccessfulActionsIsDowngraded(t *testing.T) {
	handoff := &salesbrain.Handoff{Level: salesbrain.HandoffTakeover, Reason: "confused"}

	// cancel-all question
	orders, _ := lfIncidentOrders(t)
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders, "cancel all",
		salesbrain.Decision{Intent: salesbrain.IntentCancel, CancelScope: salesbrain.CancelAll, Reply: "x", Handoff: handoff}))
	if err != nil {
		t.Fatal(err)
	}
	_ = lfRunAfter(t, out)
	if fx.did("pause") != 0 || strings.Contains(out.Reply, "opening hours") {
		t.Errorf("cancel-all question: the AI must stay on to receive the OK: %v / %q", fx.calls, out.Reply)
	}

	// order saved
	fx = &lfFakeFX{}
	st := lfConfirmState()
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Confirmed.", Handoff: handoff}
	in := lfApplyBase(lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "confirm"), st)
	in.UserText = "OK"
	out, err = lfApplyIntent(context.Background(), fx, in)
	if err != nil || out.OrderID == "" {
		t.Fatalf("order must be saved: %+v err=%v", out, err)
	}
	_ = lfRunAfter(t, out)
	if fx.did("pause") != 0 || strings.Contains(out.Reply, "opening hours") {
		t.Errorf("order create: %v / %q", fx.calls, out.Reply)
	}

	// cart changed
	fx = &lfFakeFX{}
	cart := cartFromState(lfTunaState())
	d = salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Added.", Handoff: handoff}
	out, err = lfApplyIntent(context.Background(), fx, lfApplyBase(lfTestPlan(d, salesbrain.LangEN, cart, true, "recipient"), nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = lfRunAfter(t, out)
	if fx.did("pause") != 0 || strings.Contains(out.Reply, "opening hours") {
		t.Errorf("cart update: %v / %q", fx.calls, out.Reply)
	}
}

func TestLLMFirstTakeoverStaysWhenNoActionRanOrBuyerAsksForAPerson(t *testing.T) {
	handoff := &salesbrain.Handoff{Level: salesbrain.HandoffTakeover, Reason: "wants a person"}

	// No action ran (a complaint).
	fx := &lfFakeFX{}
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(lfTestPlan(
		salesbrain.Decision{Intent: salesbrain.IntentComplaint, Reply: "Sorry.", Handoff: handoff}, salesbrain.LangEN, nil, false, ""), nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = lfRunAfter(t, out)
	if fx.did("pause") != 1 || !strings.Contains(out.Reply, "opening hours") {
		t.Errorf("no action ran: takeover still applies: %v / %q", fx.calls, out.Reply)
	}

	// The buyer asks for a person while a cancel ran the same turn (human_request wins).
	orders, _ := lfIncidentOrders(t)
	fx = &lfFakeFX{}
	out, err = lfApplyIntent(context.Background(), fx, lfCancelAllIn(t, orders, "let me talk to a person",
		salesbrain.Decision{Intent: salesbrain.IntentHumanRequest, Reply: "Sure.", Handoff: handoff}))
	if err != nil {
		t.Fatal(err)
	}
	_ = lfRunAfter(t, out)
	if fx.did("pause") != 1 || !strings.Contains(out.Reply, "opening hours") {
		t.Errorf("explicit human request: takeover stays: %v / %q", fx.calls, out.Reply)
	}
}
