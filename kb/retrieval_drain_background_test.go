package kb

import (
	"context"
	"testing"
	"time"
)

// swapDrainRunner replaces the drain implementation and returns a release func.
func swapDrainRunner(t *testing.T) (started chan string, release func()) {
	t.Helper()
	started = make(chan string, 8)
	block := make(chan struct{})
	orig := drainRunner
	drainRunner = func(_ context.Context, _ string, tenantID string) {
		started <- tenantID
		<-block
	}
	t.Cleanup(func() {
		drainRunner = orig
		select {
		case <-block:
		default:
			close(block)
		}
	})
	return started, func() { close(block) }
}

func waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		backgroundDrainMu.Lock()
		n := len(backgroundDrainInFlight)
		backgroundDrainMu.Unlock()
		if n == 0 && len(backgroundDrainSem) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("drain did not go idle")
}

func TestBackgroundDrainDisabledByDefault(t *testing.T) {
	t.Setenv(backgroundDrainEnv, "")
	started, release := swapDrainRunner(t)
	defer release()
	if scheduleBackgroundRetrievalDrain("t_a", "tenant-a") {
		t.Fatal("drain must not start when RETRIEVAL_BACKGROUND_DRAIN is unset")
	}
	select {
	case id := <-started:
		t.Fatalf("unexpected drain for %s", id)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBackgroundDrainDoesNotBlockCaller(t *testing.T) {
	t.Setenv(backgroundDrainEnv, "1")
	started, release := swapDrainRunner(t)

	returned := make(chan bool, 1)
	go func() { returned <- scheduleBackgroundRetrievalDrain("t_a", "tenant-a") }()
	select {
	case ok := <-returned:
		if !ok {
			t.Fatal("expected drain to be scheduled")
		}
	case <-time.After(time.Second):
		t.Fatal("schedule blocked on the drain: request path must return immediately")
	}
	<-started
	release()
	waitIdle(t)
}

func TestBackgroundDrainOnePerTenantAndBoundedConcurrency(t *testing.T) {
	t.Setenv(backgroundDrainEnv, "true")
	started, release := swapDrainRunner(t)

	if !scheduleBackgroundRetrievalDrain("t_a", "tenant-a") {
		t.Fatal("first tenant should schedule")
	}
	if scheduleBackgroundRetrievalDrain("t_a", "tenant-a") {
		t.Fatal("same tenant must not drain twice concurrently")
	}
	if !scheduleBackgroundRetrievalDrain("t_b", "tenant-b") {
		t.Fatal("second tenant should schedule (cap is 2)")
	}
	if scheduleBackgroundRetrievalDrain("t_c", "tenant-c") {
		t.Fatal("third tenant must be refused at the concurrency cap")
	}
	<-started
	<-started
	release()
	waitIdle(t)
	if !scheduleBackgroundRetrievalDrain("t_c", "tenant-c") {
		t.Fatal("slot should be free after drains finish")
	}
	<-started
	waitIdle(t)
}

func TestBackgroundDrainRejectsEmptyIdentifiers(t *testing.T) {
	t.Setenv(backgroundDrainEnv, "1")
	_, release := swapDrainRunner(t)
	defer release()
	if scheduleBackgroundRetrievalDrain("", "tenant-a") || scheduleBackgroundRetrievalDrain("t_a", "") {
		t.Fatal("empty schema/tenant id must not schedule")
	}
}
