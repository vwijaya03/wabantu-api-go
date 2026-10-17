package ai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func countIn(msgTexts []string, needle string) int {
	n := 0
	for _, t := range msgTexts {
		n += strings.Count(t, needle)
	}
	return n
}

// The inbound message that triggered the reply is already stored, so it is the last history
// entry; appending UserText again showed the model the same question twice.
func TestHygieneSalesMessagesNeutralNoDuplicateInbound(t *testing.T) {
	req := SalesReplyRequest{
		History: []HistoryMessage{
			{Author: "contact", Body: "Do you have tuna?"},
			{Author: "ai", Body: "Yes, we do."},
			{Author: "contact", Body: "Is it frozen?"},
		},
		UserText: "Is it frozen?",
	}
	var texts []string
	for _, m := range salesMessagesNeutral(req) {
		texts = append(texts, m.Text)
	}
	if n := countIn(texts, "Is it frozen?"); n != 1 {
		t.Fatalf(`"Is it frozen?" appears %d times across the messages, want exactly once: %q`, n, texts)
	}
	if len(texts) != 3 {
		t.Fatalf("want 3 turns (user, assistant, user), got %d: %q", len(texts), texts)
	}
}

func TestHygieneBuildSalesMessagesNoDuplicateInbound(t *testing.T) {
	msgs := buildSalesMessages(SalesReplyRequest{
		History: []HistoryMessage{
			{Author: "contact", Body: "halo"},
			{Author: "ai", Body: "halo kak"},
			{Author: "contact", Body: "harga jeans?"},
		},
		UserText: "  harga jeans?  ",
	})
	if len(msgs) != 3 || msgs[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("want 3 turns ending with the user, got %d", len(msgs))
	}
	var texts []string
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.OfText != nil {
				texts = append(texts, c.OfText.Text)
			}
		}
	}
	if n := countIn(texts, "harga jeans?"); n != 1 {
		t.Fatalf("current message appears %d times, want once: %q", n, texts)
	}
}

// Dropping the current inbound happens before the 6-turn window, so the window keeps six turns
// of real history instead of five plus a duplicate.
func TestHygieneHistoryWindowKeepsSixRealTurns(t *testing.T) {
	var hist []HistoryMessage
	for i := 1; i <= 8; i++ {
		author := "contact"
		if i%2 == 0 {
			author = "ai"
		}
		hist = append(hist, HistoryMessage{Author: author, Body: fmt.Sprintf("m%d", i)})
	}
	hist = append(hist, HistoryMessage{Author: "contact", Body: "current question"})
	msgs := salesMessagesNeutral(SalesReplyRequest{History: hist, UserText: "current question"})
	// m3..m8 (six turns: user, assistant, ...) then the current question merged into the next
	// user turn or appended after an assistant turn.
	var all []string
	for _, m := range msgs {
		all = append(all, m.Text)
	}
	joined := strings.Join(all, "|")
	if strings.Count(joined, "current question") != 1 {
		t.Fatalf("current question must appear once: %q", joined)
	}
	if !strings.Contains(joined, "m3") || strings.Contains(joined, "m2") {
		t.Fatalf("window must be m3..m8 plus the current message: %q", joined)
	}
}

func TestHygieneDropTrailingCurrentInbound(t *testing.T) {
	h := func(author, body string) HistoryMessage { return HistoryMessage{Author: author, Body: body} }
	cases := []struct {
		name    string
		history []HistoryMessage
		user    string
		want    []string // remaining bodies
	}{
		{"last contact message equals the current text", []HistoryMessage{h("ai", "hi"), h("contact", "Is it frozen?")}, "Is it frozen?", []string{"hi"}},
		{"trim on both sides", []HistoryMessage{h("contact", "  Is it frozen?\n")}, "Is it frozen?  ", nil},
		{"last message is not from the contact", []HistoryMessage{h("contact", "Is it frozen?"), h("ai", "Yes")}, "Is it frozen?", []string{"Is it frozen?", "Yes"}},
		{"different text", []HistoryMessage{h("contact", "Is it fresh?")}, "Is it frozen?", []string{"Is it fresh?"}},
		{"older identical message is kept", []HistoryMessage{h("contact", "ok"), h("ai", "sure"), h("contact", "ok")}, "ok", []string{"ok", "sure"}},
		{"empty user text changes nothing", []HistoryMessage{h("contact", "hi")}, "   ", []string{"hi"}},
		{"empty history", nil, "hi", nil},
		// Coalesced burst: the stitched text is the trailing contact messages joined by newlines.
		{"coalesced burst", []HistoryMessage{h("ai", "Hi"), h("contact", "tuna steak?"), h("contact", "frozen?")}, "tuna steak?\nfrozen?", []string{"Hi"}},
		{"burst: earlier flush stays in history", []HistoryMessage{h("contact", "hello"), h("contact", "tuna steak?"), h("contact", "frozen?")}, "tuna steak?\nfrozen?", []string{"hello"}},
		{"burst text that is not a suffix is left alone", []HistoryMessage{h("contact", "a"), h("contact", "b")}, "x\nb", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := dropTrailingCurrentInbound(c.history, c.user)
		var bodies []string
		for _, m := range got {
			bodies = append(bodies, m.Body)
		}
		if strings.Join(bodies, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: remaining = %q, want %q", c.name, bodies, c.want)
		}
	}
}

// The language hint used to REPLACE the catalog-tools hint whenever one was passed, which is
// always (ProcessAutoReply sets one), so the model was never told to call the catalog tools.
func TestHygieneTaskHintMergedWithCatalogToolsHint(t *testing.T) {
	text := salesSystemText(SalesReplyRequest{System: "SYS", TaskHint: "Reply in English."})
	if !strings.Contains(text, "Reply in English.") || !strings.Contains(text, catalogToolsTaskHint) {
		t.Fatalf("both hints must be in the system text:\n%s", text)
	}
	if strings.Index(text, "Reply in English.") > strings.Index(text, catalogToolsTaskHint) {
		t.Fatal("the caller's hint comes first, the catalog-tools hint after it")
	}

	only := salesSystemText(SalesReplyRequest{System: "SYS"})
	if strings.Count(only, catalogToolsTaskHint) != 1 {
		t.Fatalf("no caller hint: the catalog-tools hint appears once, got %d", strings.Count(only, catalogToolsTaskHint))
	}
}

func TestHygieneCatalogToolsHintIsNotDuplicated(t *testing.T) {
	once := withCatalogToolsHint(SalesReplyRequest{TaskHint: "Reply in English."})
	twice := withCatalogToolsHint(once)
	if once.TaskHint != twice.TaskHint {
		t.Fatalf("merging twice must not repeat the hint:\n%q\n%q", once.TaskHint, twice.TaskHint)
	}
	if strings.Count(twice.TaskHint, catalogToolsTaskHint) != 1 {
		t.Fatalf("catalog-tools hint count = %d", strings.Count(twice.TaskHint, catalogToolsTaskHint))
	}
	if got := withCatalogToolsHint(SalesReplyRequest{}).TaskHint; got != catalogToolsTaskHint {
		t.Fatalf("empty hint becomes exactly the catalog-tools hint, got %q", got)
	}
}

func TestKnowledgeContextOneEntryHasNoEmptyMarker(t *testing.T) {
	ctx := BuildKnowledgeContext([]KBEntry{{Question: "Kirim kapan?", Answer: "Pukul 08:00-12:00 dikirim hari itu."}})
	if strings.Contains(ctx, "(belum ada)") {
		t.Fatalf("a KB with one row must not say \"belum ada\":\n%s", ctx)
	}
	if !strings.Contains(ctx, "Kirim kapan?") {
		t.Fatalf("the row must be in the block:\n%s", ctx)
	}
	if !strings.Contains(BuildKnowledgeContext(nil), "(belum ada)") {
		t.Fatal("a truly empty KB keeps the \"belum ada\" marker")
	}
}

// The selector sends up to 30 rows in full, so the block must not cut them at 20.
func TestKnowledgeContextSendsEverythingTheSelectorKeeps(t *testing.T) {
	var entries []KBEntry
	for i := 1; i <= kbPromptFullMax; i++ {
		entries = append(entries, KBEntry{Question: fmt.Sprintf("Pertanyaan-%d", i), Answer: "jawaban"})
	}
	ctx := BuildKnowledgeContext(entries)
	for _, i := range []int{1, 20, 21, kbPromptFullMax} {
		if !strings.Contains(ctx, fmt.Sprintf("Pertanyaan-%d", i)) {
			t.Fatalf("row %d is missing from a %d-row KB block", i, kbPromptFullMax)
		}
	}
}

func kbRows(n int) []dbKBEntry {
	out := make([]dbKBEntry, n)
	for i := range out {
		out[i] = dbKBEntry{ID: fmt.Sprintf("kb-%d", i+1), Question: fmt.Sprintf("Q%d", i+1), Answer: "A", IsActive: true}
	}
	return out
}

func TestHygieneKBForLLMPrompt(t *testing.T) {
	retrieved := kbRows(3)

	if got := kbForLLMPrompt(kbRows(1), nil); len(got) != 1 {
		t.Fatalf("one KB row and nothing retrieved: the row must still be sent, got %d", len(got))
	}
	if got := kbForLLMPrompt(kbRows(30), retrieved); len(got) != 30 {
		t.Fatalf("<=30 rows: send all, got %d", len(got))
	}
	if got := kbForLLMPrompt(kbRows(31), retrieved); len(got) != 3 || got[0].ID != "kb-1" {
		t.Fatalf(">30 rows: use the retrieved rows, got %d", len(got))
	}
	// More rows than we send in full, but retrieval found nothing: never claim there is no FAQ.
	if got := kbForLLMPrompt(kbRows(31), nil); len(got) == 0 || len(got) > kbPromptFallbackHead {
		t.Fatalf(">30 rows and nothing retrieved: want a short non-empty head, got %d", len(got))
	}
	if got := kbForLLMPrompt(nil, retrieved); len(got) != 3 {
		t.Fatalf("no full list (load failed): use the retrieved rows, got %d", len(got))
	}
	if got := kbForLLMPrompt(nil, nil); len(got) != 0 {
		t.Fatalf("no KB at all: empty, got %d", len(got))
	}
}
