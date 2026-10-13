package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// L-2: the tool loop never interprets tool arguments. Gemini functionCall args are handed
// verbatim to the caller's executor (bound to the session tenant), and RunToolChat resolves
// the chain with the caller's tenantID only.
func TestGeminiToolArgsCannotSwitchTenant(t *testing.T) {
	inj := `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search_catalog","args":{"query":"kopi","tenantId":"tenant-b","schema":"tenant_b"}}}]}}]}`
	g, f := newGeminiTest(t, inj, textResp)
	var execCalls int
	var got string
	exec := func(name string, args json.RawMessage) (string, error) {
		execCalls++
		got = string(args)
		return `{"items":[]}`, nil
	}
	if _, err := g.ChatWithTools(context.Background(), ToolChatRequest{Model: "gemini-2.5-flash", Tools: catalogTools,
		Messages: []Message{{Role: "user", Text: "x"}}}, exec); err != nil {
		t.Fatal(err)
	}
	if execCalls != 1 || !strings.Contains(got, "tenant-b") {
		t.Fatalf("args must reach the bound executor verbatim: %d %s", execCalls, got)
	}
	// Nothing tenant-related is lifted from args into the provider request.
	for i, r := range f.reqs {
		b, _ := json.Marshal(r["tools"])
		if strings.Contains(strings.ToLower(string(b)), "tenant") {
			t.Fatalf("req %d declares tenant: %s", i, b)
		}
	}
}

func TestRunToolChatTenantComesFromCaller(t *testing.T) {
	st := NewMemoryStore()
	st.Set(FunctionConfig{Function: FnChat, Chain: chatChain})
	st.Set(FunctionConfig{TenantID: "tenant-b", Function: FnChat, Chain: []ChainLink{{Provider: "anthropic", Model: "claude-haiku-4-5"}}})
	reg := NewRegistry(NewResolver(st, 0))
	g := &MockToolProvider{MockProvider: &MockProvider{Name: "gemini"}, Script: func(r ToolChatRequest, exec ToolExecutor) (ToolChatResponse, error) {
		res, _ := exec("search_catalog", json.RawMessage(`{"query":"kopi","tenantId":"tenant-b","schema":"tenant_b"}`))
		return ToolChatResponse{Text: res}, nil
	}}
	reg.Register(g)
	reg.Register(&MockProvider{Name: "anthropic"})
	out, res, err := RunToolChat(context.Background(), reg, "tenant-a", FnChat, ToolChatRequest{Tools: catalogTools},
		func(string, json.RawMessage) (string, error) { return "tenant-a-catalog", nil }, nil)
	if err != nil || res.Provider != "gemini" || out.Text != "tenant-a-catalog" {
		t.Fatalf("%+v %+v %v", out, res, err)
	}
}
