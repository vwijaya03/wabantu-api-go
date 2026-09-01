package flag

import (
	"testing"
	"time"

	"encore.app/wabantu/shared/retrieval"
)

func TestRAGRolloutNotBefore_StaggeredByIndex(t *testing.T) {
	delayMs := 2000
	start := time.Now()
	for i := 0; i < 3; i++ {
		notBefore := start.Add(time.Duration(i*delayMs) * time.Millisecond)
		if i == 0 && notBefore.After(start.Add(10*time.Millisecond)) {
			t.Fatalf("index 0 should be immediate, got %v", notBefore.Sub(start))
		}
		if i == 2 {
			want := start.Add(4 * time.Second)
			if notBefore.Sub(want) > 20*time.Millisecond || want.Sub(notBefore) > 20*time.Millisecond {
				t.Fatalf("index 2 want ~4s offset, got %v", notBefore.Sub(start))
			}
		}
	}
}

func TestRAGRolloutScheduledErrorIsRetryable(t *testing.T) {
	err := rolloutTenantNotReady(time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error")
	}
	if !retrieval.IsRetryableError(err) {
		t.Fatalf("expected retryable rollout delay error, got %v", err)
	}
}
