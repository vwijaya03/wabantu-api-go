package salesbrain

import (
	"errors"
	"testing"
)

func TestParseDecisionHappyPath(t *testing.T) {
	raw := "```json\n{\"intent\":\"order\",\"items\":[{\"no\":3,\"qty\":1},{\"no\":5,\"qty\":2}],\"language_switch\":null,\"handoff\":null,\"reply\":\"Siap Kak, aku catat ya.\"}\n```"
	d, err := ParseDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Intent != IntentOrder || len(d.Items) != 2 || d.Items[1] != (ItemOp{No: 5, Qty: 2}) || d.Reply != "Siap Kak, aku catat ya." {
		t.Fatalf("got %+v", d)
	}
}

func TestParseDecisionNormalises(t *testing.T) {
	d, err := ParseDecision(`{"intent":"weird","items":[{"no":1,"qty":1}],"language_switch":"EN","handoff":{"level":"panic"},"reply":" hi "}`)
	if err != nil {
		t.Fatal(err)
	}
	if d.Intent != IntentOther {
		t.Errorf("unknown intent must become other, got %q", d.Intent)
	}
	if d.Items != nil {
		t.Errorf("items only allowed for order/amend, got %+v", d.Items)
	}
	if d.LanguageSwitch != LangEN {
		t.Errorf("language_switch EN must normalise to en, got %q", d.LanguageSwitch)
	}
	if d.Handoff != nil {
		t.Errorf("invalid handoff level must be dropped, got %+v", d.Handoff)
	}
	if d.Reply != "hi" {
		t.Errorf("reply must be trimmed, got %q", d.Reply)
	}
}

func TestParseDecisionDropsBadItems(t *testing.T) {
	d, err := ParseDecision(`{"intent":"amend","items":[{"no":0,"qty":1},{"no":2,"qty":-1},{"no":3,"qty":0}],"reply":"ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 1 || d.Items[0] != (ItemOp{No: 3, Qty: 0}) {
		t.Fatalf("got %+v, want only {3 0}", d.Items)
	}
}

func TestParseDecisionErrors(t *testing.T) {
	if _, err := ParseDecision("sorry I cannot"); !errors.Is(err, ErrNoJSON) {
		t.Errorf("want ErrNoJSON, got %v", err)
	}
	if _, err := ParseDecision(`{"intent":"question","reply":"   "}`); !errors.Is(err, ErrEmptyReply) {
		t.Errorf("want ErrEmptyReply, got %v", err)
	}
	if _, err := ParseDecision(`{"intent":"question","reply":"x",}`); err == nil {
		t.Error("broken JSON must fail")
	}
}

func TestParseDecisionFields(t *testing.T) {
	d, err := ParseDecision(`{"intent":"field_answer","fields":{"name":"Ana","phone":"0872839472828","street":"Villa Kalijudan Indah A9","city":"Surabaya","province":"Jawa Timur","postal_code":"60177"},"reply":"Thanks Ana."}`)
	if err != nil || d.Fields == nil || d.Fields.City != "Surabaya" || d.Fields.PostalCode != "60177" {
		t.Fatalf("got %+v err=%v", d, err)
	}
	d, _ = ParseDecision(`{"intent":"question","fields":{"name":"X"},"reply":"ok"}`)
	if d.Fields != nil {
		t.Fatal("fields only kept for field_answer")
	}
}

func TestParseDecisionFieldsAreTrimmed(t *testing.T) {
	d, err := ParseDecision(`{"intent":"field_answer","fields":{"name":"  Ana ","phone":" 0872839472828\n","street":" Villa Kalijudan Indah A9 ","district":" Sukolilo","city":"Surabaya ","province":" Jawa Timur ","postal_code":" 60177 "},"reply":"Thanks Ana."}`)
	if err != nil || d.Fields == nil {
		t.Fatalf("got %+v err=%v", d, err)
	}
	want := FieldData{Name: "Ana", Phone: "0872839472828", Street: "Villa Kalijudan Indah A9", District: "Sukolilo", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177"}
	if *d.Fields != want {
		t.Fatalf("got %+v, want %+v", *d.Fields, want)
	}
}

func TestParseDecisionFieldsNullStaysNil(t *testing.T) {
	d, err := ParseDecision(`{"intent":"field_answer","fields":null,"reply":"Which address?"}`)
	if err != nil || d.Fields != nil {
		t.Fatalf("null fields must stay nil without panicking, got %+v err=%v", d.Fields, err)
	}
}

func TestParseDecisionFieldsAcceptsNumbers(t *testing.T) {
	d, err := ParseDecision(`{"intent":"field_answer","fields":{"name":"Ana","city":"Surabaya","postal_code":60177},"reply":"Thanks."}`)
	if err != nil || d.Fields == nil || d.Fields.PostalCode != "60177" {
		t.Fatalf("got %+v err=%v", d.Fields, err)
	}
}

func TestParseDecisionCancelScope(t *testing.T) {
	cases := []struct {
		raw  string
		want CancelScope
	}{
		{`{"intent":"cancel","cancel_scope":"all","reply":"x"}`, CancelAll},
		{`{"intent":"cancel","cancel_scope":" ALL ","reply":"x"}`, CancelAll},
		{`{"intent":"cancel","cancel_scope":"one","reply":"x"}`, CancelOne},
		{`{"intent":"cancel","cancel_scope":"everything","reply":"x"}`, ""},
		{`{"intent":"cancel","reply":"x"}`, ""},
		{`{"intent":"question","cancel_scope":"all","reply":"x"}`, ""}, // only a cancel carries a scope
	}
	for _, c := range cases {
		d, err := ParseDecision(c.raw)
		if err != nil {
			t.Fatal(err)
		}
		if d.CancelScope != c.want {
			t.Errorf("%s: scope = %q, want %q", c.raw, d.CancelScope, c.want)
		}
	}
}
