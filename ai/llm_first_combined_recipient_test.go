package ai

import (
	"context"
	"strings"
	"testing"

	"encore.app/wabantu/internal/salesbrain"
)

func TestResumeRecipientAsksNamePhoneAndAddress(t *testing.T) {
	for _, lang := range []salesbrain.Lang{salesbrain.LangID, salesbrain.LangEN} {
		got := strings.ToLower(salesbrain.T(salesbrain.TplResumeRecipient, lang))
		for _, want := range []string{"jl. melati 12", "081234567890"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s template missing %q: %s", lang, want, got)
			}
		}
		if !strings.Contains(got, "alamat") && !strings.Contains(got, "address") {
			t.Errorf("%s template must ask the address: %s", lang, got)
		}
	}
}

func TestFieldAnswerCombinedMessageIDReachesConfirm(t *testing.T) {
	st := lfTunaState()
	text := "Nama: Ana\nHP: 0872839472828\nAlamat: Villa Kalijudan Indah A9, Surabaya, Jawa Timur, 60177"
	problems := applyFieldAnswer(st, &salesbrain.FieldData{
		Name: "Ana", Phone: "0872839472828", Street: "Villa Kalijudan Indah A9",
		City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177",
	}, text, salesbrain.LangID)
	if len(problems) != 0 || !st.ShippingComplete() || st.Step != "confirm" {
		t.Fatalf("combined ID message must confirm in one turn: problems=%v step=%q st=%+v", problems, st.Step, *st)
	}
}

func TestFieldAnswerCombinedMessageWithoutModelFieldsStillParsed(t *testing.T) {
	st := lfTunaState()
	text := "Name: Budi\nPhone: 081234567890\nAddress: Jl. Melati 12, Surabaya, Jawa Timur, 60111"
	applyFieldAnswer(st, &salesbrain.FieldData{}, text, salesbrain.LangEN)
	if !st.ShippingComplete() || st.Step != "confirm" {
		t.Fatalf("the deterministic parser must fill everything: step=%q st=%+v", st.Step, *st)
	}
}

func TestApplyFieldsPartialNamePhoneAsksAddressOnly(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentFieldAnswer, Reply: "Thanks!", Fields: &salesbrain.FieldData{Name: "Ana", Phone: "0872839472828"}}
	plan := lfTestPlan(d, salesbrain.LangEN, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	in := lfApplyBase(plan, lfTunaState())
	in.UserText = "Ana 0872839472828"
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if fx.states[0].Step != lfStepAskAddressFull {
		t.Fatalf("step = %q", fx.states[0].Step)
	}
	if !strings.Contains(out.Reply, "Still missing: street name and number, city, province, 5-digit postal code") {
		t.Fatalf("must name exactly the address parts:\n%s", out.Reply)
	}
	if strings.Contains(out.Reply, "recipient name") {
		t.Fatalf("must not ask the name again:\n%s", out.Reply)
	}
}

func TestApplyFieldsAddressOnlyAsksNameAndPhone(t *testing.T) {
	fx := &lfFakeFX{}
	d := salesbrain.Decision{Intent: salesbrain.IntentFieldAnswer, Reply: "Thanks!", Fields: &salesbrain.FieldData{
		Street: "Jl. Melati 12", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60111"}}
	plan := lfTestPlan(d, salesbrain.LangEN, []salesbrain.OrderLine{lfTunaLine()}, false, "recipient")
	in := lfApplyBase(plan, lfTunaState())
	in.UserText = "Address: Jl. Melati 12, Surabaya, Jawa Timur, 60111"
	out, err := lfApplyIntent(context.Background(), fx, in)
	if err != nil {
		t.Fatal(err)
	}
	if fx.states[0].Step != lfStepAskRecipient || fx.states[0].City != "Surabaya" {
		t.Fatalf("address kept, still on the recipient step: %+v", fx.states[0])
	}
	if !strings.Contains(out.Reply, "Still missing: recipient name, phone number") {
		t.Fatalf("must name the name and phone:\n%s", out.Reply)
	}
	if strings.Contains(out.Reply, "street name") {
		t.Fatalf("must not ask the address again as missing:\n%s", out.Reply)
	}
}
