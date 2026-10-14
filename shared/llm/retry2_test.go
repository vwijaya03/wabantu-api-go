package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "Client.Timeout exceeded while awaiting headers" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestIsProviderRetryableTransport(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unexpected EOF wrapped", fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), true},
		{"deadline", fmt.Errorf("gemini: %w", context.DeadlineExceeded), true},
		{"net timeout", &net.OpError{Op: "read", Err: timeoutErr{}}, true},
		{"canceled", fmt.Errorf("x: %w", context.Canceled), false},
		{"429 text", errors.New("POST /v1/messages: 429 Too Many Requests"), true},
		{"grpc unavailable", errors.New("rpc error: code = Unavailable desc = UNAVAILABLE"), true},
		{"503 text", errors.New("503 Service Unavailable"), true},
		{"404 unavailable text", errors.New("404 Not Found: model gemini-x is unavailable in this region"), false},
		{"typed 404 with UNAVAILABLE body", &APIError{Status: 404, Body: "UNAVAILABLE"}, false},
		{"plain EOF", io.EOF, false},
	}
	for _, c := range cases {
		if got := IsProviderRetryable(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if !NewGeminiProvider("k").IsRetryable(fmt.Errorf("x: %w", io.ErrUnexpectedEOF)) {
		t.Error("GeminiProvider.IsRetryable should accept unexpected EOF")
	}
}

func TestRunDeadlineWithoutLinkTimeoutFallsBack(t *testing.T) {
	g := &MockProvider{Name: "gemini", Errs: []error{fmt.Errorf("gemini: %w", context.DeadlineExceeded)}}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	res, err := regWith(demoChain, FnChat, g, a).Run(context.Background(), "", FnChat,
		func(ctx context.Context, p Provider, m string) error {
			_, _, e := p.Complete(ctx, m, "", "", 0)
			return e
		})
	if err != nil || res.Provider != "anthropic" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRunParentDeadlineDoesNotFallBack(t *testing.T) {
	g := &MockProvider{Name: "gemini"}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	_, err := regWith(demoChain, FnChat, g, a).Run(ctx, "", FnChat,
		func(ctx context.Context, p Provider, m string) error {
			_, _, e := p.Complete(ctx, m, "", "", 0)
			return e
		})
	if !errors.Is(err, context.DeadlineExceeded) || a.CallCount() != 0 {
		t.Fatalf("parent deadline must stop chain: err=%v anthropicCalls=%d", err, a.CallCount())
	}
}

func TestGeminiTruncatedBodyIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"candidates":[`))
		if hj, ok := w.(http.Hijacker); ok {
			c, _, _ := hj.Hijack()
			_ = c.Close()
		}
	}))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	_, _, err := g.Complete(context.Background(), "", "", "x", 0)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !g.IsRetryable(err) {
		t.Fatalf("want wrapped unexpected EOF, got %v", err)
	}
}

func TestGeminiClientTimeoutIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	g.HTTP = &http.Client{Timeout: 30 * time.Millisecond}
	_, _, err := g.Complete(context.Background(), "", "", "x", 0)
	if err == nil || !IsProviderRetryable(err) {
		t.Fatalf("client timeout should be retryable: %v", err)
	}
}

func TestMockConcurrentAndCtx(t *testing.T) {
	m := &MockProvider{Name: "m", Errs: make([]error, 50)}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _ = m.Complete(context.Background(), "", "", "", 0) }()
	}
	wg.Wait()
	if m.CallCount() != 100 {
		t.Fatalf("calls=%d", m.CallCount())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := m.Complete(ctx, "", "", "", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("mock must respect ctx, got %v", err)
	}
}
