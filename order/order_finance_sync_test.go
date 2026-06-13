package order

import "testing"

func TestShouldResyncCompletedOrderIncomeWalletOnly(t *testing.T) {
	if !shouldResyncCompletedOrderIncome("", "completed", nil, nil, true) {
		t.Fatal("wallet change on completed order should trigger resync")
	}
}

func TestShouldResyncCompletedOrderIncomeTotalOnly(t *testing.T) {
	sub := 100.0
	if !shouldResyncCompletedOrderIncome("", "completed", &sub, nil, false) {
		t.Fatal("subtotal change on completed order should trigger resync")
	}
	shipping := 10.0
	if !shouldResyncCompletedOrderIncome("", "completed", nil, &shipping, false) {
		t.Fatal("shipping change on completed order should trigger resync")
	}
}

func TestShouldResyncCompletedOrderIncomeSkipsStatusTransition(t *testing.T) {
	cases := []string{"completed", "draft", "cancelled", "processing"}
	for _, status := range cases {
		if shouldResyncCompletedOrderIncome(status, "completed", nil, nil, true) {
			t.Fatalf("should not resync when newStatus=%q (handled by status branch)", status)
		}
	}
}

func TestShouldResyncCompletedOrderIncomeSkipsNonCompleted(t *testing.T) {
	sub := 50.0
	if shouldResyncCompletedOrderIncome("", "processing", &sub, nil, true) {
		t.Fatal("non-completed order should not resync")
	}
	if shouldResyncCompletedOrderIncome("", "draft", nil, nil, true) {
		t.Fatal("draft order wallet change should not resync income")
	}
}

func TestShouldResyncCompletedOrderIncomeNoOp(t *testing.T) {
	if shouldResyncCompletedOrderIncome("", "completed", nil, nil, false) {
		t.Fatal("no financial fields changed should not resync")
	}
}
