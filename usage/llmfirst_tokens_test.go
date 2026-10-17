package usage

import (
	"encoding/json"
	"math"
	"testing"
)

func llmFirstMetaOf(t *testing.T, tok LLMFirstTokens) map[string]any {
	t.Helper()
	raw, err := llmFirstEventMeta(tok)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLLMFirstEventMetaCarriesModeTenantInboundAndCost(t *testing.T) {
	m := llmFirstMetaOf(t, LLMFirstTokens{
		Mode: "shadow", Pipeline: "llm_first_shadow", TenantID: "t-1", ConversationID: "c-1", InboundMessageID: "m-1",
		Provider: "gemini", Model: "gemini-3.1-flash-lite", Fallback: true, Attempts: 2,
		InputTokens: 2700, OutputTokens: 200, ThoughtsTokens: 50,
	})
	for k, want := range map[string]any{
		"mode": "shadow", "pipeline": "llm_first_shadow", "tenantId": "t-1", "conversationId": "c-1", "inboundMessageId": "m-1",
		"model": "gemini-3.1-flash-lite", "fallback": true, "attempts": 2.0,
		"inputTokens": 2700.0, "outputTokens": 200.0, "thoughtsTokens": 50.0, "costKnown": true,
	} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
	// 2700 in x 0.25/1M + (200+50) out x 1.50/1M (thinking is billed as output)
	want := 2700.0/1e6*0.25 + 250.0/1e6*1.50
	if got, _ := m["estimatedCostUsd"].(float64); math.Abs(got-want) > 1e-9 {
		t.Errorf("estimatedCostUsd = %v, want %v", got, want)
	}
	if _, bad := m["costNote"]; bad {
		t.Error("a priced model carries no cost note")
	}
}

func TestLLMFirstEventMetaMarksAnUnpricedModelAsUnknownNotZero(t *testing.T) {
	m := llmFirstMetaOf(t, LLMFirstTokens{Mode: "on", Pipeline: "llm_first", Model: "gemini-9-imaginary", InputTokens: 1000, OutputTokens: 10})
	if m["costKnown"] != false || m["costNote"] != "harga belum diketahui" {
		t.Fatalf("meta = %v", m)
	}
	if _, has := m["estimatedCostUsd"]; has {
		t.Error("an unknown cost must not be written as a number (0 would read as free)")
	}
}
