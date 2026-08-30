package kb

import "testing"

func TestEntityPercentComplete(t *testing.T) {
	kb := EntityIndexCounts{Indexed: 8, Pending: 2, Total: 10}
	cat := EntityIndexCounts{Indexed: 5, Pending: 5, Total: 10}
	got := entityPercentComplete(kb, cat)
	if got != 65 {
		t.Fatalf("got %d want 65", got)
	}
}

func TestEntityPercentCompleteEmpty(t *testing.T) {
	if entityPercentComplete(EntityIndexCounts{}, EntityIndexCounts{}) != 100 {
		t.Fatal("empty tenant should be 100%")
	}
}

func TestOutboxPercentDone(t *testing.T) {
	o := OutboxCounts{Done: 3, Pending: 1, Total: 4}
	if outboxPercentDone(o) != 75 {
		t.Fatalf("got %d want 75", outboxPercentDone(o))
	}
}

func TestOutboxPercentDoneEmpty(t *testing.T) {
	if outboxPercentDone(OutboxCounts{}) != 100 {
		t.Fatal("empty outbox should be 100%")
	}
}
