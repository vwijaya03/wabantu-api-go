package usage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseAIActivityEntry(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{
		"purpose":      PurposeInboundAutoreply,
		"path":         "llm",
		"model":        "claude-haiku-4-5-20251001",
		"tier":         "haiku",
		"llmUsed":      true,
		"inputTokens":  100,
		"outputTokens": 40,
	})
	e := parseAIActivityEntry("id-1", 140, meta, time.Now())
	if e.Path != "llm" || !e.LLMUsed || e.TotalTokens != 140 {
		t.Fatalf("unexpected entry: %+v", e)
	}
}
