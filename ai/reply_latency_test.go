package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMetaWithLatency(t *testing.T) {
	in := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	got := metaWithLatency(AiReplyMeta{Path: PathLLMFirst}, in, 1500, in.Add(3400*time.Millisecond))
	if got.LatencyMs != 3400 || got.CoalesceWaitMs != 1500 {
		t.Fatalf("latency=%d coalesce=%d", got.LatencyMs, got.CoalesceWaitMs)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"latencyMs":3400`) || !strings.Contains(string(b), `"coalesceWaitMs":1500`) {
		t.Fatalf("metadata json: %s", b)
	}
}

func TestMetaWithLatencyUnknownInbound(t *testing.T) {
	got := metaWithLatency(AiReplyMeta{}, time.Time{}, 0, time.Now())
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "latencyMs") || strings.Contains(string(b), "coalesceWaitMs") {
		t.Fatalf("unset values must be omitted: %s", b)
	}
}
