package ai

import (
	"context"
	"errors"
	"testing"
)

func TestIncrementAIAttemptRedisUnavailable(t *testing.T) {
	saved := svc
	t.Cleanup(func() { svc = saved })

	svc = &AutoReplyService{rdb: nil}
	got := incrementAIAttempt(context.Background(), "msg-test-1")
	if got != maxInboundAIAttempts {
		t.Fatalf("attempt=%d want %d when Redis unavailable", got, maxInboundAIAttempts)
	}
}

func TestErrAIHandoffPausedIsDistinct(t *testing.T) {
	if !errors.Is(ErrAIHandoffPaused, ErrAIHandoffPaused) {
		t.Fatal("expected sentinel ErrAIHandoffPaused")
	}
	if ErrAIHandoffPaused.Error() != "AI_HANDOFF_PAUSED" {
		t.Fatalf("message=%q", ErrAIHandoffPaused.Error())
	}
}

func TestOrderStatusNeedsStockPrecheck(t *testing.T) {
	cases := []struct {
		current, next string
		want          bool
	}{
		{"draft", "processing", true},
		{"confirmed", "processing", true},
		{"paid", "processing", true},
		{"processing", "processing", false},
		{"draft", "confirmed", false},
		{"completed", "processing", false},
	}
	for _, tc := range cases {
		got := orderStatusNeedsStockPrecheck(tc.current, tc.next)
		if got != tc.want {
			t.Fatalf("orderStatusNeedsStockPrecheck(%q,%q)=%v want %v", tc.current, tc.next, got, tc.want)
		}
	}
}
