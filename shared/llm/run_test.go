package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func captureLogs(t *testing.T) *[]string {
	var mu sync.Mutex
	logs := []string{}
	old := Logf
	Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	t.Cleanup(func() { Logf = old })
	return &logs
}

func regWith(chain []ChainLink, fn Function, ps ...Provider) *Registry {
	st := NewMemoryStore()
	st.Set(FunctionConfig{Function: fn, Chain: chain})
	r := NewRegistry(NewResolver(st, 0))
	for _, p := range ps {
		r.Register(p)
	}
	return r
}

var demoChain = []ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}, {Provider: "anthropic"}}

func runRecord(r *Registry, fn Function) (used []string, err error) {
	err = r.Run(context.Background(), "t1", fn, func(p Provider, model string) error {
		used = append(used, p.ID()+":"+model)
		_, _, e := p.Complete(context.Background(), model, "", "", 0)
		return e
	})
	return
}

func TestRunFallbackOnRetryable(t *testing.T) {
	g := &MockProvider{Name: "gemini", Err: &APIError{Status: 429, Body: "RESOURCE_EXHAUSTED"}}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	used, err := runRecord(regWith(demoChain, FnPaymentOCR, g, a), FnPaymentOCR)
	if err != nil || strings.Join(used, ",") != "gemini:gemini-2.5-flash,anthropic:" {
		t.Fatalf("used=%v err=%v", used, err)
	}
}

func TestRunNoFallbackOnPermanent(t *testing.T) {
	g := &MockProvider{Name: "gemini", Err: &APIError{Status: 400, Body: "INVALID_ARGUMENT"}}
	a := &MockProvider{Name: "anthropic"}
	_, err := runRecord(regWith(demoChain, FnPaymentOCR, g, a), FnPaymentOCR)
	if err == nil || a.Calls != 0 {
		t.Fatalf("must not fall back on 400: err=%v anthropicCalls=%d", err, a.Calls)
	}
}

type classifierMock struct{ MockProvider }

func (c *classifierMock) IsRetryable(err error) bool {
	return strings.Contains(err.Error(), "overloaded")
}

func TestRunUsesProviderClassifier(t *testing.T) {
	a := &classifierMock{MockProvider{Name: "anthropic", Err: errors.New("overloaded_error")}}
	g := &MockProvider{Name: "gemini", Reply: "ok"}
	chain := []ChainLink{{Provider: "anthropic"}, {Provider: "gemini"}}
	used, err := runRecord(regWith(chain, FnChat, a, g), FnChat)
	if err != nil || len(used) != 2 {
		t.Fatalf("used=%v err=%v", used, err)
	}
}

func TestRunLastErrorReturned(t *testing.T) {
	g := &MockProvider{Name: "gemini", Err: &APIError{Status: 503}}
	a := &MockProvider{Name: "anthropic", Err: &APIError{Status: 529}}
	_, err := runRecord(regWith(demoChain, FnChat, g, a), FnChat)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 529 {
		t.Fatalf("want last error, got %v", err)
	}
}

func TestUnregisteredProviderLoggedAndSkipped(t *testing.T) {
	logs := captureLogs(t)
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	used, err := runRecord(regWith(demoChain, FnPaymentOCR, a), FnPaymentOCR)
	if err != nil || len(used) != 1 || used[0] != "anthropic:" {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[0], `"gemini"`) || !strings.Contains((*logs)[0], "not registered") {
		t.Fatalf("expected log about unregistered gemini, got %v", *logs)
	}
}

func TestGeminiWithoutKeySkipped(t *testing.T) {
	logs := captureLogs(t)
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	used, err := runRecord(regWith(demoChain, FnPaymentOCR, NewGeminiProvider(""), a), FnPaymentOCR)
	if err != nil || len(used) != 1 || used[0] != "anthropic:" {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "unavailable") {
		t.Fatalf("expected unavailable log: %v", *logs)
	}
}

func TestChainOfOnlyUnusableFallsBackToCodeDefault(t *testing.T) {
	logs := captureLogs(t)
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	used, err := runRecord(regWith([]ChainLink{{Provider: "gemini"}}, FnPaymentOCR, a), FnPaymentOCR)
	if err != nil || len(used) != 1 || used[0] != "anthropic:" {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "code default") {
		t.Fatalf("expected code-default log: %v", *logs)
	}
}

func TestRunStopsOnCanceledContext(t *testing.T) {
	g := &MockProvider{Name: "gemini", Err: &APIError{Status: 503}}
	a := &MockProvider{Name: "anthropic"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = regWith(demoChain, FnChat, g, a).Run(ctx, "", FnChat, func(p Provider, m string) error {
		_, _, e := p.Complete(ctx, m, "", "", 0)
		return e
	})
	if a.Calls != 0 {
		t.Fatal("must not fall back after ctx cancel")
	}
}

func TestPaymentOCRCodeDefaultIsAnthropic(t *testing.T) {
	c := CodeDefaults(FnPaymentOCR)
	if c.Chain[0].Provider != "anthropic" || c.Chain[0].Model != "" {
		t.Fatalf("%+v", c)
	}
}

func TestTenantOverrideForPaymentOCR(t *testing.T) {
	st := NewMemoryStore()
	st.Set(FunctionConfig{Function: FnPaymentOCR, Chain: demoChain})
	st.Set(FunctionConfig{Function: FnPaymentOCR, TenantID: "t1", Chain: []ChainLink{{Provider: "anthropic", Model: "claude-sonnet"}}})
	r := NewRegistry(NewResolver(st, 0))
	r.Register(&MockProvider{Name: "gemini"})
	r.Register(&MockProvider{Name: "anthropic"})
	p, m, _ := r.For(context.Background(), "t1", FnPaymentOCR)
	if p.ID() != "anthropic" || m != "claude-sonnet" {
		t.Fatalf("%s %s", p.ID(), m)
	}
	p, _, _ = r.For(context.Background(), "t2", FnPaymentOCR)
	if p.ID() != "gemini" {
		t.Fatalf("t2 should get global demo chain, got %s", p.ID())
	}
}
