package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/shared/llm"
	"encore.app/wabantu/usage"
)

// A boxer in three sizes (one row per size, as the legacy catalog stores variants), a T-shirt row
// that needs a size or colour, and a plain steak. Row numbers: 1 boxer L, 2 boxer M, 3 boxer XL
// (sold out), 4 kaos, 5 salmon.
func lfVariantCatalog() []dbCatalogItem {
	return []dbCatalogItem{
		{ID: "boxer-l", ExternalCode: "BOXER-MONO-L", Name: "Boxer Pria Mono - L", SellPrice: 56900, SellUnit: "pcs", StockTracked: true, StockAvailable: 5},
		{ID: "boxer-m", ExternalCode: "BOXER-MONO-M", Name: "Boxer Pria Mono - M", SellPrice: 54900, SellUnit: "pcs", StockTracked: true, StockAvailable: 3},
		{ID: "boxer-xl", ExternalCode: "BOXER-MONO-XL", Name: "Boxer Pria Mono - XL", SellPrice: 58900, SellUnit: "pcs", StockTracked: true, StockAvailable: 0},
		{ID: "kaos", ExternalCode: "KAOS-POLOS", Name: "Kaos Polos Premium", SellPrice: 89000, SellUnit: "pcs"},
		{ID: "salmon-100", ExternalCode: "SALMON_STEAK_100_GRAM", Name: "Salmon Steak 100 Gram", SellPrice: 75000, SellUnit: "pcs"},
	}
}

func lfVariantPlanBase(msg string, extraTexts ...string) planInput {
	in := lfPlanBase(msg)
	in.Catalog = lfCatalogFacts(lfVariantCatalog())
	in.BuyerTexts = append([]string{msg}, extraTexts...)
	return in
}

// ── catalog facts ────────────────────────────────────────────────────────────

func TestLFCatalogFactsCarryVariantStockAndPrice(t *testing.T) {
	facts := lfCatalogFacts(lfVariantCatalog())
	l, m, xl, kaos, salmon := facts[0], facts[1], facts[2], facts[3], facts[4]

	if l.Size != "L" || l.BaseName != "Boxer Pria Mono" || !l.NeedsVariant || l.Stock != salesbrain.StockIn || l.StockQty != 5 || l.Price != 56900 {
		t.Errorf("boxer L = %+v", l)
	}
	if m.Size != "M" || m.BaseName != l.BaseName || m.Price != 54900 || m.StockQty != 3 {
		t.Errorf("boxer M = %+v", m)
	}
	if xl.Stock != salesbrain.StockOut || xl.StockQty != 0 {
		t.Errorf("boxer XL = %+v", xl)
	}
	if kaos.Size != "" || !kaos.NeedsVariant || kaos.Stock != "" {
		t.Errorf("kaos needs a variant but has no size of its own: %+v", kaos)
	}
	if salmon.Size != "" || salmon.NeedsVariant || salmon.BaseName != "Salmon Steak 100 Gram" {
		t.Errorf("a plain product has no variant data: %+v", salmon)
	}
	if sibs := salesbrain.Siblings(facts, l); len(sibs) != 2 || sibs[0].ID != "boxer-m" || sibs[1].ID != "boxer-xl" {
		t.Errorf("siblings of L = %+v", sibs)
	}
}

func TestTopKCatalogKeepsTheOtherSizesOfAKeptProduct(t *testing.T) {
	var catalog []dbCatalogItem
	for i := 0; i < 30; i++ {
		catalog = append(catalog, dbCatalogItem{ID: "x" + string(rune('A'+i)), Name: "Produk Lain " + string(rune('A'+i)), SellPrice: 1000})
	}
	catalog = append(catalog, lfVariantCatalog()[:3]...) // the three boxer rows: only the first one matches the query
	facts := lfCatalogFacts(catalog)
	got := topKCatalog(facts, "boxer ukuran L", nil, 1)
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.ID] = true
	}
	if !ids["boxer-l"] || !ids["boxer-m"] || !ids["boxer-xl"] {
		t.Fatalf("all sizes of the matched boxer must stay: %+v", ids)
	}
}

// ── cart state with variants ─────────────────────────────────────────────────

func TestSizedRowsPersistAndSurviveTheStateRoundTrip(t *testing.T) {
	cart := []salesbrain.OrderLine{
		{ID: "boxer-l", Name: "Boxer Pria Mono - L", Qty: 2, UnitPrice: 56900, Size: "L"},
		{ID: "boxer-m", Name: "Boxer Pria Mono - M", Qty: 1, UnitPrice: 54900, Size: "M"},
	}
	st := stateWithCart(nil, cart, lfVariantCatalog())
	if st == nil || len(st.Items) != 2 || st.Items[0].Size != "L" || st.Items[1].Size != "M" {
		t.Fatalf("state = %+v", st)
	}
	if st.Step != lfStepAskRecipient || !st.CartReadyForDraft() {
		t.Fatalf("a sized cart is complete: step=%q ready=%v", st.Step, st.CartReadyForDraft())
	}
	back := cartFromState(st)
	if len(back) != 2 || back[0].Size != "L" || back[1].Size != "M" || back[0].VariantMissing || back[1].VariantMissing {
		t.Fatalf("round trip = %+v", back)
	}
	// The same product in two sizes stays two lines after a quantity change of one of them.
	next := stateWithCart(st, []salesbrain.OrderLine{
		{ID: "boxer-l", Name: "Boxer Pria Mono - L", Qty: 4, UnitPrice: 56900, Size: "L"},
		cart[1],
	}, lfVariantCatalog())
	if len(next.Items) != 2 || next.Items[0].Qty != 4 || next.Items[1].Qty != 1 || next.Items[1].ExternalCode != "BOXER-MONO-M" {
		t.Fatalf("next = %+v", next.Items)
	}
}

func TestLegacyStateWithoutSizeReadsTheSizeFromTheName(t *testing.T) {
	legacy := &orderState{Step: "ask_recipient", CatalogItemID: "boxer-l", ProductName: "Boxer Pria Mono - L", Qty: 1, UnitPrice: 56900}
	got := cartFromState(legacy)
	if len(got) != 1 || got[0].Size != "L" || got[0].VariantMissing {
		t.Fatalf("got %+v", got)
	}
}

func TestMissingVariantHoldsTheCheckoutAtAskVariant(t *testing.T) {
	cart := []salesbrain.OrderLine{{ID: "kaos", Name: "Kaos Polos Premium", Qty: 2, UnitPrice: 89000, VariantMissing: true}}
	st := stateWithCart(nil, cart, lfVariantCatalog())
	if st.Step != lfStepAskVariant || pendingFieldFromState(st) != "variant" {
		t.Fatalf("step=%q pending=%q", st.Step, pendingFieldFromState(st))
	}
	if st.CartReadyForDraft() {
		t.Fatal("a cart with an open variant must not be saved as a draft")
	}
	if got := cartFromState(st); len(got) != 1 || !got[0].VariantMissing || got[0].Qty != 2 {
		t.Fatalf("the quantity must be kept: %+v", got)
	}

	// Shipping data collected earlier is not asked again once the variant is filled.
	st.RecipientName, st.RecipientPhone = "Ana", "+62872839472828"
	st.Street, st.City, st.Province, st.PostalCode = "Villa Kalijudan Indah A9", "Surabaya", "Jawa Timur", "60177"
	filled := stateWithCart(st, []salesbrain.OrderLine{{ID: "kaos", Name: "Kaos Polos Premium", Qty: 2, UnitPrice: 89000, Size: "M", Color: "hitam"}}, lfVariantCatalog())
	if filled.Step != lfStepConfirm || filled.Items[0].Size != "M" || filled.Items[0].Color != "hitam" {
		t.Fatalf("filled = %+v", filled)
	}

	// An address answer cannot reach confirm while a variant is open.
	open := stateWithCart(st, cart, lfVariantCatalog())
	problems := applyFieldAnswer(open, lfFullAddressFields(), lfFullAddressText, salesbrain.LangEN)
	if len(problems) != 0 || open.Step != lfStepAskVariant {
		t.Fatalf("step=%q problems=%v: the final OK is only asked once the variant is known", open.Step, problems)
	}
}

// ── planTurn ─────────────────────────────────────────────────────────────────

func TestPlanMultiLineVariantsOfOneProduct(t *testing.T) {
	msg := "boxer ukuran L 2 pcs dan ukuran M 1 pcs ya"
	fake := lfTexts(`{"intent":"order","items":[{"no":1,"qty":2},{"no":2,"qty":1}],"reply":"Siap Kak, aku catat ya."}`)
	in := lfVariantPlanBase(msg)
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangID}
	plan := planTurn(context.Background(), in, fake.chat)

	if plan.Fallback || len(plan.Issues) != 0 || len(plan.Cart) != 2 || !plan.CartChanged {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Cart[0].Size != "L" || plan.Cart[1].Size != "M" || salesbrain.CartSubtotal(plan.Cart) != 2*56900+54900 {
		t.Fatalf("cart = %+v", plan.Cart)
	}
	for _, want := range []string{"Siap Kak, aku catat ya.", "1. 2x Boxer Pria Mono - L", "2. 1x Boxer Pria Mono - M", "Subtotal: Rp168.700"} {
		if !strings.Contains(plan.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, plan.Reply)
		}
	}
	if !strings.Contains(fake.requests[0].System, "ukuran lain: no 2 (M, tersedia); no 3 (XL, habis)") {
		t.Errorf("the prompt must name the sibling rows:\n%s", fake.requests[0].System)
	}
}

func TestPlanAsksOnceForAMissingVariantAndKeepsTheQuantity(t *testing.T) {
	msg := "mau kaos polos 2 ya"
	fake := lfTexts(`{"intent":"order","items":[{"no":4,"qty":2}],"reply":"Siap Kak, kaosnya sudah masuk keranjang."}`)
	in := lfVariantPlanBase(msg)
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangID}
	plan := planTurn(context.Background(), in, fake.chat)

	if plan.Fallback || len(plan.Cart) != 1 || !plan.Cart[0].VariantMissing || plan.Cart[0].Qty != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.ReplacedReason != lfReplacedCartIssue || len(plan.Issues) != 1 {
		t.Fatalf("reason=%q issues=%+v", plan.ReplacedReason, plan.Issues)
	}
	if strings.Contains(plan.Reply, "sudah masuk keranjang") {
		t.Errorf("the model's promise must not be sent while the variant is open:\n%s", plan.Reply)
	}
	if n := strings.Count(plan.Reply, "?"); n != 1 || !strings.HasPrefix(plan.Reply, "Untuk Kaos Polos Premium, Kakak mau ukuran atau warna apa?") {
		t.Errorf("exactly one question, in the template wording:\n%s", plan.Reply)
	}
}

func TestPlanVariantMustBeGroundedInTheBuyerText(t *testing.T) {
	// The model invents size XL and colour merah; the buyer only said "kaos polos".
	msg := "mau kaos polos 1"
	fake := lfTexts(`{"intent":"order","items":[{"no":4,"qty":1,"size":"XL","color":"merah"}],"reply":"Siap."}`)
	in := lfVariantPlanBase(msg)
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangID}
	plan := planTurn(context.Background(), in, fake.chat)
	if len(plan.Cart) != 1 || plan.Cart[0].Size != "" || plan.Cart[0].Color != "" || !plan.Cart[0].VariantMissing {
		t.Fatalf("invented variant accepted: %+v", plan.Cart)
	}

	// Follow-up: variant is grounded only from this turn's text (not an earlier "ukuran M" alone).
	in = lfVariantPlanBase("ukuran M warna hitam", "ukuran M")
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangID}
	in.State.Cart = plan.Cart
	in.State.PendingField = "variant"
	in.PendingAfterVariant = "recipient"
	fake = lfTexts(`{"intent":"order","items":[{"no":4,"qty":1,"size":"M","color":"hitam"}],"reply":"Oke, aku catat."}`)
	plan = planTurn(context.Background(), in, fake.chat)
	if len(plan.Cart) != 1 || plan.Cart[0].Size != "M" || plan.Cart[0].Color != "hitam" || plan.Cart[0].VariantMissing || len(plan.Issues) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if !strings.Contains(plan.Reply, "(ukuran M, warna hitam)") || !strings.Contains(plan.Reply, salesbrain.ResumeLineFor("recipient", salesbrain.LangID)) {
		t.Errorf("once the variant is known the checkout moves on:\n%s", plan.Reply)
	}
}

func TestPlanSoldOutSizeIsAnsweredHonestlyWithAlternatives(t *testing.T) {
	msg := "boxer XL 1 ya"
	fake := lfTexts(`{"intent":"order","items":[{"no":3,"qty":1}],"reply":"Siap, boxer XL sudah masuk keranjang ya."}`)
	in := lfVariantPlanBase(msg)
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangID}
	plan := planTurn(context.Background(), in, fake.chat)

	if plan.Fallback || plan.CartChanged || len(plan.Cart) != 0 || len(plan.Issues) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if strings.Contains(plan.Reply, "sudah masuk keranjang") || !strings.Contains(plan.Reply, "Boxer Pria Mono - XL sedang habis") ||
		!strings.Contains(plan.Reply, "Yang masih tersedia: L, M") {
		t.Errorf("reply:\n%s", plan.Reply)
	}

	// English buyer, same refusal in English.
	in = lfVariantPlanBase("boxer XL please")
	fake = lfTexts(`{"intent":"order","items":[{"no":3,"qty":1}],"reply":"Sure, added."}`)
	plan = planTurn(context.Background(), in, fake.chat)
	if !strings.Contains(plan.Reply, "out of stock") || !strings.Contains(plan.Reply, "Still available: L, M") || strings.Contains(plan.Reply, "habis") {
		t.Errorf("EN reply:\n%s", plan.Reply)
	}
}

func TestPlanSizeWithoutBuyerChoiceAsksTheChoicesInsteadOfGuessing(t *testing.T) {
	msg := "mau boxer mono 1 pcs"
	fake := lfTexts(`{"intent":"order","items":[{"no":1,"qty":1}],"reply":"Siap."}`)
	in := lfVariantPlanBase(msg)
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangID}
	plan := planTurn(context.Background(), in, fake.chat)
	if plan.CartChanged || len(plan.Cart) != 0 || !strings.Contains(plan.Reply, "Boxer Pria Mono ada dalam ukuran: L, M. Kakak mau yang mana?") {
		t.Fatalf("plan cart=%+v reply:\n%s", plan.Cart, plan.Reply)
	}
}

// ── apply: confirm and persistence with variants ─────────────────────────────

func lfVariantConfirmState(t *testing.T) *orderState {
	t.Helper()
	st := stateWithCart(nil, []salesbrain.OrderLine{
		{ID: "boxer-l", Name: "Boxer Pria Mono - L", Qty: 2, UnitPrice: 56900, Size: "L"},
		{ID: "boxer-m", Name: "Boxer Pria Mono - M", Qty: 1, UnitPrice: 54900, Size: "M"},
	}, lfVariantCatalog())
	st.RecipientName, st.RecipientPhone = "Ana", "+62872839472828"
	st.Street, st.City, st.Province, st.PostalCode = "Villa Kalijudan Indah A9", "Surabaya", "Jawa Timur", "60177"
	st.Step = lfStepConfirm
	return st
}

func TestConfirmSavesVariantLinesThatPassTheLegacyPersistGate(t *testing.T) {
	fx := &lfFakeFX{}
	st := lfVariantConfirmState(t)
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Pesanan diproses ya."}, salesbrain.LangID, cartFromState(st), false, "confirm")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil || fx.did("persist") != 1 {
		t.Fatalf("err=%v calls=%v", err, fx.calls)
	}
	saved := fx.persisted[0]
	if !saved.ReadyToPersist() || len(saved.Items) != 2 || saved.Items[0].Size != "L" || saved.Items[1].Size != "M" {
		t.Fatalf("the saved state must pass ReadyToPersist with both sizes: %+v", saved)
	}
	if !strings.Contains(out.Reply, "WB-") || out.OrderAction != "create" {
		t.Errorf("outcome = %+v", out)
	}
}

func TestConfirmWithAnOpenVariantAsksInsteadOfSavingOrFailing(t *testing.T) {
	fx := &lfFakeFX{}
	st := lfVariantConfirmState(t)
	st.Items = append(st.Items, orderLineState{CatalogItemID: "kaos", ProductName: "Kaos Polos Premium", Qty: 1, UnitPrice: 89000, ExternalCode: "KAOS-POLOS"})
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Pesanan diproses ya."}, salesbrain.LangID, cartFromState(st), false, "confirm")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil || fx.did("persist") != 0 || fx.did("notify") != 0 {
		t.Fatalf("nothing may be saved and nobody paged for a missing size: err=%v calls=%v", err, fx.calls)
	}
	if out.Author != "system" || out.ReplacedReason != lfReplacedVariantMissing ||
		!strings.Contains(out.Reply, "Untuk Kaos Polos Premium, Kakak mau ukuran atau warna apa?") {
		t.Fatalf("outcome = %+v", out)
	}
	if len(fx.states) != 1 || fx.states[0].Step != lfStepAskVariant {
		t.Fatalf("the checkout goes back to the variant step: %+v", fx.states)
	}
}

func TestConfirmPersistFailureKeepsTheDraftAndTellsTheOwner(t *testing.T) {
	fx := &lfFakeFX{persistErr: errors.New("stok tidak cukup")}
	st := lfVariantConfirmState(t)
	st.PersistedOrderID = "7a0b1c2d-0000-0000-0000-000000000000"
	plan := lfTestPlan(salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Pesanan diproses ya."}, salesbrain.LangEN, cartFromState(st), false, "confirm")
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, st))
	if err != nil {
		t.Fatal(err)
	}
	if out.ReplacedReason != lfReplacedPersistFailed || out.Reply != salesbrain.T(salesbrain.TplSystemBusy, salesbrain.LangEN, "Omah Apparel") {
		t.Fatalf("the buyer must not be told the order is confirmed: %+v", out)
	}
	if fx.did("clear") != 0 || fx.did("set") != 0 {
		t.Fatalf("the cart and the pinned draft stay as they were: %v", fx.calls)
	}
	if err := lfRunAfter(t, out); err != nil || len(fx.notes) != 1 || fx.notes[0].title != notifyTitleSystemError {
		t.Fatalf("the owner is told: err=%v notes=%+v", err, fx.notes)
	}
}

func TestOrderTurnAtConfirmWithNewSizeShowsTheSummaryAgain(t *testing.T) {
	// Buyer at the final OK adds an item with a missing variant, then answers it: the answer lands
	// on the full summary (items + address), not on a bare "reply OK".
	st := lfVariantConfirmState(t)
	withOpen := stateWithCart(st, append(cartFromState(st), salesbrain.OrderLine{ID: "kaos", Name: "Kaos Polos Premium", Qty: 1, UnitPrice: 89000, VariantMissing: true}), lfVariantCatalog())
	if withOpen.Step != lfStepAskVariant || pendingFieldFromState(withOpen) != "variant" {
		t.Fatalf("step=%q", withOpen.Step)
	}
	filled := cartFromState(withOpen)
	filled[2].Size, filled[2].VariantMissing = "M", false

	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentOrder, Reply: "Oke, ukuran M ya."}
	plan := turnPlan{Decision: d, Lang: salesbrain.LangID, Cart: filled, CartChanged: true, Reply: d.Reply}
	out, err := lfApplyIntent(context.Background(), fx, lfApplyBase(plan, withOpen))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Oke, ukuran M ya.", "Dikirim ke:", "Villa Kalijudan Indah A9", salesbrain.T(salesbrain.TplResumeConfirm, salesbrain.LangID)} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, out.Reply)
		}
	}
	if len(fx.states) == 0 || fx.states[0].Step != lfStepConfirm {
		t.Fatalf("states = %+v", fx.states)
	}
}

// ── whole turns ──────────────────────────────────────────────────────────────

func lfVariantLoaded(userText string, state *orderState) lfLoaded {
	ld := lfFlowLoaded(userText)
	ld.catalog = lfVariantCatalog()
	ld.state = state
	return ld
}

func TestLLMFirstFlowOrdersTwoSizesOfOneProduct(t *testing.T) {
	text := "boxer ukuran L 2 pcs dan ukuran M 1 pcs ya"
	chat := lfTexts(`{"intent":"order","items":[{"no":1,"qty":2},{"no":2,"qty":1}],"reply":"Siap Kak, aku catat ya."}`)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("id")}, lfVariantLoaded(text, nil), chat)
	if handled, err := f.run(); !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	st := lfRedisState(t, f)
	if len(st.Items) != 2 || st.Items[0].CatalogItemID != "boxer-l" || st.Items[0].Size != "L" || st.Items[0].Qty != 2 ||
		st.Items[1].CatalogItemID != "boxer-m" || st.Items[1].Size != "M" || st.Step != lfStepAskRecipient {
		t.Fatalf("state = %+v", st)
	}
	if !strings.Contains(f.sent[0].text, "Subtotal: Rp168.700") {
		t.Errorf("reply:\n%s", f.sent[0].text)
	}
}

func TestLLMFirstFlowMissingVariantAsksOnceAndWaitsWithoutADraft(t *testing.T) {
	text := "mau kaos polos 2 ya"
	chat := lfTexts(`{"intent":"order","items":[{"no":4,"qty":2}],"reply":"Siap Kak, kaosnya sudah masuk."}`)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("id")}, lfVariantLoaded(text, nil), chat)
	if handled, err := f.run(); !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got := strings.Join(f.events, ","); got != "send" {
		t.Fatalf("no draft is written while the variant is open, events = %s", got)
	}
	st := lfRedisState(t, f)
	if st.Step != lfStepAskVariant || len(st.Items) != 1 || st.Items[0].Qty != 2 || st.Items[0].Size != "" {
		t.Fatalf("state = %+v", st)
	}
	sent := f.sent[0]
	if !strings.Contains(sent.text, "Untuk Kaos Polos Premium, Kakak mau ukuran atau warna apa?") || strings.Contains(sent.text, "sudah masuk") ||
		sent.meta.ReplacedReason != lfReplacedCartIssue {
		t.Fatalf("sent = %+v", sent)
	}

	// Next turn: the buyer answers; the line is filled, the draft is saved and the checkout moves on.
	text = "M hitam"
	chat = lfTexts(`{"intent":"order","items":[{"no":4,"qty":2,"size":"M","color":"hitam"}],"reply":"Oke, aku catat."}`)
	f = newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("id")}, lfVariantLoaded(text, &st), chat)
	if handled, err := f.run(); !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	st2 := lfRedisState(t, f)
	if st2.Step != lfStepAskRecipient || len(st2.Items) != 1 || st2.Items[0].Size != "M" || st2.Items[0].Color != "hitam" {
		t.Fatalf("state = %+v", st2)
	}
	if !strings.Contains(f.sent[0].text, "(ukuran M, warna hitam)") || !strings.Contains(f.sent[0].text, salesbrain.ResumeLineFor("recipient", salesbrain.LangID)) {
		t.Errorf("reply:\n%s", f.sent[0].text)
	}
	if st2.PersistedOrderID == "" {
		t.Errorf("a complete cart is saved as a draft and the draft id is pinned in the state: %+v", st2)
	}
	if st.PersistedOrderID != "" {
		t.Errorf("no draft while the variant was open: %+v", st)
	}
}

func TestLLMFirstFlowSoldOutSizeWritesNothingAndOffersTheOthers(t *testing.T) {
	text := "boxer XL 1 ya"
	chat := lfTexts(`{"intent":"order","items":[{"no":3,"qty":1}],"reply":"Siap, boxer XL sudah masuk keranjang."}`)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("id")}, lfVariantLoaded(text, nil), chat)
	if handled, err := f.run(); !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if cmds := f.redis.commands(); len(cmds) != 0 {
		t.Errorf("no cart, no state: %v", cmds)
	}
	if got := strings.Join(f.events, ","); got != "send" {
		t.Errorf("events = %s", got)
	}
	reply := f.sent[0].text
	if !strings.Contains(reply, "Boxer Pria Mono - XL sedang habis") || !strings.Contains(reply, "Yang masih tersedia: L, M") || strings.Contains(reply, "sudah masuk") {
		t.Errorf("reply:\n%s", reply)
	}
}

// ── fail-closed at the confirm step ──────────────────────────────────────────

func TestLLMFirstFlowFailureAtConfirmKeepsTheDraftAndAsksForTheOKAgain(t *testing.T) {
	for name, replies := range map[string]*lfFakeChat{
		"invalid output twice": lfTexts("not json", "still not json"),
		"llm error":            {replies: []lfFakeReply{{err: errors.New("provider down")}}},
	} {
		text := "ok"
		f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfVariantLoaded(text, lfConfirmState()), replies)
		handled, err := f.run()
		if !handled || err != nil {
			t.Fatalf("%s: a turn held at confirm is handled, not handed to the legacy flow: handled=%v err=%v", name, handled, err)
		}
		if len(f.sent) != 1 || f.sent[0].author != "system" {
			t.Fatalf("%s: sent = %+v", name, f.sent)
		}
		reply := f.sent[0].text
		for _, want := range []string{
			salesbrain.T(salesbrain.TplConfirmHold, salesbrain.LangEN), "Order summary", "Yellow Tail Tuna Portion 100 Gram",
			"Shipping to:", "Villa Kalijudan Indah A9", salesbrain.T(salesbrain.TplResumeConfirm, salesbrain.LangEN),
		} {
			if !strings.Contains(reply, want) {
				t.Errorf("%s: reply missing %q:\n%s", name, want, reply)
			}
		}
		if f.sent[0].meta.ReplacedReason != lfReplacedConfirmHold || f.sent[0].meta.Pipeline != lfPipeline {
			t.Errorf("%s: meta = %+v", name, f.sent[0].meta)
		}
		for _, cmd := range f.redis.commands() {
			if strings.Contains(cmd, "ai:order") {
				t.Errorf("%s: the checkout state must not be touched: %v", name, f.redis.commands())
			}
		}
		if got := strings.Join(f.events, ","); got != "send,notify" {
			t.Errorf("%s: nothing is saved or paused, the owner is told: events = %s", name, got)
		}
		if len(f.notes) != 1 || f.notes[0].kind != notifyKindSystemError || !strings.Contains(f.notes[0].body, "TIDAK disimpan") {
			t.Errorf("%s: notes = %+v", name, f.notes)
		}
	}
}

func TestLLMFirstFlowFailureOutsideConfirmStillHandsTheTurnToTheLegacyFlow(t *testing.T) {
	text := "ok"
	cases := map[string]*orderState{
		"no checkout":           nil,
		"waiting for recipient": lfTunaState(),
		"confirm, no shipping":  func() *orderState { s := lfConfirmState(); s.Street = ""; return s }(),
	}
	for name, state := range cases {
		f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("id")}, lfVariantLoaded(text, state), lfTexts("not json", "not json"))
		handled, err := f.run()
		if handled || err != nil || len(f.sent) != 0 {
			t.Errorf("%s: handled=%v err=%v sent=%v", name, handled, err, f.sent)
		}
	}
}

// ── token events ─────────────────────────────────────────────────────────────

func lfCaptureTokenSink(t *testing.T) *[]usage.LLMFirstTokens {
	t.Helper()
	var got []usage.LLMFirstTokens
	old := llmFirstTokenSink
	llmFirstTokenSink = func(_ context.Context, _ string, tok usage.LLMFirstTokens) error {
		got = append(got, tok)
		return nil
	}
	t.Cleanup(func() { llmFirstTokenSink = old })
	return &got
}

func lfUsageChat(text string) *lfFakeChat {
	return &lfFakeChat{replies: []lfFakeReply{{text: text, usage: llm.Usage{
		Provider: "gemini", Model: "gemini-3.1-flash-lite", InputTokens: 1000, OutputTokens: 100, ThoughtsTokens: 20,
	}}}}
}

func TestLLMFirstTokensAreRecordedSeparatelyLiveAndShadow(t *testing.T) {
	text := "Is it frozen or freshly cut?"
	reply := `{"intent":"question","reply":"It's sold frozen and vacuum-sealed. Anything else?"}`

	got := lfCaptureTokenSink(t)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded(text), lfUsageChat(reply))
	if handled, err := f.run(); !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(*got) != 1 {
		t.Fatalf("live: want one event, got %+v", *got)
	}
	ev := (*got)[0]
	if ev.Pipeline != "llm_first" || ev.Provider != "gemini" || ev.Model != "gemini-3.1-flash-lite" || ev.Intent != "question" || ev.Fallback ||
		ev.InputTokens != 1000 || ev.OutputTokens != 100 || ev.ThoughtsTokens != 20 || ev.ConversationID != lfFlowConvo {
		t.Fatalf("live event = %+v", ev)
	}

	*got = nil
	f = newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded(text), lfUsageChat(reply))
	f.s.shadowLLMFirst(context.Background(), f.turn)
	if len(*got) != 1 || (*got)[0].Pipeline != lfShadowPurpose || (*got)[0].InputTokens != 1000 {
		t.Fatalf("shadow: want one llm_first_shadow event, got %+v", *got)
	}
	if len(f.sent) != 0 || len(f.events) != 0 {
		t.Errorf("shadow still writes nothing else: sent=%v events=%v", f.sent, f.events)
	}

	// A fallback spent tokens too.
	*got = nil
	bad := &lfFakeChat{replies: []lfFakeReply{
		{text: "not json", usage: llm.Usage{Model: "m", InputTokens: 10, OutputTokens: 1}},
		{text: "not json", usage: llm.Usage{Model: "m", InputTokens: 10, OutputTokens: 1}},
	}}
	f = newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded(text), bad)
	if handled, err := f.run(); handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(*got) != 1 || !(*got)[0].Fallback || (*got)[0].InputTokens != 20 || (*got)[0].Attempts != 2 {
		t.Fatalf("fallback event = %+v", *got)
	}
}

func TestLLMFirstTokenEventDoesNotTouchThePlanQuota(t *testing.T) {
	if usage.EventAITokenLLMFirst == "ai_token" || usage.EventAITokenLLMFirst != "ai_token_llm_first" {
		t.Fatalf("the analytics event must be its own type, got %q", usage.EventAITokenLLMFirst)
	}
	if err := usage.RecordLLMFirstTokens(context.Background(), "t_x", usage.LLMFirstTokens{Pipeline: "llm_first"}); err != nil {
		t.Fatalf("a turn without tokens records nothing and fails nothing: %v", err)
	}
}

// ── chat model default ───────────────────────────────────────────────────────

func TestLLMFirstChatDefaultsToHaikuNotSonnet(t *testing.T) {
	cfg := lfAnthropicConfig()
	if cfg.Model != DefaultHaikuAPIID() || cfg.Model == defaultAnthropicModel {
		t.Fatalf("model = %q, want Haiku %q (Sonnet is %q)", cfg.Model, DefaultHaikuAPIID(), defaultAnthropicModel)
	}
	p, ok := LLMRegistry("key", cfg).Get("anthropic")
	if !ok {
		t.Fatal("anthropic provider must always be registered")
	}
	if got := p.(*AnthropicProvider).Client.model; got != DefaultHaikuAPIID() {
		t.Fatalf("an Anthropic chain link without a model resolves to %q, want Haiku", got)
	}
}

// lfRedisState reads the checkout state the turn wrote to Redis.
func lfRedisState(t *testing.T, f *lfFlow) orderState {
	t.Helper()
	raw := f.redis.value("ai:order:t1:c1")
	if raw == "" {
		t.Fatal("no checkout state in Redis")
	}
	var st orderState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("state: %v", err)
	}
	return st
}
