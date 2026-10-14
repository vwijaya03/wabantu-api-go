package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// slowProvider blocks until ctx is done (simulates a hung upstream).
type slowProvider struct{ MockProvider }

func (s *slowProvider) Complete(ctx context.Context, model, _, _ string, _ int64) (string, Usage, error) {
	s.Calls++
	<-ctx.Done()
	return "", Usage{}, ctx.Err()
}

func TestRunPerLinkTimeoutFallsBack(t *testing.T) {
	slow := &slowProvider{MockProvider{Name: "gemini"}}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	chain := []ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash", TimeoutMS: 30}, {Provider: "anthropic"}}
	start := time.Now()
	res, err := regWith(chain, FnPaymentOCR, slow, a).Run(context.Background(), "", FnPaymentOCR,
		func(ctx context.Context, p Provider, m string) error {
			_, _, e := p.Complete(ctx, m, "", "", 0)
			return e
		})
	if err != nil || res.Provider != "anthropic" || res.Attempts != 2 || slow.Calls != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("timeout not applied, took %v", el)
	}
}

func TestRunTimeoutOnLastLinkReturnsDeadline(t *testing.T) {
	slow := &slowProvider{MockProvider{Name: "gemini"}}
	chain := []ChainLink{{Provider: "gemini", TimeoutMS: 20}}
	_, err := regWith(chain, FnChat, slow).Run(context.Background(), "", FnChat,
		func(ctx context.Context, p Provider, m string) error {
			_, _, e := p.Complete(ctx, m, "", "", 0)
			return e
		})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestRunOpaqueDeadlineNormalised(t *testing.T) {
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	g := &MockProvider{Name: "gemini"}
	chain := []ChainLink{{Provider: "gemini", TimeoutMS: 10}, {Provider: "anthropic"}}
	res, err := regWith(chain, FnChat, g, a).Run(context.Background(), "", FnChat,
		func(ctx context.Context, p Provider, m string) error {
			if p.ID() == "gemini" {
				<-ctx.Done()
				return errors.New("sdk: request aborted") // deadline hidden by SDK
			}
			return nil
		})
	if err != nil || res.Provider != "anthropic" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRunNoTimeoutWhenUnset(t *testing.T) {
	var had bool
	_, _ = regWith([]ChainLink{{Provider: "anthropic"}}, FnChat, &MockProvider{Name: "anthropic"}).Run(context.Background(), "", FnChat,
		func(ctx context.Context, _ Provider, _ string) error { _, had = ctx.Deadline(); return nil })
	if had {
		t.Fatal("no deadline expected when timeout_ms unset")
	}
}

func TestRunCustomRetryClassifier(t *testing.T) {
	g := &MockProvider{Name: "gemini", Err: errors.New("weird transient")}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	r := regWith(demoChain, FnChat, g, a)
	call := func(ctx context.Context, p Provider, m string) error {
		_, _, e := p.Complete(ctx, m, "", "", 0)
		return e
	}
	if _, err := r.Run(context.Background(), "", FnChat, call); err == nil {
		t.Fatal("default classifier should not retry 'weird transient'")
	}
	r.RetryClassifier = func(err error) bool { return IsProviderRetryable(err) || strings.Contains(err.Error(), "transient") }
	res, err := r.Run(context.Background(), "", FnChat, call)
	if err != nil || res.Provider != "anthropic" || res.Attempts != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSkipLoggedOncePerProcess(t *testing.T) {
	logs := captureLogs(t)
	var dbg int
	oldD := Debugf
	Debugf = func(string, ...any) { dbg++ }
	t.Cleanup(func() { Debugf = oldD })
	r := regWith(demoChain, FnPaymentOCR, &MockProvider{Name: "anthropic", Reply: "ok"})
	for i := 0; i < 5; i++ {
		_, _ = runRecord(r, FnPaymentOCR)
	}
	n := 0
	for _, l := range *logs {
		if strings.Contains(l, "not registered") {
			n++
		}
	}
	if n != 1 || dbg != 4 {
		t.Fatalf("want 1 info + 4 debug, got info=%d debug=%d", n, dbg)
	}
}

func TestRunResultReportsModel(t *testing.T) {
	g := &MockProvider{Name: "gemini", Reply: "ok"}
	res, err := regWith(demoChain, FnPaymentOCR, g, &MockProvider{Name: "anthropic"}).Run(context.Background(), "", FnPaymentOCR,
		func(context.Context, Provider, string) error { return nil })
	if err != nil || res.Provider != "gemini" || res.Model != "gemini-2.5-flash" || ModelFamily(res.Model) != "gemini-2.5-flash" {
		t.Fatalf("%+v %v", res, err)
	}
}
