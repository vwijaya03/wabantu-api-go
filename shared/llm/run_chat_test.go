package llm

import (
	"context"
	"errors"
	"testing"
)

// recordingChat is a Provider whose Chat records the request it received, so tests can assert
// what RunChat forwards to each chain link. The other Provider methods come from MockProvider.
type recordingChat struct {
	MockProvider
	got   ChatRequest
	calls int
	err   error
	text  string
}

func (p *recordingChat) Chat(ctx context.Context, r ChatRequest) (ChatResponse, error) {
	p.got = r
	p.calls++
	if p.err != nil {
		return ChatResponse{}, p.err
	}
	return ChatResponse{Text: p.text, Usage: Usage{Provider: p.ID(), Model: r.Model}}, nil
}

// Chain links must carry allowlisted model ids (Registry.usable runs ValidateModel and skips
// the rest), so these tests use chatChain's ids instead of an arbitrary label.

func TestRunChatUsesLinkModelAndForwardsJSONTemperature(t *testing.T) {
	a := &recordingChat{MockProvider: MockProvider{Name: "gemini"}, text: `{"reply":"hi"}`}
	reg := regWith([]ChainLink{{Provider: "gemini", Model: "gemini-2.5-flash"}}, FnChat, a)
	temp := 0.3
	req := ChatRequest{
		System:    "sys",
		Messages:  []Message{{Role: "user", Text: "hello"}},
		MaxTokens: 512, JSON: true, Temperature: &temp,
	}

	out, res, err := RunChat(context.Background(), reg, "t1", FnChat, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != `{"reply":"hi"}` {
		t.Errorf("text = %q", out.Text)
	}
	if res.Provider != "gemini" || res.Model != "gemini-2.5-flash" || res.Attempts != 1 {
		t.Errorf("res = %+v", res)
	}
	if a.got.Model != "gemini-2.5-flash" {
		t.Errorf("link model not applied: Model = %q", a.got.Model)
	}
	if !a.got.JSON {
		t.Error("JSON flag not forwarded")
	}
	if a.got.Temperature != &temp {
		t.Errorf("Temperature pointer not forwarded: got %p want %p", a.got.Temperature, &temp)
	} else if *a.got.Temperature != 0.3 {
		t.Errorf("Temperature = %v", *a.got.Temperature)
	}
	if a.got.System != "sys" || a.got.MaxTokens != 512 || len(a.got.Messages) != 1 || a.got.Messages[0].Text != "hello" {
		t.Errorf("rest of request not forwarded: %+v", a.got)
	}
	if out.Usage.Provider != "gemini" || out.Usage.Model != "gemini-2.5-flash" {
		t.Errorf("usage = %+v", out.Usage)
	}
}

func TestRunChatFallsBackOnRetryableError(t *testing.T) {
	failing := &recordingChat{MockProvider: MockProvider{Name: "gemini"}, err: &APIError{Status: 429, Body: "RESOURCE_EXHAUSTED"}}
	backup := &recordingChat{MockProvider: MockProvider{Name: "anthropic"}, text: "dari anthropic"}
	reg := regWith(chatChain, FnChat, failing, backup)
	temp := 0.7

	out, res, err := RunChat(context.Background(), reg, "t1", FnChat, ChatRequest{
		Messages: []Message{{Role: "user", Text: "hai"}}, JSON: true, Temperature: &temp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "dari anthropic" {
		t.Errorf("text = %q", out.Text)
	}
	if res.Attempts != 2 || res.Provider != "anthropic" || res.Model != "claude-haiku-4-5" {
		t.Errorf("res = %+v", res)
	}
	// Each link must receive its own model, and the request options must reach the fallback too.
	if failing.calls != 1 || failing.got.Model != "gemini-2.5-flash" {
		t.Errorf("first link: calls=%d model=%q", failing.calls, failing.got.Model)
	}
	if backup.calls != 1 || backup.got.Model != "claude-haiku-4-5" {
		t.Errorf("fallback link: calls=%d model=%q", backup.calls, backup.got.Model)
	}
	if !backup.got.JSON || backup.got.Temperature != &temp {
		t.Errorf("fallback request lost JSON/Temperature: %+v", backup.got)
	}
}

func TestRunChatNoFallbackOnPermanentError(t *testing.T) {
	failing := &recordingChat{MockProvider: MockProvider{Name: "gemini"}, err: &APIError{Status: 400, Body: "INVALID_ARGUMENT"}}
	backup := &recordingChat{MockProvider: MockProvider{Name: "anthropic"}, text: "tidak boleh dipanggil"}
	reg := regWith(chatChain, FnChat, failing, backup)

	_, res, err := RunChat(context.Background(), reg, "t1", FnChat, ChatRequest{Messages: []Message{{Role: "user", Text: "x"}}})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("want the 400 APIError back, got %v", err)
	}
	if res.Attempts != 1 || backup.calls != 0 {
		t.Errorf("must not fall back on 400: attempts=%d backupCalls=%d", res.Attempts, backup.calls)
	}
}
