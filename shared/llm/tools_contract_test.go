package llm

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestToolsContractGeminiLive is QA's tools contract test for BE-04 (Gemini function calling).
// It is skipped on purpose: it hits the real Gemini API. QA enables it by removing the
// unconditional skip below and running with GEMINI_API_KEY set (staging key, never prod).
//
// Contract: given the catalog tool declarations, the model must (1) call search_catalog with a
// non-empty "query", (2) accept our functionResponse, and (3) answer with text that uses the
// price from the tool result, not an invented one.
func TestToolsContractGeminiLive(t *testing.T) {
	t.Skip("QA-gated: live Gemini tool-calling contract; enable once a staging GEMINI_API_KEY is provisioned (BE-04)")

	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	g := NewGeminiProvider(key)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var calledWith string
	out, err := g.ChatWithTools(ctx, ToolChatRequest{
		Model:    "gemini-2.5-flash",
		System:   "Kamu asisten toko. Untuk harga WAJIB panggil search_catalog dulu. Jangan mengarang harga.",
		Tools:    catalogTools,
		Messages: []Message{{Role: "user", Text: "Berapa harga kopi gayo 250g?"}},
	}, func(name string, args json.RawMessage) (string, error) {
		calledWith = name + string(args)
		return `{"items":[{"name":"Kopi Arabika Gayo 250g","price":85000}]}`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.UsedTools || !strings.HasPrefix(calledWith, "search_catalog") || !strings.Contains(calledWith, `"query"`) {
		t.Fatalf("tool not called correctly: %q", calledWith)
	}
	if !strings.Contains(strings.ReplaceAll(out.Text, ".", ""), "85000") {
		t.Fatalf("reply does not use tool price: %q", out.Text)
	}
}
