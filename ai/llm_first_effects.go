package ai

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"encore.dev/rlog"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
)

// Intent side effects of the LLM-first pipeline (spec §3.1 step "adapter pesanan", §4.2, §4.4).
//
// planTurn only decides; lfApplyIntent turns the decision into writes through lfFX and into the
// text that is sent. The split keeps the rules testable without Redis or the database, and it keeps
// the guarantee of the shadow mode structural: the shadow path never receives an lfFX.
//
// The reply is the model's validated text unless a rule replaces or extends it. Anything the
// buyer reads that the model did not write comes from salesbrain templates, so it follows the
// language lock.

// lfFX is the write side of one LLM-first turn.
type lfFX interface {
	setState(ctx context.Context, st orderState)
	clearState(ctx context.Context)
	persistOrder(ctx context.Context, st orderState) (string, error)
	// persistEarlyDraft saves the cart as a draft order without shipping data
	// (persistDraftOrderEarly, as the legacy flow does at ask_recipient). needPick means several
	// drafts compete and none is pinned: nothing was written.
	persistEarlyDraft(ctx context.Context, st orderState) (id string, needPick bool, err error)
	cancelOrder(ctx context.Context, orderID string) error
	// setPendingCancel stores the order ids a "cancel all" waits for the buyer's OK on; an error
	// means the question can not be answered later, so it must not be asked. clearPendingCancel
	// drops it (best effort).
	setPendingCancel(ctx context.Context, orderIDs []string) error
	clearPendingCancel(ctx context.Context)
	pause(ctx context.Context, reason string) error
	notify(ctx context.Context, kind, title, body string)
}

// lfApplyIn is what lfApplyIntent needs besides the plan.
type lfApplyIn struct {
	Plan         turnPlan
	UserText     string
	Shop         string // shopLabel
	OpeningHours string
	State        *orderState              // the checkout state loaded for this turn (nil: none)
	Catalog      []dbCatalogItem          // the loaded catalog, for ExternalCode / SellUnit of cart lines
	Facts        []salesbrain.CatalogFact // the rows the model saw (upsell detection)
	Orders       []persistedOrder         // the buyer's non-cancelled orders in this conversation
	// Open is the buyer's UNFINISHED orders (any age, capped for the prompt): what a cancel or
	// continue picks from. It is not the reminder selection (the 72-hour rule lives in RemindOrders).
	Open []salesbrain.OrderSnapshot
	// PendingCancel holds the order ids a "cancel all" question is waiting on (Redis, 15 minutes).
	PendingCancel []string
}

// lfOutcome is the result of applying a plan: the text to send and what to record or do after.
type lfOutcome struct {
	Reply          string
	Author         string // "ai", or "system" for a text that is not the model's
	ReplacedReason string // why the model's reply was replaced (message metadata)
	OrderID        string
	OrderAction    string
	// ActionRan: a deterministic transactional step succeeded this turn (cancel, cancel request,
	// cancel-all question or execution, order saved, cart changed). A model "takeover" after it is
	// downgraded to a note (lfApplyHandoff).
	ActionRan bool
	// After runs once the reply has been sent: the pause of a takeover, owner notifications that
	// follow the reply, and the checkout clean-up that must survive a failed send.
	After func(ctx context.Context) error
}

// Owner-facing copy (Indonesian like the other notification kinds).
const (
	notifyTitleCancelPaid          = "Pembeli minta batalkan pesanan yang sudah dibayar"
	notifyReasonCancelPaid         = "pesanan %s sudah dibayar; pembatalan perlu ditangani tim."
	notifyReasonDraftSaveFailed    = "draft pesanan awal gagal disimpan ke database; keranjang masih ada di sesi, pembeli tetap dibalas. Pengingat pesanan terbuka belum bisa muncul untuk keranjang ini."
	notifyReasonOrderSaveFailed    = "pesanan gagal disimpan setelah pembeli konfirmasi (stok tidak cukup atau data belum lengkap); pembeli sudah diberi tahu."
	notifyReasonConfirmHeld        = "pembeli sedang di langkah konfirmasi akhir, tetapi AI gagal membaca balasannya; pesanan TIDAK disimpan otomatis. Ringkasan dikirim ulang dan menunggu OK dari pembeli."
	notifyAIReasonLabel            = " Catatan AI: "
	notifyReasonTakeoverDowngraded = "AI meminta takeover, tetapi aksi pembeli sudah berhasil diproses sistem sehingga AI tetap aktif; cek chat bila perlu."
)

// lfHandoffReasonRunes caps the model's own reason in an owner notification.
const lfHandoffReasonRunes = 120

// Reasons recorded in message metadata when the model's reply was replaced.
const (
	lfReplacedCancel             = "cancel_template"
	lfReplacedCancelPaid         = "cancel_paid_template"
	lfReplacedCancelPick         = "cancel_pick"
	lfReplacedCancelAllAsk       = "cancel_all_confirm"
	lfReplacedCancelAllDone      = "cancel_all_template"
	lfReplacedPersistFailed      = "order_persist_failed"
	lfReplacedVariantMissing     = "variant_missing"
	lfReplacedNotExplicitConfirm = "confirm_not_explicit"
	lfReplacedFieldIncomplete    = "field_incomplete"
	lfReplacedConfirmNotReady    = "confirm_not_ready"
)

// lfApplyIntent applies the accepted decision of a turn. An error means a write that must be
// retried by the job (a failed cancel); a failed order save is answered honestly instead and is
// not an error. IntentStatus is not handled here: the shell hands it to the order-status handler.
func lfApplyIntent(ctx context.Context, fx lfFX, in lfApplyIn) (lfOutcome, error) {
	out := lfOutcome{Reply: in.Plan.Reply, Author: "ai", ReplacedReason: in.Plan.ReplacedReason}
	var err error
	d := in.Plan.Decision
	if len(in.PendingCancel) > 0 {
		confirmed := (d.Intent == salesbrain.IntentConfirm || (d.Intent == salesbrain.IntentCancel && d.CancelScope == salesbrain.CancelAll)) &&
			bf.IsExplicitOrderConfirm(in.UserText)
		switch {
		case confirmed:
			// The buyer answered the "cancel all?" question with an explicit OK (the model's intent
			// is only the hint; the words are checked by the same validator as the checkout OK).
			if err = lfApplyCancelAllConfirmed(ctx, fx, in, &out); err != nil {
				return out, err
			}
			lfApplyHandoff(fx, in, &out)
			return out, nil
		case d.Intent == salesbrain.IntentCancel && d.CancelScope == salesbrain.CancelAll:
			// asked again: the question below is re-sent and the pending set refreshed.
		default:
			fx.clearPendingCancel(ctx) // declined, or the buyer moved on: fail-closed, nothing is cancelled
		}
	}
	switch in.Plan.Decision.Intent {
	case salesbrain.IntentOrder, salesbrain.IntentAmend:
		lfApplyCart(ctx, fx, in, &out)
	case salesbrain.IntentFieldAnswer:
		lfApplyFields(ctx, fx, in, &out)
	case salesbrain.IntentConfirm:
		lfApplyConfirm(ctx, fx, in, &out)
	case salesbrain.IntentCancel:
		err = lfApplyCancel(ctx, fx, in, &out)
	case salesbrain.IntentContinueOrder:
		lfApplyContinue(ctx, fx, in, &out)
	case salesbrain.IntentNewOrder:
		lfApplyNewOrder(ctx, fx, in, &out)
	}
	if err != nil {
		return out, err
	}
	if in.Plan.CartChanged {
		out.ActionRan = true
	}
	lfApplyHandoff(fx, in, &out)
	return out, nil
}

// lfApplyCart stores the cart planTurn computed. The reply (model text, summary, resume line) is
// already composed. An offer of another product is remembered so the order carries it only once.
// A changed, non-empty cart is also saved as a draft order once the reply has gone out (spec §4.2:
// the open-order reminder reads the database, not Redis).
func lfApplyCart(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) {
	p := in.Plan
	offers := lfReplyOffersUpsell(p.Decision.Reply, in.Facts, p.Cart)
	switch {
	case p.CartChanged:
		next := stateWithCart(in.State, p.Cart, in.Catalog)
		if next == nil {
			fx.clearState(ctx) // every line was removed
			return
		}
		if offers {
			next.UpsellShown = true
		}
		if strings.TrimSpace(next.PersistedOrderID) == "" {
			next.ForceNewOrder = true // a cart without a draft of its own never merges into a parked one
		}
		fx.setState(ctx, *next)
		if next.Step == lfStepConfirm && len(p.Issues) == 0 && (in.State == nil || in.State.Step != lfStepConfirm) {
			// The last open question (a size) was answered and everything else was collected before:
			// the buyer gets the full summary (items, address) for the final OK.
			out.Reply = p.Decision.Reply + "\n\n" + lfConfirmBlock(next, p.Lang)
		}
		lfScheduleEarlyDraft(fx, in, *next, out)
	case offers && in.State != nil && !in.State.UpsellShown:
		st := *in.State
		st.UpsellShown = true
		fx.setState(ctx, st)
	}
}

// lfScheduleEarlyDraft queues the draft write after the send. It runs before the steps already
// queued by the caller's After (none today) and never fails the turn.
func lfScheduleEarlyDraft(fx lfFX, in lfApplyIn, st orderState, out *lfOutcome) {
	if !lfStepReachedRecipient(st.Step) || !st.CartReadyForDraft() {
		return
	}
	previous := out.After
	out.After = func(ctx context.Context) error {
		var first error
		if previous != nil {
			first = previous(ctx)
		}
		lfSaveEarlyDraft(ctx, fx, in.UserText, st)
		return first
	}
}

// lfStepReachedRecipient: ask_recipient or later (the stage the legacy flow saves its early draft).
func lfStepReachedRecipient(step string) bool {
	switch step {
	case "", "ask_product", "ask_variant", "ask_qty":
		return false
	}
	return true
}

// lfSaveEarlyDraft writes or updates the draft order of the live cart through persistDraftOrderEarly
// and pins its id in the state, so the next amend and the confirm step update the same row. A cart
// without a pinned draft is always a new draft ("new order" parks the old one; "continue" brings the
// old one back pinned). When the pinned draft is gone and several drafts compete, the cart is saved
// as a fresh draft: the pipeline never shows the legacy pick list. Failures are logged and told to
// the owner; the buyer already has the reply.
func lfSaveEarlyDraft(ctx context.Context, fx lfFX, userText string, st orderState) {
	pinned := strings.TrimSpace(st.PersistedOrderID) != ""
	st.ForceNewOrder = !pinned
	id, needPick, err := fx.persistEarlyDraft(ctx, st)
	if err == nil && needPick && pinned {
		st.PersistedOrderID, st.ForceNewOrder = "", true
		id, needPick, err = fx.persistEarlyDraft(ctx, st)
	}
	if err != nil {
		rlog.Warn("llm_first: early draft not saved", "err", err)
		fx.notify(ctx, notifyKindSystemError, notifyTitleSystemError,
			ownerNotificationSummary(userText, notifyReasonDraftSaveFailed))
		return
	}
	if id == "" || needPick || (id == st.PersistedOrderID && !st.ForceNewOrder) {
		return
	}
	st.PersistedOrderID, st.ForceNewOrder = id, false
	fx.setState(ctx, st)
}

// lfApplyFields writes the checkout data the model extracted, after applyFieldAnswer validated it
// against the buyer's own words. Invalid data is named and the pending field is asked again; once
// everything is there the full summary is shown for the final OK.
func lfApplyFields(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) {
	if in.State == nil {
		return // no cart, nothing to fill in: the model's answer stands
	}
	p := in.Plan
	st := *in.State
	st.Items = append([]orderLineState(nil), in.State.Items...)
	problems := applyFieldAnswer(&st, p.Decision.Fields, in.UserText, p.Lang)
	fx.setState(ctx, st)

	modelReply := p.Decision.Reply
	pending := pendingFieldFromState(&st)
	if pending != "" && pending != "confirm" {
		// Fields are still missing: whatever the model claimed ("ready for payment", "address
		// saved") is not what code did. The buyer reads what was stored, what is missing, and the
		// question again, all from code.
		out.Reply = lfFieldProgressReply(in.State, &st, pending, problems, p.Lang)
		out.Author = "system"
		out.ReplacedReason = lfReplacedFieldIncomplete
		return
	}
	var b strings.Builder
	b.WriteString(modelReply)
	if len(problems) > 0 {
		b.WriteString("\n\n" + salesbrain.T(salesbrain.TplFieldProblems, p.Lang, lfJoinProblems(problems)))
	}
	switch {
	case pending == "confirm":
		b.WriteString("\n\n" + lfConfirmBlock(&st, p.Lang))
	case pending != "" && (len(problems) > 0 || !strings.Contains(modelReply, "?")):
		b.WriteString("\n\n" + salesbrain.ResumeLineFor(pending, p.Lang))
	}
	out.Reply = b.String()
}

// lfFieldProgressReply is the code-built answer to a field_answer that left the checkout
// incomplete: what was stored this turn (short), the invalid data, exactly what is missing, and
// the resume line for the pending field.
func lfFieldProgressReply(before, after *orderState, pending string, problems []string, lang salesbrain.Lang) string {
	en := lang == salesbrain.LangEN
	label := func(id, eng string) string {
		if en {
			return eng
		}
		return id
	}
	var saved []string
	if after.RecipientName != before.RecipientName {
		saved = append(saved, label("nama ", "name ")+after.RecipientName)
	}
	if after.RecipientPhone != before.RecipientPhone {
		saved = append(saved, label("HP ", "phone ")+after.RecipientPhone)
	}
	var addr []string
	for _, c := range []struct{ was, now string }{
		{before.Street, after.Street}, {before.Kecamatan, after.Kecamatan}, {before.City, after.City},
		{before.Province, after.Province}, {before.PostalCode, after.PostalCode},
	} {
		if strings.TrimSpace(c.now) != "" && c.now != c.was {
			addr = append(addr, strings.TrimSpace(c.now))
		}
	}
	if len(addr) > 0 {
		saved = append(saved, label("alamat ", "address ")+strings.Join(addr, ", "))
	}

	var parts []string
	if len(saved) > 0 {
		parts = append(parts, salesbrain.T(salesbrain.TplFieldSaved, lang, strings.Join(saved, "; ")))
	}
	if len(problems) > 0 {
		parts = append(parts, salesbrain.T(salesbrain.TplFieldProblems, lang, lfJoinProblems(problems)))
	}
	if missing := lfMissingFields(after, pending, en); len(missing) > 0 {
		parts = append(parts, salesbrain.T(salesbrain.TplFieldMissing, lang, strings.Join(missing, ", ")))
	}
	parts = append(parts, salesbrain.ResumeLineFor(pending, lang))
	return strings.Join(parts, "\n\n")
}

// lfMissingFields names what the pending field still lacks (the same parts addressMissingReply
// of the legacy flow names).
func lfMissingFields(st *orderState, pending string, en bool) []string {
	var missing []string
	add := func(id, eng string) {
		if en {
			missing = append(missing, eng)
		} else {
			missing = append(missing, id)
		}
	}
	switch pending {
	case "recipient":
		if strings.TrimSpace(st.RecipientName) == "" {
			add("nama penerima", "recipient name")
		}
		if strings.TrimSpace(st.RecipientPhone) == "" {
			add("nomor HP", "phone number")
		}
	case "address":
		if strings.TrimSpace(st.Street) == "" {
			add("nama jalan dan nomor", "street name and number")
		}
		if strings.TrimSpace(st.City) == "" {
			add("kota", "city")
		}
		if strings.TrimSpace(st.Province) == "" {
			add("provinsi", "province")
		}
		if len(st.PostalCode) != 5 {
			add("kode pos 5 digit", "5-digit postal code")
		}
	}
	return missing
}

// lfConfirmBlock is the final summary: the items with the subtotal, where the order goes, and the
// line that asks for the OK. Every label comes from a salesbrain template.
func lfConfirmBlock(st *orderState, lang salesbrain.Lang) string {
	var parts []string
	if summary := salesbrain.RenderCartSummary(cartFromState(st), lang); summary != "" {
		parts = append(parts, summary)
	}
	parts = append(parts,
		salesbrain.T(salesbrain.TplShippingSummary, lang, st.RecipientName, st.RecipientPhone, lfAddressLine(st)),
		salesbrain.T(salesbrain.TplResumeConfirm, lang))
	return strings.Join(parts, "\n\n")
}

// lfApplyConfirm saves the order when the buyer approved the summary: only at the confirm step
// with complete shipping data. The save goes through persistDraftOrder, the function the legacy
// checkout uses, so stock guards and stock sync are the same. The draft id is pinned in the state
// before the send and the state is cleared after it, so a retried turn updates the same draft.
func lfApplyConfirm(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) {
	st := in.State
	if st == nil || st.Step != lfStepConfirm || !st.ShippingComplete() || len(cartFromState(st)) == 0 {
		// Nothing to confirm. The model's reply (probably "confirmed" / "ready") would be false:
		// the buyer is led back to the pending field instead.
		if pending := lfPendingForUnconfirmed(st); pending != "" {
			out.Reply = salesbrain.ResumeLineFor(pending, in.Plan.Lang)
			out.Author = "system"
			out.ReplacedReason = lfReplacedConfirmNotReady
		}
		return
	}
	p := in.Plan
	next := *st
	next.Items = append([]orderLineState(nil), st.Items...)

	if lfCartMissingVariant(cartFromState(&next)) {
		// A line still lacks its size or colour (state written before the variant was asked):
		// nothing can be saved honestly. Go back one step and ask.
		next.Step = lfStepAskVariant
		fx.setState(ctx, next)
		out.Reply = lfAskVariantReply(in.Facts, &next, p.Lang)
		out.Author = "system"
		out.ReplacedReason = lfReplacedVariantMissing
		return
	}

	if !bf.IsExplicitOrderConfirm(in.UserText) {
		// The model may classify "ok tapi ganti alamat" as confirm; only the buyer's words count.
		out.Reply = lfConfirmBlock(st, p.Lang)
		out.Author = "system"
		out.ReplacedReason = lfReplacedNotExplicitConfirm
		return
	}

	id, err := lfPersistOrder(ctx, fx, next)
	if err != nil {
		// The model's reply (probably "your order is confirmed") would be false: say so honestly
		// in the locked language and tell the owner. The cart is kept, nothing is paused.
		out.Reply = salesbrain.T(salesbrain.TplSystemBusy, p.Lang, in.Shop)
		out.Author = "system"
		out.ReplacedReason = lfReplacedPersistFailed
		out.After = func(ctx context.Context) error {
			fx.notify(ctx, notifyKindSystemError, notifyTitleSystemError,
				ownerNotificationSummary(in.UserText, notifyReasonOrderSaveFailed))
			return nil
		}
		return
	}
	next.PersistedOrderID, next.ForceNewOrder = id, false
	fx.setState(ctx, next)
	out.Reply = p.Decision.Reply + "\n\n" + salesbrain.T(salesbrain.TplOrderRef, p.Lang, FormatOrderNumber(id))
	out.OrderID, out.OrderAction = id, "create"
	out.ActionRan = true
	out.After = func(ctx context.Context) error {
		fx.clearState(ctx)
		return nil
	}
}

// lfPendingForUnconfirmed is the field to ask for when the buyer "confirmed" something that is not
// ready to confirm: "" when there is no live checkout (the model's answer stands).
func lfPendingForUnconfirmed(st *orderState) string {
	if st == nil || len(cartFromState(st)) == 0 {
		return ""
	}
	pending := pendingFieldFromState(st)
	if pending == "confirm" || pending == "" {
		switch lfStepAfterItems(st) {
		case lfStepAskRecipient:
			return "recipient"
		case lfStepAskAddressFull:
			return "address"
		}
		return "" // everything is there and the step is confirm: handled by the caller
	}
	return pending
}

// lfFirstMissingVariantName is the product of the first cart line that lacks its variant.
func lfAskVariantReply(facts []salesbrain.CatalogFact, st *orderState, lang salesbrain.Lang) string {
	for _, l := range cartFromState(st) {
		if l.VariantMissing {
			return salesbrain.AskVariantReply(facts, l, bf.ProductBaseName(l.Name), lang)
		}
	}
	return salesbrain.T(salesbrain.TplAskVariant, lang, "")
}

func lfFirstMissingVariantName(st *orderState) string {
	for _, l := range cartFromState(st) {
		if l.VariantMissing {
			return bf.ProductBaseName(l.Name)
		}
	}
	return ""
}

// lfPersistOrder saves the checkout. Without a pinned draft it is a new order, never a merge into a
// leftover draft ("pesanan baru" and "lanjut" are decided in the conversation, not by the save).
// A pinned draft that is gone (no longer a draft) makes the legacy save ask which draft to amend:
// the order is then saved as a fresh draft.
func lfPersistOrder(ctx context.Context, fx lfFX, st orderState) (string, error) {
	pinned := strings.TrimSpace(st.PersistedOrderID) != ""
	st.ForceNewOrder = !pinned
	id, err := fx.persistOrder(ctx, st)
	if err != nil && pinned && errors.Is(err, errMultiDraftDisambiguation) {
		st.PersistedOrderID, st.ForceNewOrder = "", true
		return fx.persistOrder(ctx, st)
	}
	return id, err
}

// lfOrderIsPaid: anything past the draft stage, or a draft whose transfer proof is in.
func lfOrderIsPaid(o *persistedOrder) bool {
	if o.Status != "draft" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(o.PaymentStatus)) {
	case "proof_submitted", "verified":
		return true
	}
	return false
}

// lfFindOrderByRef finds the buyer's order whose number starts with ref ("WB-F4BFCE" matches
// "WB-F4BFCE06").
func lfFindOrderByRef(orders []persistedOrder, ref string) *persistedOrder {
	for i := range orders {
		if strings.HasPrefix(FormatOrderNumber(orders[i].ID), strings.ToUpper(ref)) {
			return &orders[i]
		}
	}
	return nil
}

// lfPickCancelTarget decides which saved order a single "cancel" is about, or nil when there is none:
//  1. an order number in the message (unknown number: nil, never another order),
//  2. the draft pinned to the buyer's live checkout,
//  3. with no live cart, the one unfinished order. With two or more unfinished orders and nothing
//     that names one, the choice is the buyer's: a cancellation is never guessed (lfApplyCancel then
//     sends the pick list).
//
// open is the buyer's unfinished orders of any age (TurnState.OpenOrders), not the reminder
// selection: an old unpaid draft counts as one of the orders to choose from.
func lfPickCancelTarget(orders []persistedOrder, open []salesbrain.OrderSnapshot, st *orderState, userText string) *persistedOrder {
	if ref := parseOrderRefFromMessage(userText); ref != "" {
		return lfFindOrderByRef(orders, ref)
	}
	if st != nil && strings.TrimSpace(st.PersistedOrderID) != "" {
		for i := range orders {
			if orders[i].ID == st.PersistedOrderID {
				return &orders[i]
			}
		}
	}
	if len(cartFromState(st)) == 0 && len(open) == 1 {
		return lfFindOrderByRef(orders, open[0].Ref)
	}
	return nil
}

// lfUnfinishedOrders are the saved orders a "cancel all" covers: every order that is not cancelled,
// shipped or completed, newest first and not capped (the prompt list is; the confirmation shows all).
func lfUnfinishedOrders(orders []persistedOrder) []persistedOrder {
	var out []persistedOrder
	for i := range orders {
		if _, ok := lfUnfinishedSnapshot(&orders[i]); ok {
			out = append(out, orders[i])
		}
	}
	return out
}

// lfPendingRefs are the order numbers of the ids a "cancel all" waits on that still exist (what the
// prompt shows as STATE.konfirmasi_batal_semua).
func lfPendingRefs(orders []persistedOrder, ids []string) []string {
	var refs []string
	for _, id := range ids {
		for i := range orders {
			if orders[i].ID == id {
				refs = append(refs, FormatOrderNumber(id))
				break
			}
		}
	}
	return refs
}

// lfNumberedOrders renders "1. WB-…: items, total, stage" lines; a paid order says that its
// cancellation goes to the team.
func lfNumberedOrders(snaps []salesbrain.OrderSnapshot, lang salesbrain.Lang) string {
	en := lang == salesbrain.LangEN
	lines := make([]string, 0, len(snaps))
	for i, o := range snaps {
		line := fmt.Sprintf("%d. %s", i+1, salesbrain.DescribeOrder(o, lang))
		if o.Paid {
			if en {
				line += " (already paid: goes to the team)"
			} else {
				line += " (sudah dibayar: diteruskan ke tim)"
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// lfAskCancelAll answers a model decision "cancel, scope all": it lists the unfinished orders and
// asks the buyer to confirm, storing the ids in Redis (15 minutes). Nothing is cancelled here.
// It reports false when fewer than two orders are unfinished: the single-order rules apply.
func lfAskCancelAll(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) bool {
	unfinished := lfUnfinishedOrders(in.Orders)
	if len(unfinished) < 2 {
		return false
	}
	p := in.Plan
	ids := make([]string, 0, len(unfinished))
	snaps := make([]salesbrain.OrderSnapshot, 0, len(unfinished))
	for i := range unfinished {
		snap, _ := lfUnfinishedSnapshot(&unfinished[i])
		ids = append(ids, unfinished[i].ID)
		snaps = append(snaps, snap)
	}
	out.Author = "system"
	out.ActionRan = true // the question is the deterministic answer; the AI must stay on for the OK
	if err := fx.setPendingCancel(ctx, ids); err != nil {
		rlog.Warn("llm_first: cancel-all confirmation not stored", "err", err)
		out.Reply = salesbrain.T(salesbrain.TplSystemBusy, p.Lang, in.Shop)
		out.ReplacedReason = lfReplacedPersistFailed
		return true
	}
	out.Reply = salesbrain.T(salesbrain.TplCancelAllConfirm, p.Lang, lfNumberedOrders(snaps, p.Lang))
	out.ReplacedReason = lfReplacedCancelAllAsk
	return true
}

// lfApplyCancelAllConfirmed runs the cancel-all the buyer just confirmed, over the ids stored when
// it was asked: an unpaid order is cancelled in the database (cancelPersistedOrder), a paid one goes
// to the team (one owner notification), an id that is no longer among the buyer's active orders is
// reported as cancelled (an earlier attempt of this turn did it). A failed cancel is returned so
// the job retries; the stored ids stay until the reply has been sent, so the retry is complete.
func lfApplyCancelAllConfirmed(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) error {
	p := in.Plan
	var cancelled, paid []string
	clearCart := false
	for _, id := range in.PendingCancel {
		ref := FormatOrderNumber(id)
		var o *persistedOrder
		for i := range in.Orders {
			if in.Orders[i].ID == id {
				o = &in.Orders[i]
				break
			}
		}
		if o == nil {
			cancelled = append(cancelled, ref)
			continue
		}
		if _, ok := lfUnfinishedSnapshot(o); !ok {
			continue // shipped or completed since the question: never touched
		}
		if lfOrderIsPaid(o) {
			paid = append(paid, ref)
			continue
		}
		if err := fx.cancelOrder(ctx, id); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		cancelled = append(cancelled, ref)
		if in.State != nil && in.State.PersistedOrderID == id {
			clearCart = true
		}
	}
	if len(paid) > 0 {
		fx.notify(ctx, notifyKindHandoff, notifyTitleCancelPaid,
			ownerNotificationSummary(in.UserText, fmt.Sprintf(notifyReasonCancelPaid, strings.Join(paid, ", "))))
	}
	if clearCart {
		fx.clearState(ctx)
	}
	var parts []string
	switch len(paid) {
	case 0:
	case 1:
		parts = append(parts, salesbrain.T(salesbrain.TplCancelPaidOrder, p.Lang, paid[0], in.Shop))
	default:
		parts = append(parts, salesbrain.T(salesbrain.TplCancelPaidMany, p.Lang, strings.Join(paid, ", "), in.Shop))
	}
	switch len(cancelled) {
	case 0:
	case 1:
		parts = append(parts, salesbrain.T(salesbrain.TplOrderCancelled, p.Lang, cancelled[0]))
	default:
		parts = append(parts, salesbrain.T(salesbrain.TplCancelledMany, p.Lang, strings.Join(cancelled, ", ")))
	}
	if len(parts) > 0 {
		out.Reply = strings.Join(parts, "\n\n")
		out.ReplacedReason = lfReplacedCancelAllDone
		out.OrderAction = "cancel_all"
		out.ActionRan = true
	} else {
		out.Reply = p.Decision.Reply
	}
	out.After = func(ctx context.Context) error {
		fx.clearPendingCancel(ctx)
		return nil
	}
	return nil
}

// lfApplyCancel cancels what the buyer asked to cancel. The model decides the scope
// (Decision.CancelScope): "all" with two or more unfinished orders only asks for confirmation
// (lfAskCancelAll); the cancellation itself happens when the buyer confirms. A single cancel acts
// on the order named in the message, the live checkout's draft, or the only unfinished order; with
// several unfinished orders and no name, the buyer gets the pick list. An unpaid draft is cancelled
// in the database through cancelPersistedOrder (the legacy cancel) and the reply is a template: the
// model must not claim a cancellation that code did not do. A paid order is never cancelled by the
// bot: it goes to the owner first, then the buyer is told so. With nothing saved, only the live cart
// is cleared and the model's answer stands.
func lfApplyCancel(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) error {
	p := in.Plan
	namedRef := parseOrderRefFromMessage(in.UserText) != ""
	if p.Decision.CancelScope == salesbrain.CancelAll && !namedRef && lfAskCancelAll(ctx, fx, in, out) {
		return nil
	}
	target := lfPickCancelTarget(in.Orders, in.Open, in.State, in.UserText)
	if target == nil {
		if !namedRef && len(cartFromState(in.State)) == 0 && len(in.Open) >= 2 {
			out.Reply = salesbrain.T(salesbrain.TplCancelPick, p.Lang, lfNumberedOrders(in.Open, p.Lang))
			out.Author = "system"
			out.ReplacedReason = lfReplacedCancelPick
			return nil
		}
		out.Reply = p.Decision.Reply // not the composed one: its resume line belongs to the cart being dropped
		if in.State != nil && !namedRef {
			fx.clearState(ctx)
		}
		return nil
	}
	ref := FormatOrderNumber(target.ID)
	if lfOrderIsPaid(target) {
		fx.notify(ctx, notifyKindHandoff, notifyTitleCancelPaid,
			ownerNotificationSummary(in.UserText, fmt.Sprintf(notifyReasonCancelPaid, ref)))
		out.Reply = salesbrain.T(salesbrain.TplCancelPaidOrder, p.Lang, ref, in.Shop)
		out.ReplacedReason = lfReplacedCancelPaid
		out.OrderID, out.OrderAction = target.ID, "cancel_request"
		out.ActionRan = true
		return nil
	}
	if err := fx.cancelOrder(ctx, target.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err // sql.ErrNoRows: already cancelled by an earlier attempt of this turn
	}
	otherCartLive := in.State != nil && len(cartFromState(in.State)) > 0 && in.State.PersistedOrderID != target.ID
	if !otherCartLive {
		fx.clearState(ctx)
	}
	out.Reply = salesbrain.T(salesbrain.TplOrderCancelled, p.Lang, ref)
	out.ReplacedReason = lfReplacedCancel
	out.OrderID, out.OrderAction = target.ID, "cancel"
	out.ActionRan = true
	return nil
}

// lfFirstUnpaid is the newest unfinished order that is still unpaid (nil when none).
func lfFirstUnpaid(open []salesbrain.OrderSnapshot) *salesbrain.OrderSnapshot {
	for i := range open {
		if !open[i].Paid {
			return &open[i]
		}
	}
	return nil
}

// lfPickOpenOrder picks the unfinished order a "continue" is about: the one named in the message
// (unknown number: nil), else the newest unpaid one (a paid order has nothing left to continue).
func lfPickOpenOrder(open []salesbrain.OrderSnapshot, userText string) *salesbrain.OrderSnapshot {
	if len(open) == 0 {
		return nil
	}
	if ref := parseOrderRefFromMessage(userText); ref != "" {
		for i := range open {
			if strings.HasPrefix(open[i].Ref, strings.ToUpper(ref)) {
				return &open[i]
			}
		}
		return nil
	}
	return lfFirstUnpaid(open)
}

// lfApplyContinue brings a saved draft back as the live checkout and asks only for what it still
// lacks. A buyer who already has a live cart means that cart (planTurn resumed it already), and an
// order that waits for payment has nothing left to collect: the model's answer stands.
func lfApplyContinue(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) {
	if len(cartFromState(in.State)) > 0 {
		return
	}
	snap := lfPickOpenOrder(in.Open, in.UserText)
	if snap == nil {
		return
	}
	order := lfFindOrderByRef(in.Orders, snap.Ref)
	if order == nil {
		return
	}
	st, ok := lfStateFromOpenOrder(order)
	if !ok {
		return
	}
	fx.setState(ctx, *st)
	out.Reply = lfComposeReply(in.Plan.Decision.Reply, cartFromState(st), true, pendingFieldFromState(st), in.Plan.Lang)
}

// lfApplyNewOrder drops the live cart and keeps the saved draft (it is only parked, never
// cancelled). The buyer is told which order stays saved unless the model already said so.
func lfApplyNewOrder(ctx context.Context, fx lfFX, in lfApplyIn, out *lfOutcome) {
	fx.clearState(ctx)
	out.Reply = in.Plan.Decision.Reply // not the composed one: the old cart's resume line no longer applies
	if parked := lfFirstUnpaid(in.Open); parked != nil && !lfReplyMentionsRef(out.Reply, parked.Ref) {
		out.Reply += "\n\n" + salesbrain.T(salesbrain.TplNewOrderParked, in.Plan.Lang, parked.Ref)
	}
}

// lfHandoffLevel is how far the shop team has to get involved. A buyer who asks for a person is a
// takeover whatever the handoff field says; otherwise the model's level counts.
func lfHandoffLevel(d salesbrain.Decision) salesbrain.HandoffLevel {
	if d.Intent == salesbrain.IntentHumanRequest {
		return salesbrain.HandoffTakeover
	}
	if d.Handoff != nil {
		return d.Handoff.Level
	}
	return ""
}

// lfApplyHandoff involves the shop team after the reply has gone out. A takeover that follows a
// successful transactional step (out.ActionRan) of a buyer who did not ask for a person is
// downgraded to a note. Otherwise a takeover pauses the AI
// (it resumes by itself after AutoResumeAfter when no staff replied) and tells the owner, also when
// the pause failed; the reply carries the opening hours so the buyer knows when to expect staff. A
// note only tells the owner and the AI keeps serving.
func lfApplyHandoff(fx lfFX, in lfApplyIn, out *lfOutcome) {
	d := in.Plan.Decision
	level := lfHandoffLevel(d)
	if level == "" {
		return
	}
	why := ""
	if d.Handoff != nil {
		if r := strings.Join(strings.Fields(d.Handoff.Reason), " "); r != "" {
			why = notifyAIReasonLabel + salesbrain.RedactForLog(r, lfHandoffReasonRunes)
		}
	}
	previous := out.After
	if level == salesbrain.HandoffTakeover && d.Intent != salesbrain.IntentHumanRequest && out.ActionRan {
		// The turn's transactional step already succeeded (cancel, order saved, cart changed, the
		// cancel-all question): pausing the AI now would strand a buyer whose request was served.
		// The owner still hears about it, as a note; no opening-hours sentence, no pause.
		out.After = func(ctx context.Context) error {
			var first error
			if previous != nil {
				first = previous(ctx)
			}
			fx.notify(ctx, notifyKindHandoff, notifyTitleNoAnswer,
				ownerNotificationSummary(in.UserText, notifyReasonTakeoverDowngraded+why))
			return first
		}
		return
	}
	switch level {
	case salesbrain.HandoffTakeover:
		out.Reply = lfWithOpeningHours(out.Reply, in.OpeningHours, in.Plan.Lang)
		out.After = func(ctx context.Context) error {
			var first error
			if previous != nil {
				first = previous(ctx)
			}
			pauseErr := fx.pause(ctx, HandoffReasonHumanRequest)
			fx.notify(ctx, notifyKindHandoff, notifyTitleHumanRequest,
				ownerNotificationSummary(in.UserText, notifyReasonHumanRequest+why))
			if first == nil {
				first = pauseErr
			}
			return first
		}
	case salesbrain.HandoffNote:
		out.After = func(ctx context.Context) error {
			var first error
			if previous != nil {
				first = previous(ctx)
			}
			fx.notify(ctx, notifyKindHandoff, notifyTitleNoAnswer,
				ownerNotificationSummary(in.UserText, notifyReasonNoAnswer+why))
			return first
		}
	}
}

// lfWithOpeningHours adds the tenant's opening hours to a handoff reply (no promise of a time).
func lfWithOpeningHours(reply, hours string, lang salesbrain.Lang) string {
	hours = strings.TrimSpace(hours)
	if hours == "" || strings.Contains(reply, hours) {
		return reply
	}
	return reply + "\n\n" + salesbrain.OpeningHoursSentence(hours, lang)
}
