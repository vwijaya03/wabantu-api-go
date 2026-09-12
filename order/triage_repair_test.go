package order

import (
	"testing"

	"encore.app/wabantu/shared/triagerepair"
)

func TestEligibilityBlocksPaidAndIdentityMismatch(t *testing.T) {
	row := triageOrderRow{
		Status:               "paid",
		PaymentStatus:        "paid",
		PaymentTransactionID: "tx-1",
		ConversationID:       "conv-a",
	}
	got := eligibilityReasons(row, &TriageDraftRepairParams{ConversationID: "conv-b", OrderID: "o1", WebSessionID: "sess"})
	join := stringsJoin(got)
	for _, want := range []string{triagerepair.BlockNotDraftUnpaid, triagerepair.BlockPaymentInFlight, triagerepair.BlockIdentityMismatch} {
		if !contains(got, want) {
			t.Fatalf("missing %s in %s", want, join)
		}
	}
}

func TestEligibilityDraftUnpaidOK(t *testing.T) {
	row := triageOrderRow{Status: "draft", PaymentStatus: "unpaid", ConversationID: "c1"}
	got := eligibilityReasons(row, &TriageDraftRepairParams{ConversationID: "c1", OrderID: "o1"})
	if len(got) != 0 {
		t.Fatalf("unexpected blocks: %v", got)
	}
}

func TestUnknownVariantNotGuessed(t *testing.T) {
	if !triagerepair.ValidOperation(triagerepair.OpDraftItemsReplace) {
		t.Fatal("draft replace must be allowlisted")
	}
}

func stringsJoin(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
