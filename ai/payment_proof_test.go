package ai

import (
	"testing"

	"encore.app/wabantu/aivision"
)

func TestLoadPaymentAccountsFromKB(t *testing.T) {
	cat := "payment"
	kb := []dbKBEntry{{
		Question: "Rekening transfer?",
		Answer:   "BCA 1234567890 atas nama Toko Sejahtera",
		Category: &cat,
		IsActive: true,
	}}
	accounts := loadPaymentAccountsFromKB(kb)
	if len(accounts) == 0 {
		t.Fatal("expected accounts from KB")
	}
	found := false
	for _, a := range accounts {
		if a.AccountNumber == "1234567890" {
			found = true
		}
	}
	if !found {
		t.Fatalf("account number not parsed: %+v", accounts)
	}
}

func TestEvaluatePaymentProofRulesAmountMatch(t *testing.T) {
	total := 150000.0
	target := &persistedOrder{Total: total}
	ocr := aivision.PaymentProofExtract{
		Amount:        150500,
		Bank:          "BCA",
		AccountNumber: "1234567890",
		AccountName:   "Toko Sejahtera",
		Confidence:    0.98,
	}
	accounts := []paymentAccount{{Bank: "BCA", AccountNumber: "1234567890", AccountName: "Toko Sejahtera"}}
	ok, flags := evaluatePaymentProofRules(target, ocr, accounts)
	if !ok {
		t.Fatalf("expected match, flags=%v", flags)
	}
}

func TestEvaluatePaymentProofRulesMismatchAmount(t *testing.T) {
	target := &persistedOrder{Total: 200000}
	ocr := aivision.PaymentProofExtract{Amount: 100000, Bank: "BCA", AccountNumber: "1234567890"}
	accounts := []paymentAccount{{Bank: "BCA", AccountNumber: "1234567890"}}
	ok, flags := evaluatePaymentProofRules(target, ocr, accounts)
	if ok {
		t.Fatal("expected mismatch")
	}
	found := false
	for _, f := range flags {
		if f == "mismatch_amount" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mismatch_amount flag, got %v", flags)
	}
}

func TestEvaluatePaymentProofRulesKBEmpty(t *testing.T) {
	target := &persistedOrder{Total: 100000}
	ocr := aivision.PaymentProofExtract{Amount: 100000}
	ok, flags := evaluatePaymentProofRules(target, ocr, nil)
	if ok || flags[0] != "kb_empty" {
		t.Fatalf("expected kb_empty, ok=%v flags=%v", ok, flags)
	}
}

func TestIsPaymentKBEntry(t *testing.T) {
	if !isPaymentKBEntry("nomor rekening?", "", "BCA 123") {
		t.Fatal("rekening question should match")
	}
	if isPaymentKBEntry("jam buka?", "", "08-17") {
		t.Fatal("non payment should not match")
	}
}

func TestIsPaymentProofInbound(t *testing.T) {
	if !IsPaymentProofInbound("image", "bukti bayar untuk pesanan WB-58D662BC") {
		t.Fatal("payment proof caption should skip AI")
	}
	if IsPaymentProofInbound("image", "kamu punya barang ini gak min ?") {
		t.Fatal("product image caption should not skip AI")
	}
	if !IsPaymentProofInbound("image", "") {
		t.Fatal("image without caption should skip AI for payment pipeline")
	}
	if IsPaymentProofInbound("text", "sudah transfer") {
		t.Fatal("text should not be payment proof inbound")
	}
}

func TestIsPayablePaymentOrder(t *testing.T) {
	if !isPayablePaymentOrder(&persistedOrder{Status: "draft", PaymentStatus: "unpaid"}) {
		t.Fatal("unpaid draft should be payable")
	}
	if isPayablePaymentOrder(&persistedOrder{Status: "processing", PaymentStatus: "verified"}) {
		t.Fatal("verified should not be payable")
	}
}

func TestPaymentStatusLabelID(t *testing.T) {
	if paymentStatusLabelID("proof_submitted") != "bukti transfer perlu dicek" {
		t.Fatalf("unexpected label: %s", paymentStatusLabelID("proof_submitted"))
	}
}
