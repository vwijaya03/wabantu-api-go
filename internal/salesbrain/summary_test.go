package salesbrain

import (
	"strings"
	"testing"
	"time"
)

func TestDecideSummarySessionRule(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		summary  string
		age      time.Duration
		zero     bool
		wantKind SummaryKind
		wantDays int
	}{
		{name: "fresh", summary: "s", age: time.Minute, wantKind: SummaryCurrent},
		{name: "exactly 72h is still current", summary: "s", age: 72 * time.Hour, wantKind: SummaryCurrent},
		{name: "just past 72h is previous", summary: "s", age: 72*time.Hour + time.Second, wantKind: SummaryPrevious, wantDays: 3},
		{name: "10 days", summary: "s", age: 10*24*time.Hour + 5*time.Hour, wantKind: SummaryPrevious, wantDays: 10},
		{name: "exactly 30d is previous", summary: "s", age: 30 * 24 * time.Hour, wantKind: SummaryPrevious, wantDays: 30},
		{name: "past 30d is omitted", summary: "s", age: 30*24*time.Hour + time.Second, wantKind: SummaryNone},
		{name: "zero time is omitted", summary: "s", zero: true, wantKind: SummaryNone},
		{name: "empty summary is omitted", summary: "", age: time.Hour, wantKind: SummaryNone},
		{name: "blank summary is omitted", summary: " \n\t ", age: time.Hour, wantKind: SummaryNone},
		{name: "future timestamp counts as current", summary: "s", age: -time.Minute, wantKind: SummaryCurrent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := now.Add(-c.age)
			if c.zero {
				at = time.Time{}
			}
			got := DecideSummary(c.summary, at, now)
			if got.Kind != c.wantKind || got.AgeDays != c.wantDays {
				t.Fatalf("got kind=%q days=%d, want kind=%q days=%d", got.Kind, got.AgeDays, c.wantKind, c.wantDays)
			}
			if c.wantKind == SummaryNone && got.Text != "" {
				t.Errorf("an omitted summary must carry no text, got %q", got.Text)
			}
		})
	}
	if got := DecideSummary("  pembeli suka salmon \n", now, now); got.Text != "pembeli suka salmon" {
		t.Errorf("text must be trimmed, got %q", got.Text)
	}
}

func TestBuildPromptSummarySection(t *testing.T) {
	in := omahInput()

	_, user := BuildPrompt(in)
	if strings.Contains(user, "RINGKASAN PERCAKAPAN") || strings.Contains(user, "KUNJUNGAN SEBELUMNYA") {
		t.Fatal("no summary: the prompt must not carry the section")
	}

	in.Summary = SummaryContext{Kind: SummaryCurrent, Text: "Pembeli menanyakan salmon beku untuk sashimi."}
	sys, user := BuildPrompt(in)
	if !strings.Contains(user, "[RINGKASAN PERCAKAPAN]\nPembeli menanyakan salmon beku untuk sashimi.") {
		t.Errorf("current summary missing:\n%s", user)
	}
	if strings.Contains(user, "KUNJUNGAN SEBELUMNYA") {
		t.Error("a current summary must not be labelled as a previous visit")
	}
	if strings.Contains(sys, "salmon beku untuk sashimi") {
		t.Error("the summary is dynamic and must stay out of the cacheable system prefix")
	}
	if i, j, k := strings.Index(user, "[STATE]"), strings.Index(user, "[RINGKASAN PERCAKAPAN]"), strings.Index(user, "[RIWAYAT]"); !(i < j && j < k) {
		t.Errorf("the summary belongs between STATE and RIWAYAT (%d %d %d)", i, j, k)
	}

	in.Summary = SummaryContext{Kind: SummaryPrevious, Text: "Pernah tanya tuna.", AgeDays: 9}
	_, user = BuildPrompt(in)
	if !strings.Contains(user, "[KUNJUNGAN SEBELUMNYA (9 hari lalu) — hanya latar, jangan anggap masih berlaku]\nPernah tanya tuna.") {
		t.Errorf("previous-visit summary missing:\n%s", user)
	}
	if strings.Contains(user, "[RINGKASAN PERCAKAPAN]") {
		t.Error("a previous visit must not be labelled as the current conversation")
	}
}

func TestBuildPromptSummaryRulesAreInTheSystemPrompt(t *testing.T) {
	sys, _ := BuildPrompt(omahInput())
	for _, want := range []string{"STATE selalu menang", "Jangan pernah menyebut harga", "hanya latar belakang"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing summary rule %q", want)
		}
	}
}

func TestBuildPromptSummaryIsRedactedAndCapped(t *testing.T) {
	in := omahInput()
	in.Summary = SummaryContext{Kind: SummaryCurrent, Text: "Kirim ke budi@example.com\ntelp 081234567890 total Rp350.000 " + strings.Repeat("kata ", 400)}
	_, user := BuildPrompt(in)
	if strings.Contains(user, "budi@example.com") || strings.Contains(user, "081234567890") {
		t.Errorf("email and phone must be masked:\n%s", user)
	}
	if !strings.Contains(user, "[EMAIL]") || !strings.Contains(user, "[NUM]") {
		t.Errorf("masks expected:\n%s", user)
	}
	block := user[strings.Index(user, "[RINGKASAN PERCAKAPAN]\n")+len("[RINGKASAN PERCAKAPAN]\n"):]
	block = block[:strings.Index(block, "\n")]
	if n := len([]rune(block)); n > MaxSummaryRunes+1 { // +1 for the trailing "…"
		t.Errorf("summary is %d runes, cap is %d", n, MaxSummaryRunes)
	}
	if strings.Contains(block, "\n") {
		t.Error("summary must be flattened to one line")
	}
}

// The summary is background only: it is not an input of BuildFacts, so an amount that appears only
// in a stale summary is not grounded and a reply quoting it is rejected.
func TestFactsExcludeSummaryAmounts(t *testing.T) {
	in := omahInput()
	in.Summary = SummaryContext{Kind: SummaryPrevious, Text: "Dulu tuna Rp199.000 dan transfer ke 9988776655.", AgeDays: 20}
	_, user := BuildPrompt(in)
	if !strings.Contains(user, "Rp199.000") {
		t.Fatal("precondition: the stale amount is in the prompt")
	}

	facts := BuildFacts(FactsInput{
		Catalog:    in.Catalog,
		KB:         in.KB,
		BuyerTexts: []string{in.Message},
		Message:    in.Message,
	})
	if facts.Amounts[199000] {
		t.Fatal("an amount from the summary must not be a fact")
	}
	if facts.Numbers["9988776655"] {
		t.Fatal("a number from the summary must not be a fact")
	}
	vs := Validate(Decision{Reply: "Tuna-nya Rp199.000 ya Kak."}, facts, LangID)
	if len(vs) == 0 || vs[0].Code != VAmount {
		t.Fatalf("a stale summary amount must be rejected, got %+v", vs)
	}
}
