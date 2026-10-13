package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestTransportEOFClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"url.Error EOF (stale keep-alive)", &url.Error{Op: "Post", URL: "https://x", Err: io.EOF}, true},
		{"wrapped url.Error EOF", fmt.Errorf("sdk: %w", &url.Error{Op: "Post", URL: "https://x", Err: io.EOF}), true},
		{"TransportError", &TransportError{Provider: "gemini", Err: errors.New("dial tcp: refused")}, true},
		{"TransportError canceled", &TransportError{Provider: "gemini", Err: context.Canceled}, false},
		{"plain EOF", io.EOF, false},
		{"wrapped plain EOF", fmt.Errorf("decode: %w", io.EOF), false},
		{"APIError 400 wrapping nothing", &APIError{Status: 400, Body: "x [body read error: unexpected EOF]"}, false},
	}
	for _, c := range cases {
		if got := IsProviderRetryable(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestUnavailableTextTightened(t *testing.T) {
	for msg, want := range map[string]bool{
		`{"error":{"code":503,"status":"UNAVAILABLE"}}`:      true,
		`{"error": {"status": "UNAVAILABLE"}}`:               true,
		"rpc error: code = Unavailable desc = conn refused":  true,
		"status: UNAVAILABLE":                                true,
		"503 Service Unavailable":                            true,
		"MODEL UNAVAILABLE IN REGION":                        false,
		"feature UNAVAILABLE for this tier":                  false,
		"404 Not Found: model is unavailable in this region": false,
	} {
		if got := IsProviderRetryable(errors.New(msg)); got != want {
			t.Errorf("%q: got %v want %v", msg, got, want)
		}
	}
}

func TestGeminiConnClosedBeforeResponseFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c, _, _ := w.(http.Hijacker).Hijack()
		_ = c.Close()
	}))
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	_, _, err := g.Complete(context.Background(), "", "", "x", 0)
	var te *TransportError
	if !errors.As(err, &te) || !IsProviderRetryable(err) {
		t.Fatalf("want retryable TransportError, got %T %v", err, err)
	}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	res, rerr := regWith(demoChain, FnChat, g, a).Run(context.Background(), "", FnChat,
		func(ctx context.Context, p Provider, m string) error {
			_, _, e := p.Complete(ctx, m, "", "", 0)
			return e
		})
	if rerr != nil || res.Provider != "anthropic" {
		t.Fatalf("res=%+v err=%v", res, rerr)
	}
}

func truncatedServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":`))
		c, _, _ := w.(http.Hijacker).Hijack()
		_ = c.Close()
	}))
}

func TestGeminiTruncated4xxStatusWins(t *testing.T) {
	srv := truncatedServer(400)
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	_, _, err := g.Complete(context.Background(), "", "", "x", 0)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 || IsProviderRetryable(err) || g.IsRetryable(err) {
		t.Fatalf("truncated 400 must be non-retryable APIError, got %T %v", err, err)
	}
	a := &MockProvider{Name: "anthropic", Reply: "ok"}
	_, rerr := regWith(demoChain, FnChat, g, a).Run(context.Background(), "", FnChat,
		func(ctx context.Context, p Provider, m string) error {
			_, _, e := p.Complete(ctx, m, "", "", 0)
			return e
		})
	if rerr == nil || a.CallCount() != 0 {
		t.Fatalf("must not fall back: err=%v anthropicCalls=%d", rerr, a.CallCount())
	}
}

func TestGeminiTruncated5xxStillRetryable(t *testing.T) {
	srv := truncatedServer(503)
	defer srv.Close()
	g := NewGeminiProvider("k")
	g.BaseURL = srv.URL
	_, _, err := g.Complete(context.Background(), "", "", "x", 0)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 503 || !IsProviderRetryable(err) {
		t.Fatalf("got %T %v", err, err)
	}
}
