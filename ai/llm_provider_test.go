package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestAnthropicProviderChatViaMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"oke kak"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`))
	}))
	defer srv.Close()
	c := NewAnthropicClient("fake", DefaultAnthropicConfig())
	c.client = anthropic.NewClient(option.WithAPIKey("fake"), option.WithBaseURL(srv.URL))
	p := &AnthropicProvider{Client: c}
	text, u, err := p.Complete(context.Background(), "", "sys", "hai", 0)
	if err != nil || text != "oke kak" || u.Provider != "anthropic" || u.InputTokens != 5 {
		t.Fatalf("%q %+v %v", text, u, err)
	}
}
