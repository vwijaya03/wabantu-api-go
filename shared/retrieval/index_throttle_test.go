package retrieval

import (
	"context"
	"testing"
)

func TestWithIndexingLaneRunsFn(t *testing.T) {
	liveIndexBudget = NewBudget(2)
	backfillIndexBudget = NewBudget(2)

	called := false
	if err := WithIndexingLane(context.Background(), IndexLaneLive, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected fn to run")
	}

	called = false
	if err := WithIndexingLane(context.Background(), IndexLaneBackfill, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected backfill fn to run")
	}
}
