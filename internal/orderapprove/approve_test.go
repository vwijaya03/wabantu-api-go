package orderapprove

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		s    State
		want Decision
		err  *Error
	}{
		{State{Status: "completed", PaymentStatus: "verified"}, AlreadyDone, nil},
		{State{Status: "completed", PaymentStatus: "unpaid"}, AlreadyDone, nil},
		{State{Status: "cancelled", PaymentStatus: "proof_submitted"}, Reject, ErrCancelled},
		{State{Status: "confirmed", PaymentStatus: "unpaid"}, Reject, ErrNoProof},
		{State{Status: "confirmed", PaymentStatus: "proof_submitted", ProofMessageID: "m"}, Proceed, nil},
		{State{Status: "confirmed", PaymentStatus: "rejected", ProofMessageID: "m"}, Proceed, nil},
		{State{Status: "confirmed", PaymentStatus: "rejected"}, Reject, ErrNoProof},
		{State{Status: "processing", PaymentStatus: "verified"}, Proceed, nil},
		{State{Status: "confirmed", PaymentStatus: "proof_submitted", ProofBlocked: true}, Reject, ErrBlocked},
		{State{Status: "draft", PaymentStatus: "proof_submitted"}, Proceed, nil},
		{State{Status: "draft", PaymentStatus: "rejected", HasProofMeta: true}, Reject, ErrDraft},
	}
	for i, c := range cases {
		d, e := Decide(c.s)
		if d != c.want || e != c.err {
			t.Errorf("%d %+v: got %v %v", i, c.s, d, e)
		}
	}
	if ErrNoProof.Kind != "unprocessable" || ErrCancelled.Kind != "conflict" {
		t.Fatal("kinds")
	}
}

type fakeStore struct {
	state              State
	found              bool
	failOn             string
	calls              []string
	incomeRows         int
	committedIncome    int
	committedCompleted bool
	pendingCompleted   bool
	staleRow           bool
}

func (f *fakeStore) step(n string) error {
	f.calls = append(f.calls, n)
	if f.failOn == n {
		return errors.New(n + " failed")
	}
	return nil
}
func (f *fakeStore) LockOrder(context.Context) (State, bool, error) {
	return f.state, f.found, f.step("lock")
}
func (f *fakeStore) CompleteVerified(context.Context) error {
	if f.staleRow {
		f.calls = append(f.calls, "complete-stale")
		return ErrStaleRow
	}
	f.pendingCompleted = true
	return f.step("complete")
}
func (f *fakeStore) RecordIncome(context.Context) error {
	f.incomeRows++
	return f.step("income")
}
func (f *fakeStore) NotifyBuyer(context.Context) error { return f.step("notify") }
func (f *fakeStore) Commit() error {
	if err := f.step("commit"); err != nil {
		return err
	}
	f.committedIncome += f.incomeRows
	f.committedCompleted = f.committedCompleted || f.pendingCompleted
	f.state.Status, f.state.PaymentStatus = "completed", "verified"
	f.incomeRows, f.pendingCompleted = 0, false
	return nil
}
func (f *fakeStore) Rollback() error {
	f.calls = append(f.calls, "rollback")
	f.incomeRows, f.pendingCompleted = 0, false
	return nil
}

func TestRunHappyPathAndIdempotent(t *testing.T) {
	f := &fakeStore{found: true, state: State{Status: "confirmed", PaymentStatus: "proof_submitted", ProofMessageID: "m"}}
	r, err := Run(context.Background(), f)
	if err != nil || r.Decision != Proceed || !f.committedCompleted || f.committedIncome != 1 {
		t.Fatalf("%+v %v %+v", r, err, f)
	}
	if !reflect.DeepEqual(f.calls, []string{"lock", "complete", "income", "notify", "commit"}) {
		t.Fatal(f.calls)
	}
	// Second call (retry / double click): no writes, still one income row.
	f.calls = nil
	r, err = Run(context.Background(), f)
	if err != nil || r.Decision != AlreadyDone || f.committedIncome != 1 {
		t.Fatalf("%+v %v income=%d", r, err, f.committedIncome)
	}
	if !reflect.DeepEqual(f.calls, []string{"lock", "rollback"}) {
		t.Fatal(f.calls)
	}
}

func TestRunRollsBackOnFailure(t *testing.T) {
	for _, step := range []string{"complete", "income", "notify", "commit"} {
		f := &fakeStore{found: true, failOn: step, state: State{Status: "confirmed", PaymentStatus: "proof_submitted"}}
		if _, err := Run(context.Background(), f); err == nil {
			t.Fatalf("%s: want error", step)
		}
		if f.committedCompleted || f.committedIncome != 0 || f.calls[len(f.calls)-1] != "rollback" {
			t.Fatalf("%s: partial commit %+v", step, f)
		}
	}
}

func TestRunRejectsAndNotFound(t *testing.T) {
	f := &fakeStore{found: true, state: State{Status: "confirmed", PaymentStatus: "unpaid"}}
	if _, err := Run(context.Background(), f); err != ErrNoProof || f.committedIncome != 0 {
		t.Fatal(err)
	}
	// Another tenant's order is invisible in this tenant's schema → not found.
	if _, err := Run(context.Background(), &fakeStore{found: false}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

// Guarded UPDATE affected 0 rows → ErrStale (409 "pesanan berubah, muat ulang"), rolled back,
// no income, no buyer message.
func TestRunStaleGuardIs409AndRollsBack(t *testing.T) {
	f := &fakeStore{state: State{Status: "draft", PaymentStatus: "proof_submitted", ProofMessageID: "m1"}, found: true, staleRow: true}
	_, err := Run(context.Background(), f)
	if err != ErrStale {
		t.Fatalf("err = %v, want ErrStale", err)
	}
	if f.committedCompleted || f.committedIncome != 0 {
		t.Fatal("stale approve must not commit")
	}
	want := []string{"lock", "complete-stale", "rollback"}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v want %v", f.calls, want)
	}
	if ErrStale.Message != "pesanan berubah, muat ulang" || ErrStale.Kind != "conflict" {
		t.Fatal("ErrStale contract")
	}
}
