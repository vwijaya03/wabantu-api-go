package adminauth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type errStore struct{}

func (errStore) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, errors.New("redis down")
}
func (errStore) SetLock(context.Context, string, time.Duration) error {
	return errors.New("redis down")
}
func (errStore) Locked(context.Context, string) (bool, error) { return false, errors.New("redis down") }
func (errStore) Del(context.Context, ...string) error         { return errors.New("redis down") }

// L-1: bootstrap is fail-closed → 503 when Redis is missing or erroring.
func TestBootstrapGuardFailClosed(t *testing.T) {
	ctx := context.Background()
	for name, s := range map[string]CounterStore{"nil": nil, "error": errStore{}} {
		g := Guard{S: s, P: BootstrapPolicy()}
		ok, _, err := g.Check(ctx, "1.2.3.4", "bootstrap")
		if ok || err == nil {
			t.Fatalf("%s: ok=%v err=%v", name, ok, err)
		}
		if st := g.CheckStatus(ok, err); st != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d", name, st)
		}
	}
	if err := (Guard{P: BootstrapPolicy()}).Fail(ctx, "ip", "a"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatal(err)
	}
}

func TestBootstrapGuardRateLimitedIs429(t *testing.T) {
	ctx := context.Background()
	g := Guard{S: newMem(time.Now), P: BootstrapPolicy()}
	for i := 0; i < 5; i++ {
		ok, _, err := g.Check(ctx, "1.2.3.4", "bootstrap")
		if st := g.CheckStatus(ok, err); !ok || st != 0 {
			t.Fatalf("attempt %d blocked: %d", i, st)
		}
	}
	ok, reason, err := g.Check(ctx, "1.2.3.4", "bootstrap")
	if ok || err != nil || g.CheckStatus(ok, err) != http.StatusTooManyRequests || reason != ReasonRateIP {
		t.Fatalf("ok=%v reason=%s err=%v", ok, reason, err)
	}
}

// Client portal keeps its fail-open behaviour; admin stays fail-closed (unchanged).
func TestGuardFailModesUnchanged(t *testing.T) {
	ctx := context.Background()
	c := Guard{S: errStore{}, P: ClientPolicy()}
	if ok, _, err := c.Check(ctx, "ip", "a"); !ok || err == nil || c.CheckStatus(ok, err) != 0 {
		t.Fatal("client portal should fail open")
	}
	a := Guard{S: errStore{}, P: AdminPolicy()}
	if ok, _, err := a.Check(ctx, "ip", "a"); ok || a.CheckStatus(ok, err) != http.StatusServiceUnavailable {
		t.Fatal("admin portal should fail closed")
	}
	if BootstrapPolicy().Portal != PortalBootstrap || !BootstrapPolicy().FailClosed {
		t.Fatal("bootstrap policy")
	}
}
