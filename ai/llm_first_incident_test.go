package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/internal/salesbrain"
)

// Texts of the 18 Oct production incident (tenant omah_apparel, gemini-3.1-flash-lite).
const (
	lfIncidentRecipientText = "Name: Anastasi\nPhone: 081222000111"
	lfIncidentAddressText   = "Jl. Taman Setiabudi II No. 28, South Jakarta, DKI Jakarta, 10197"
)

func lfIncidentAddressState() *orderState {
	st := lfTunaState()
	st.Step = lfStepAskAddressFull
	st.RecipientName, st.RecipientPhone = "Anastasi", "+6281222000111"
	return st
}

// The model labelled "DKI Jakarta" as the city and "South Jakarta" as the district and left the
// province empty; the deterministic parser must still complete the address.
func TestIncidentAddressMislabelledByModelStillCompletes(t *testing.T) {
	st := lfIncidentAddressState()
	f := &salesbrain.FieldData{
		Street: "Jl. Taman Setiabudi II No. 28", District: "South Jakarta", City: "DKI Jakarta", PostalCode: "10197",
	}
	problems := applyFieldAnswer(st, f, lfIncidentAddressText, salesbrain.LangEN)

	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if !st.ShippingComplete() {
		t.Fatalf("shipping must be complete: street=%q city=%q province=%q postal=%q", st.Street, st.City, st.Province, st.PostalCode)
	}
	if st.Street != "Jl. Taman Setiabudi II No. 28" || st.Province != "DKI Jakarta" || st.City != "Jakarta Selatan" || st.PostalCode != "10197" {
		t.Fatalf("wrong fields: %+v", *st)
	}
	if st.Step != lfStepConfirm {
		t.Fatalf("step = %q, want confirm", st.Step)
	}
}

// When the parser knows nothing, the model's mislabelled city is read as the province.
func TestIncidentModelCityThatIsAProvinceBecomesProvince(t *testing.T) {
	st := lfIncidentAddressState()
	text := "Jl. Melati 5 Blok C, Kebayoran Baru, DKI Jakarta 12130"
	f := &salesbrain.FieldData{Street: "Jl. Melati 5 Blok C", District: "Kebayoran Baru", City: "DKI Jakarta", PostalCode: "12130"}
	lfApplyModelAddress(st, f, text, lfParsedFields{})

	if st.Province != "DKI Jakarta" || st.City != "Kebayoran Baru" || st.Kecamatan != "" {
		t.Fatalf("province=%q city=%q kecamatan=%q", st.Province, st.City, st.Kecamatan)
	}
	// A district that is the city the parser already recognised is ignored.
	st = lfIncidentAddressState()
	st.City = "Jakarta Selatan"
	lfApplyModelAddress(st, &salesbrain.FieldData{District: "South Jakarta"}, lfIncidentAddressText, lfParsedFields{})
	if st.Kecamatan != "" {
		t.Fatalf("district that duplicates the city must be ignored, got %q", st.Kecamatan)
	}
}

func TestIncidentRecipientTextFillsNameAndPhone(t *testing.T) {
	st := lfTunaState()
	st.Step = lfStepAskRecipient
	applyFieldAnswer(st, &salesbrain.FieldData{Name: "Anastasi", Phone: "081222000111"}, lfIncidentRecipientText, salesbrain.LangEN)
	if st.RecipientName != "Anastasi" || st.RecipientPhone != "+6281222000111" || st.Step != lfStepAskAddressFull {
		t.Fatalf("got %+v", *st)
	}
	if strings.TrimSpace(st.Street) != "" {
		t.Fatalf("no street in the text: %q", st.Street)
	}
}

const lfIncidentPaymentClaim = "Thank you, Anastasi! Your order is now ready for payment. Please transfer the total of Rp350.000 to BCA/Mandiri/BNI and send payment proof."

// 00:19:19: the model said "ready for payment" although the address was still missing.
func TestIncidentFieldAnswerNeverSendsModelClaimWhileIncomplete(t *testing.T) {
	st := lfTunaState()
	st.Step = lfStepAskRecipient
	d := salesbrain.Decision{
		Intent: salesbrain.IntentFieldAnswer, Reply: lfIncidentPaymentClaim,
		Fields: &salesbrain.FieldData{Name: "Anastasi", Phone: "081222000111"},
	}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "recipient")
	in := lfApplyBase(plan, st)
	in.UserText = lfIncidentRecipientText

	out, err := lfApplyIntent(context.Background(), &lfFakeFX{}, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"payment", "transfer", "BCA"} {
		if strings.Contains(out.Reply, bad) {
			t.Errorf("the model's claim leaked (%q):\n%s", bad, out.Reply)
		}
	}
	for _, want := range []string{"Noted: name Anastasi; phone +6281222000111.", "Still missing: street name and number, city, province, 5-digit postal code.", salesbrain.ResumeLineFor("address", salesbrain.LangEN)} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, out.Reply)
		}
	}
	if out.Author != "system" || out.ReplacedReason != lfReplacedFieldIncomplete {
		t.Errorf("outcome = %+v", out)
	}
}

// 00:20:02: the address was not stored (no province) yet the model said "updated". With the
// parser it completes; when it really does not, only what is missing is named.
func TestIncidentAddressReplyNamesWhatIsMissing(t *testing.T) {
	st := lfIncidentAddressState()
	d := salesbrain.Decision{
		Intent: salesbrain.IntentFieldAnswer, Reply: "I have successfully updated your delivery address. Please proceed with the payment.",
		Fields: &salesbrain.FieldData{Street: "Jl. Taman Setiabudi II No. 28"},
	}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "address")
	in := lfApplyBase(plan, st)
	in.UserText = "Jl. Taman Setiabudi II No. 28"

	out, _ := lfApplyIntent(context.Background(), &lfFakeFX{}, in)
	if strings.Contains(out.Reply, "successfully") || strings.Contains(out.Reply, "payment") {
		t.Fatalf("model claim leaked:\n%s", out.Reply)
	}
	if !strings.Contains(out.Reply, "Still missing: city, province, 5-digit postal code.") {
		t.Fatalf("missing parts not named:\n%s", out.Reply)
	}

	// The full incident text completes: the model's acknowledgement is allowed, with the summary.
	st = lfIncidentAddressState()
	d.Fields = &salesbrain.FieldData{Street: "Jl. Taman Setiabudi II No. 28", District: "South Jakarta", City: "DKI Jakarta", PostalCode: "10197"}
	d.Reply = "Thanks, I have your address."
	in = lfApplyBase(lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "address"), st)
	in.UserText = lfIncidentAddressText
	out, _ = lfApplyIntent(context.Background(), &lfFakeFX{}, in)
	for _, want := range []string{"Thanks, I have your address.", "Shipping to:", salesbrain.ResumeLineFor("confirm", salesbrain.LangEN)} {
		if !strings.Contains(out.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, out.Reply)
		}
	}
}

// A "confirm" while the checkout is not at the confirm step must not keep "confirmed".
func TestIncidentConfirmOutsideConfirmStepResumesPendingField(t *testing.T) {
	st := lfIncidentAddressState()
	d := salesbrain.Decision{Intent: salesbrain.IntentConfirm, Reply: "Your order is confirmed and recorded, ready for payment."}
	plan := lfTestPlan(d, salesbrain.LangEN, cartFromState(st), false, "address")
	in := lfApplyBase(plan, st)
	in.UserText = "ok"

	fx := &lfFakeFX{persistID: "nope"}
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if fx.did("persist") != 0 {
		t.Fatalf("must not persist: %v", fx.calls)
	}
	if out.Reply != salesbrain.ResumeLineFor("address", salesbrain.LangEN) || out.ReplacedReason != lfReplacedConfirmNotReady {
		t.Fatalf("outcome = %+v", out)
	}
}

// 00:19:19 raw output: payment instructions with no address and no confirmation.
func TestIncidentPrematurePaymentIsRejectedAndRetried(t *testing.T) {
	in := lfPlanBase(lfIncidentRecipientText)
	in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
	in.State.PendingField = "recipient"
	bad := `{"intent":"field_answer","fields":{"name":"Anastasi","phone":"081222000111"},"reply":"Thank you, Anastasi! Your order is now ready for payment; please transfer the total of Rp350.000 to BCA 000111222 / Mandiri 333111222 / BNI 555111222 and send payment proof."}`
	good := `{"intent":"field_answer","fields":{"name":"Anastasi","phone":"081222000111"},"reply":"Thank you, Anastasi!"}`
	fake := lfTexts(bad, good)

	plan := planTurn(context.Background(), in, fake.chat)

	if plan.Fallback || plan.Attempts != 2 {
		t.Fatalf("want a retry that succeeds, got fallback=%v attempts=%d violations=%+v", plan.Fallback, plan.Attempts, plan.Violations)
	}
	if !strings.Contains(fake.userOf(1), "Pesanan belum dikonfirmasi") {
		t.Fatalf("retry must carry the premature_payment note:\n%s", fake.userOf(1))
	}
	if strings.Contains(plan.Reply, "transfer") {
		t.Fatalf("payment instructions must not be sent: %q", plan.Reply)
	}
}

func TestIncidentPrematurePaymentPhrasesTwice(t *testing.T) {
	in := lfPlanBase(lfIncidentAddressText)
	in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
	in.State.PendingField = "address"
	bad := `{"intent":"field_answer","reply":"I have successfully updated your delivery address. Please proceed with the payment."}`
	plan := planTurn(context.Background(), in, lfTexts(bad, bad).chat)
	if !plan.Fallback || len(plan.Violations) == 0 || plan.Violations[0].Code != salesbrain.VPrematurePayment {
		t.Fatalf("want fallback on premature_payment, got %+v", plan)
	}
}

// A buyer who asks how to pay is still answered with the payment details.
func TestIncidentBuyerAskingHowToPayIsAnswered(t *testing.T) {
	for _, msg := range []string{"how do I pay?", "ke rekening mana transfernya?", "Do you accept QRIS?"} {
		in := lfPlanBase(msg)
		in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
		in.State.PendingField = "recipient"
		fake := lfTexts(`{"intent":"question","reply":"You can pay by bank transfer to BCA 000111222 (Omah Apparel) or QRIS."}`)
		plan := planTurn(context.Background(), in, fake.chat)
		if plan.Fallback || plan.Attempts != 1 || !strings.Contains(plan.Reply, "000111222") {
			t.Fatalf("%q must be answered: fallback=%v attempts=%d violations=%+v", msg, plan.Fallback, plan.Attempts, plan.Violations)
		}
	}
}

// The confirm reply at the confirm step may mention payment; an order awaiting payment too.
func TestPrematurePaymentAllowedAtConfirmAndForOpenPaymentOrder(t *testing.T) {
	in := lfPlanBase("OK")
	in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
	in.State.PendingField = "confirm"
	fake := lfTexts(`{"intent":"confirm","reply":"Thanks! Please transfer Rp350.000 to BCA 000111222 and send the payment proof."}`)
	if plan := planTurn(context.Background(), in, fake.chat); plan.Fallback || plan.Attempts != 1 {
		t.Fatalf("confirm at the confirm step may speak of payment: %+v", plan.Violations)
	}

	in = lfPlanBase("ok thanks")
	in.State.OpenOrders = []salesbrain.OrderSnapshot{{Ref: "WB-AAAA1111", Status: salesbrain.StatusAwaitingPayment, Stage: salesbrain.StagePayment, Subtotal: 350000}}
	fake = lfTexts(`{"intent":"other","reply":"Your order WB-AAAA1111 is waiting for the transfer; send the payment proof when done."}`)
	if plan := planTurn(context.Background(), in, fake.chat); plan.Fallback || plan.Attempts != 1 {
		t.Fatalf("an order awaiting payment may be discussed: %+v", plan.Violations)
	}
}

// 00:18 WIB: the model decided "past 12:00" itself. Code now hands it the answer.
func TestIncidentShipTimeLineComputedByCodeAt0018(t *testing.T) {
	in := lfShellInputs(t)
	in.KBAll = []dbKBEntry{{Question: "Kapan pesanan saya akan dikirim?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan dikirim di hari yang sama. Pesanan setelah pukul 12:00 WIB akan dikirim pada hari kerja berikutnya."}}
	in.Now = time.Date(2026, 10, 19, 0, 18, 0, 0, lfWIB) // a Monday

	got := lfBuildPlanInput(in)
	if got.State.ShipIfPaidNow != "hari ini bila dibayar antara 08:00–12:00" {
		t.Fatalf("ShipIfPaidNow = %q", got.State.ShipIfPaidNow)
	}
	_, user := salesbrain.BuildPrompt(salesbrain.PromptInput{State: got.State})
	if !strings.Contains(user, "pengiriman_jika_bayar_sekarang: hari ini bila dibayar antara 08:00–12:00") {
		t.Fatalf("prompt lacks the line:\n%s", user)
	}

	// A FAQ without a window: no line.
	in.KBAll = []dbKBEntry{{Question: "Metode bayar?", Answer: "BCA"}}
	if got := lfBuildPlanInput(in); got.State.ShipIfPaidNow != "" {
		t.Fatalf("no parseable window, got %q", got.State.ShipIfPaidNow)
	}
}
