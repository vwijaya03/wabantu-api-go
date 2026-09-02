package retrieval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassifyProviderError_ParentAliveBudgetExceededTripsBreaker(t *testing.T) {
	t.Parallel()
	parent := context.Background()
	child, cancel := context.WithTimeout(parent, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Nanosecond)

	re := ClassifyProviderError(parent, child.Err(), ProviderOpenAI)
	if re.Category != CategoryBudgetExceeded {
		t.Fatalf("category=%q want budget_exceeded", re.Category)
	}
	if !re.TripsBreaker {
		t.Fatal("budget_exceeded should trip breaker when parent alive")
	}
}

func TestClassifyProviderError_ParentCanceledDoesNotTrip(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	child, childCancel := context.WithTimeout(parent, time.Second)
	defer childCancel()

	re := ClassifyProviderError(parent, child.Err(), ProviderOpenAI)
	if re.Category != CategoryCallerCanceled {
		t.Fatalf("category=%q want caller_canceled", re.Category)
	}
	if re.TripsBreaker {
		t.Fatal("caller_canceled must not trip breaker")
	}
}

func TestClassifyProviderError_Provider429TripsBreaker(t *testing.T) {
	t.Parallel()
	re := ClassifyProviderError(context.Background(), errors.New("openai 429 rate limit"), ProviderOpenAI)
	if re.Category != CategoryProviderRate {
		t.Fatalf("category=%q", re.Category)
	}
	if !re.TripsBreaker {
		t.Fatal("429 should trip breaker")
	}
}

func TestClassifyProviderError_ConfigurationDoesNotTrip(t *testing.T) {
	t.Parallel()
	re := ClassifyProviderError(context.Background(), ErrServiceNotConfigured, ProviderOpenAI)
	if re.Category != CategoryConfiguration {
		t.Fatalf("category=%q", re.Category)
	}
	if re.TripsBreaker {
		t.Fatal("configuration_error must not trip breaker")
	}
}

func TestFallbackReasonFromCategory(t *testing.T) {
	t.Parallel()
	if FallbackReasonFromCategory(RetrievalError{Category: CategoryBudgetExceeded}) != FallbackReasonClientTimeout {
		t.Fatal("budget_exceeded maps to client_timeout fallback label")
	}
	if FallbackReasonFromCategory(RetrievalError{Category: CategoryProvider5xx, Provider: ProviderPinecone}) != FallbackReasonQueryError {
		t.Fatal("pinecone 5xx maps to query_error")
	}
}

func TestShouldTripBreakerUsesParentContext(t *testing.T) {
	t.Parallel()
	parent := context.Background()
	child, cancel := context.WithTimeout(parent, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Nanosecond)
	if !ShouldTripBreaker(parent, child.Err(), ProviderOpenAI) {
		t.Fatal("expected breaker trip when parent alive and budget exceeded")
	}
}

func TestIsClientTimeout(t *testing.T) {
	if !IsClientTimeout(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded is client timeout")
	}
}

func TestClassifyVectorError(t *testing.T) {
	if ClassifyVectorError(errors.New("openai embed 503")) != FallbackReasonEmbedError {
		t.Fatal("expected embed error class")
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
