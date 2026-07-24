package finance

import (
	"context"
	"testing"
)

func TestRecordInventoryEntryZeroAmountNoDBCall(t *testing.T) {
	err := RecordInventoryEntry(
		context.Background(), "does_not_exist", "user-1",
		"ref-1", "expense", "HPP / COGS", "test", 0, "",
	)
	if err != nil {
		t.Fatalf("RecordInventoryEntry(amount=0) = %v, want nil", err)
	}
}

func TestRecordInventoryEntryInvalidFlowNoDBCall(t *testing.T) {
	err := RecordInventoryEntry(
		context.Background(), "does_not_exist", "user-1",
		"ref-1", "transfer", "HPP / COGS", "test", 1000, "",
	)
	if err == nil {
		t.Fatal("RecordInventoryEntry(invalid flow) = nil, want error")
	}
}
