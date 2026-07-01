package order

import "testing"

func TestParsePaymentProofMeta(t *testing.T) {
	raw := []byte(`{"rejectionCount":3,"proofBlocked":false,"amount":100000}`)
	meta := ParsePaymentProofMeta(raw)
	if meta.RejectionCount != 3 {
		t.Fatalf("rejectionCount: %d", meta.RejectionCount)
	}
	if meta.Amount == nil || *meta.Amount != 100000 {
		t.Fatalf("amount: %+v", meta.Amount)
	}
}

func TestIsPaymentProofBlocked(t *testing.T) {
	if IsPaymentProofBlocked(PaymentProofMeta{RejectionCount: 4}) {
		t.Fatal("4 rejections should not be blocked")
	}
	if !IsPaymentProofBlocked(PaymentProofMeta{RejectionCount: 5}) {
		t.Fatal("5 rejections should be blocked")
	}
	if !IsPaymentProofBlocked(PaymentProofMeta{ProofBlocked: true}) {
		t.Fatal("proofBlocked flag should block")
	}
}

func TestIncrementPaymentRejection(t *testing.T) {
	meta := PaymentProofMeta{RejectionCount: 4}
	meta = IncrementPaymentRejection(meta)
	if meta.RejectionCount != 5 || !meta.ProofBlocked {
		t.Fatalf("expected block at 5, got %+v", meta)
	}
}

func TestResetPaymentProofBlock(t *testing.T) {
	meta := PaymentProofMeta{RejectionCount: 5, ProofBlocked: true, BlockedNotified: true}
	meta = ResetPaymentProofBlock(meta)
	if meta.RejectionCount != 0 || meta.ProofBlocked || meta.BlockedNotified {
		t.Fatalf("expected full reset, got %+v", meta)
	}
}
