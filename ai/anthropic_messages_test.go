package ai

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestBuildSalesSystemBlocksCacheControl(t *testing.T) {
	blocks := buildSalesSystemBlocks(SalesReplyRequest{
		System:   "instruksi",
		Business: "Nama bisnis: Toko",
		KB:       "--- RETRIEVED KNOWLEDGE ---",
		Summary:  "ringkasan",
	})
	if len(blocks) != 4 {
		t.Fatalf("blocks len = %d want 4", len(blocks))
	}
	if blocks[1].CacheControl.Type != "ephemeral" {
		t.Fatalf("business block missing cache_control, got %+v", blocks[1].CacheControl)
	}
	if blocks[2].CacheControl.Type != "" {
		t.Fatal("KB block should not be cached")
	}
}

func TestHistoryMessagesToAnthropicRoles(t *testing.T) {
	msgs := HistoryMessagesToAnthropic([]HistoryMessage{
		{Author: "contact", Body: "halo"},
		{Author: "ai", Body: "halo kak"},
		{Author: "contact", Body: "harga berapa?"},
	}, 6)
	if len(msgs) != 3 {
		t.Fatalf("msgs len = %d want 3", len(msgs))
	}
	if msgs[0].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("msg0 role = %s", msgs[0].Role)
	}
	if msgs[1].Role != anthropic.MessageParamRoleAssistant {
		t.Fatalf("msg1 role = %s", msgs[1].Role)
	}
	if msgs[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("msg2 role = %s", msgs[2].Role)
	}
}

func TestHistoryMessagesMergeSameRole(t *testing.T) {
	msgs := HistoryMessagesToAnthropic([]HistoryMessage{
		{Author: "contact", Body: "pertanyaan 1"},
		{Author: "contact", Body: "pertanyaan 2"},
	}, 6)
	if len(msgs) != 1 {
		t.Fatalf("expected merged user turn, got %d messages", len(msgs))
	}
}

func TestBuildSalesMessagesAppendsLatestUser(t *testing.T) {
	msgs := buildSalesMessages(SalesReplyRequest{
		History: []HistoryMessage{
			{Author: "contact", Body: "halo"},
			{Author: "ai", Body: "halo kak"},
		},
		UserText: "harga jeans?",
	})
	if len(msgs) != 3 {
		t.Fatalf("msgs len = %d want 3", len(msgs))
	}
	if msgs[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("last role = %s want user", msgs[2].Role)
	}
}

func TestFinalizeAnthropicReplyUTF8(t *testing.T) {
	emoji := "👋" + string(make([]byte, 1300))
	out := finalizeAnthropicReply(emoji, anthropic.StopReasonEndTurn)
	if len(out) > anthropicReplyMaxBytes {
		t.Fatalf("reply not truncated: len=%d max=%d", len(out), anthropicReplyMaxBytes)
	}
	// Must not split multibyte rune.
	if out != "" && out[len(out)-1] == 0xF0 {
		t.Fatal("truncated mid-rune")
	}
}

func TestFinalizeAnthropicReplyMaxTokensCloser(t *testing.T) {
	long := string(make([]byte, 1500))
	out := finalizeAnthropicReply(long, anthropic.StopReasonMaxTokens)
	if !contains(out, "balasan dipersingkat") {
		t.Fatalf("expected graceful closer, got %q", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
