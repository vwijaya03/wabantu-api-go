package order

import (
	"strings"
	"testing"
)

func TestValidPaymentStatuses(t *testing.T) {
	for _, s := range []string{"unpaid", "proof_submitted", "verified", "rejected"} {
		if !validPaymentStatuses[s] {
			t.Fatalf("expected valid payment status %q", s)
		}
	}
	if validPaymentStatuses["bogus"] {
		t.Fatal("bogus should not be valid")
	}
}

func TestApplyPaymentProofMeta(t *testing.T) {
	o := &Order{}
	raw := []byte(`{"amount":100000,"bank":"BCA","confidence":0.92,"flags":["mismatch_amount"]}`)
	applyPaymentProofMeta(o, raw)
	if o.PaymentProofMeta == nil {
		t.Fatal("expected meta")
	}
	if o.PaymentProofMeta.Amount == nil || *o.PaymentProofMeta.Amount != 100000 {
		t.Fatalf("amount: %+v", o.PaymentProofMeta)
	}
	if o.PaymentProofMeta.Bank != "BCA" {
		t.Fatalf("bank: %q", o.PaymentProofMeta.Bank)
	}
	if len(o.PaymentProofMeta.Flags) != 1 || o.PaymentProofMeta.Flags[0] != "mismatch_amount" {
		t.Fatalf("flags: %+v", o.PaymentProofMeta.Flags)
	}
}

func TestOrderSelectColsPaymentProofFields(t *testing.T) {
	cols := orderSelectCols("")
	required := []string{
		"payment_status", "payment_proof_message_id",
		"payment_proof_submitted_at", "payment_proof_verified_at",
		"payment_proof_verified_by", "payment_proof_meta",
	}
	for _, f := range required {
		if !strings.Contains(cols, f) {
			t.Errorf("orderSelectCols() missing field %q", f)
		}
	}
}
