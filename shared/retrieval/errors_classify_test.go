package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShouldDLQAlignsWithPubSubRetries(t *testing.T) {
	if !ShouldDLQ(6) {
		t.Fatal("attempt 6 should DLQ (1 initial + 5 pubsub retries)")
	}
	if ShouldDLQ(5) {
		t.Fatal("attempt 5 should still retry")
	}
}

func TestIsClientTimeout(t *testing.T) {
	if !IsClientTimeout(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded is client timeout")
	}
	if ShouldTripBreaker(context.DeadlineExceeded) {
		t.Fatal("client timeout must not trip breaker")
	}
}

func TestClassifyVectorError(t *testing.T) {
	if ClassifyVectorError(errors.New("openai embed 503")) != FallbackReasonEmbedError {
		t.Fatal("expected embed error class")
	}
	if ClassifyVectorError(context.DeadlineExceeded) != FallbackReasonClientTimeout {
		t.Fatal("expected client timeout class")
	}
}

func TestBreakerPoolIsolatesTenants(t *testing.T) {
	pool := NewBreakerPool(1, time.Minute)
	a := pool.For("tenant-a")
	b := pool.For("tenant-b")
	a.RecordFailure(errors.New("down"))
	if !a.Open() {
		t.Fatal("tenant-a breaker should be open")
	}
	if b.Open() {
		t.Fatal("tenant-b breaker should remain closed")
	}
}
