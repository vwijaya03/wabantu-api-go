package ai

import (
	"encoding/json"
	"testing"
	"time"

	"encore.app/wabantu/usage"
)

func TestKeepLiveAnomalyEntry(t *testing.T) {
	exists := map[string]struct{}{"in-live": {}}
	now := time.Now()
	cases := []struct {
		name string
		e    TriageAnomalyEntry
		want bool
	}{
		{
			name: "inbound_autoreply_live_message",
			e:    TriageAnomalyEntry{Purpose: usage.PurposeInboundAutoreply, InboundID: "in-live", CreatedAt: now},
			want: true,
		},
		{
			name: "orphan_inbound_after_message_delete",
			e:    TriageAnomalyEntry{Purpose: usage.PurposeInboundAutoreply, InboundID: "deleted", CreatedAt: now},
			want: false,
		},
		{
			name: "llm_judge_is_not_mencurigakan",
			e:    TriageAnomalyEntry{Purpose: usage.PurposeTriageLLMJudge, InboundID: "in-live", CreatedAt: now},
			want: false,
		},
		{
			name: "empty_inbound",
			e:    TriageAnomalyEntry{Purpose: usage.PurposeInboundAutoreply, CreatedAt: now},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepLiveAnomalyEntry(tc.e, exists); got != tc.want {
				t.Fatalf("keepLiveAnomalyEntry = %v want %v", got, tc.want)
			}
		})
	}
}

func TestParseAnomalyMetadataPurpose(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"path":           "order_flow",
		"purpose":        usage.PurposeTriageLLMJudge,
		"inboundId":      "in-1",
		"conversationId": "c-1",
	})
	e := parseAnomalyMetadata(raw, time.Now())
	if e.Purpose != usage.PurposeTriageLLMJudge {
		t.Fatalf("purpose = %q", e.Purpose)
	}
	if e.InboundID != "in-1" {
		t.Fatalf("inbound = %q", e.InboundID)
	}
}
