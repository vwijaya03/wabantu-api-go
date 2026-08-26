package tenant

import (
	"sync"
	"testing"
)

func TestLazyMigrateOnceForReturnsSameInstance(t *testing.T) {
	a := lazyMigrateOnceFor("t_demo_a")
	b := lazyMigrateOnceFor("t_demo_a")
	if a != b {
		t.Fatal("expected same sync.Once for identical schema")
	}
	c := lazyMigrateOnceFor("t_demo_b")
	if a == c {
		t.Fatal("expected different sync.Once per schema")
	}
}

func TestResetLazyMigrateOnceAllowsNewInstance(t *testing.T) {
	schema := "t_demo_reset"
	first := lazyMigrateOnceFor(schema)
	resetLazyMigrateOnce(schema)
	second := lazyMigrateOnceFor(schema)
	if first == second {
		t.Fatal("reset should allocate a new sync.Once")
	}
}

func TestLazyMigrateOnceRunsOnce(t *testing.T) {
	schema := "t_demo_run_once"
	var calls int
	var mu sync.Mutex
	lazyMigrateOnceFor(schema).Do(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	lazyMigrateOnceFor(schema).Do(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	if calls != 1 {
		t.Fatalf("sync.Once Do ran %d times, want 1", calls)
	}
	resetLazyMigrateOnce(schema)
}
