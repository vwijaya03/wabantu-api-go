package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"encore.app/wabantu/aivision"
	"encore.app/wabantu/order"
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

func TestLoadPaymentAccountsFromKBSpacedNumbers(t *testing.T) {
	cat := "Nomor Rekening"
	kb := []dbKBEntry{{
		Question: "Nomor Rekening",
		Answer:   "BCA 110 220 330 atas nama Omah Apparel\nMandiri 311 211 111 atas nama Omah Apparel",
		Category: &cat,
		IsActive: true,
	}}
	accounts := loadPaymentAccountsFromKB(kb)
	if len(accounts) < 2 {
		t.Fatalf("expected at least 2 accounts, got %+v", accounts)
	}
	foundBCA := false
	for _, a := range accounts {
		if a.AccountNumber == "110220330" && a.Bank == "BCA" {
			foundBCA = true
		}
	}
	if !foundBCA {
		t.Fatalf("spaced BCA account not parsed: %+v", accounts)
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

func TestMarkPaymentProofAIReviewFlags(t *testing.T) {
	flags := markPaymentProofAIReviewFlags("auto_verify", false, "proof_submitted", []string{"mismatch_amount"})
	if !containsFlag(flags, "ai_review_required") {
		t.Fatalf("expected ai_review_required, got %v", flags)
	}
	flags = markPaymentProofAIReviewFlags("auto_verify", true, "verified", []string{})
	if containsFlag(flags, "ai_review_required") {
		t.Fatalf("verified should not add ai_review_required: %v", flags)
	}
	flags = markPaymentProofAIReviewFlags("manual", false, "proof_submitted", nil)
	if containsFlag(flags, "ai_review_required") {
		t.Fatalf("manual mode should not add ai_review_required: %v", flags)
	}
}

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
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
	if !IsPaymentProofInbound("image", "WB-58D662BC") {
		t.Fatal("image with order ref only should skip AI")
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

func TestPaymentProofBuyerMessage(t *testing.T) {
	got := paymentProofBuyerMessage("WB-ABC", "proof_submitted", "rejected", "")
	if !strings.Contains(got, "baru") {
		t.Fatalf("resubmit message: %q", got)
	}
	dup := paymentProofBuyerMessage("WB-ABC", "rejected", "", "WB-XYZ")
	if !strings.Contains(dup, "WB-XYZ") {
		t.Fatalf("duplicate message: %q", dup)
	}
}

func TestIsPayablePaymentOrder(t *testing.T) {
	if !isPayablePaymentOrder(&persistedOrder{Status: "draft", PaymentStatus: "unpaid"}) {
		t.Fatal("unpaid draft should be payable")
	}
	if isPayablePaymentOrder(&persistedOrder{Status: "processing", PaymentStatus: "verified"}) {
		t.Fatal("verified should not be payable")
	}
	blockedMeta, _ := json.Marshal(order.PaymentProofMeta{RejectionCount: 5, ProofBlocked: true})
	if isPayablePaymentOrder(&persistedOrder{
		Status:               "draft",
		PaymentStatus:        "rejected",
		PaymentProofMetaJSON: blockedMeta,
	}) {
		t.Fatal("blocked order should not be payable")
	}
}

func TestIsOrderPaymentProofBlocked(t *testing.T) {
	meta, _ := json.Marshal(order.PaymentProofMeta{ProofBlocked: true})
	if !isOrderPaymentProofBlocked(&persistedOrder{PaymentProofMetaJSON: meta}) {
		t.Fatal("expected blocked")
	}
}

func TestPaymentStatusLabelID(t *testing.T) {
	if paymentStatusLabelID("proof_submitted") != "bukti transfer perlu dicek" {
		t.Fatalf("unexpected label: %s", paymentStatusLabelID("proof_submitted"))
	}
}

func TestPaymentProofDoneKey(t *testing.T) {
	if got := paymentProofDoneKey("msg-1"); got != paymentProofDoneKeyPrefix+"msg-1" {
		t.Fatalf("unexpected key %q", got)
	}
	if paymentProofDoneKey("") != "" {
		t.Fatal("empty inbound id must not mint a redis key")
	}
}
