package orderapprove

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// fakeStock models the DB: an atomic marker and a NON-idempotent deduction counter (worst
// case), so only the marker can prevent double deduction.
type fakeStock struct {
	mu        sync.Mutex
	marker    string
	seq       int
	deducted  int
	failNext  int // number of upcoming ApplyStock calls that fail
	gate      chan struct{}
	applyCall int
}

func (f *fakeStock) ClaimStock(context.Context) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.marker != "" {
		return "", false, nil
	}
	f.seq++
	f.marker = fmt.Sprintf("t%d", f.seq)
	return f.marker, true, nil
}

func (f *fakeStock) ApplyStock(context.Context) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCall++
	if f.failNext > 0 {
		f.failNext--
		return errors.New("stock tx failed")
	}
	f.deducted++
	return nil
}

func (f *fakeStock) ReleaseStock(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.marker == token {
		f.marker = ""
	}
	return nil
}

func TestApplyStockOnceRepeatedApprove(t *testing.T) {
	f := &fakeStock{}
	for i := 0; i < 3; i++ {
		applied, err := ApplyStockOnce(context.Background(), f)
		if err != nil || applied != (i == 0) {
			t.Fatalf("call %d: applied=%v err=%v", i, applied, err)
		}
	}
	if f.deducted != 1 || f.applyCall != 1 {
		t.Fatalf("deducted=%d calls=%d", f.deducted, f.applyCall)
	}
}

func TestApplyStockOnceRetryAfterFailure(t *testing.T) {
	f := &fakeStock{failNext: 1}
	if _, err := ApplyStockOnce(context.Background(), f); err == nil {
		t.Fatal("first call must fail")
	}
	if f.marker != "" || f.deducted != 0 {
		t.Fatalf("claim not released: %+v", f)
	}
	applied, err := ApplyStockOnce(context.Background(), f)
	if err != nil || !applied {
		t.Fatal(applied, err)
	}
	if again, _ := ApplyStockOnce(context.Background(), f); again || f.deducted != 1 {
		t.Fatalf("deducted=%d", f.deducted)
	}
}

func TestApplyStockOnceConcurrent(t *testing.T) {
	f := &fakeStock{gate: make(chan struct{})}
	const n = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	appliedCount := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := ApplyStockOnce(context.Background(), f)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				appliedCount++
				mu.Unlock()
			}
		}()
	}
	close(f.gate) // let the winner's deduction proceed while others race the claim
	wg.Wait()
	if f.deducted != 1 || appliedCount != 1 {
		t.Fatalf("deducted=%d applied=%d", f.deducted, appliedCount)
	}
}

func TestReleaseOnlyOwnClaim(t *testing.T) {
	f := &fakeStock{}
	tok, _, _ := f.ClaimStock(context.Background())
	f.marker = "restamped-by-stock-tx" // stock tx committed then a later step failed
	_ = f.ReleaseStock(context.Background(), tok)
	if f.marker == "" {
		t.Fatal("must not clear a marker it does not own")
	}
}
