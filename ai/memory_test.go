package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/internal/salesbrain"
)

func TestSummaryTriggerFromTimes(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	// newestFirst builds n timestamps, newest at base, spaced by step; the last gap can differ.
	newestFirst := func(n int, step, firstGap time.Duration) []time.Time {
		var out []time.Time
		cur := base
		for i := 0; i < n; i++ {
			out = append(out, cur)
			gap := step
			if i == 0 {
				gap = firstGap
			}
			cur = cur.Add(-gap)
		}
		return out
	}
	cases := []struct {
		name string
		in   []time.Time
		want bool
	}{
		{"none", nil, false},
		{"few and chatty", newestFirst(10, time.Minute, time.Minute), false},
		{"threshold reached", newestFirst(20, time.Minute, time.Minute), true},
		{"3 messages after a long break is too few", newestFirst(3, time.Minute, 100*time.Hour), false},
		{"4 messages after 72h", newestFirst(4, time.Minute, 72*time.Hour), true},
		{"4 messages after 71h59m", newestFirst(4, time.Minute, 72*time.Hour-time.Minute), false},
		{"10 messages after a week", newestFirst(10, time.Minute, 7*24*time.Hour), true},
		{"old gap inside the batch is not a return", func() []time.Time {
			ts := newestFirst(6, time.Minute, time.Minute)
			ts[4] = ts[3].Add(-100 * time.Hour) // the break is between older messages
			ts[5] = ts[4].Add(-time.Minute)
			return ts
		}(), false},
	}
	for _, c := range cases {
		if got := summaryTriggerFromTimes(c.in); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSummaryTriggerUsesTheSessionWindowConstant(t *testing.T) {
	if salesbrain.SummarySessionWindow != 72*time.Hour {
		t.Fatalf("session window is %v, the spec and docs say 72h", salesbrain.SummarySessionWindow)
	}
}

func TestBuildSummarizerPromptFirstSummary(t *testing.T) {
	p := buildSummarizerPrompt("", "", "Pelanggan: halo\nAI: hai")
	if strings.Contains(p, "Ringkasan sebelumnya") {
		t.Error("no previous summary: the section must be absent")
	}
	if !strings.Contains(p, "Percakapan:\nPelanggan: halo\nAI: hai") {
		t.Errorf("messages missing:\n%s", p)
	}
}

func TestBuildSummarizerPromptIsRolling(t *testing.T) {
	p := buildSummarizerPrompt("Konteks bisnis tenant\n\n", "  Pembeli suka salmon.  ", "Pelanggan: ada tuna?")
	for _, want := range []string{
		"Konteks bisnis tenant",
		"Ringkasan sebelumnya:\nPembeli suka salmon.\n\nPesan baru:\nPelanggan: ada tuna?",
		"Gabungkan ringkasan sebelumnya",
		"Maksimal 5 kalimat",
		"preferensi pembeli",
		"Jangan anggap status pesanan, harga, alamat, atau pembayaran sebagai fakta final",
		"Buang hal yang sudah selesai",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Index(p, "Ringkasan sebelumnya") > strings.Index(p, "Pesan baru") {
		t.Error("the previous summary comes before the new messages")
	}
}

func TestLFBuildPlanInputAppliesTheSummarySessionRule(t *testing.T) {
	in := lfShellInputs(t)
	in.Summary = "Pembeli suka salmon."

	in.SummaryAt = in.Now.Add(-time.Hour)
	if got := lfBuildPlanInput(in).Summary; got.Kind != salesbrain.SummaryCurrent || got.Text != "Pembeli suka salmon." {
		t.Errorf("recent summary = %+v", got)
	}
	in.SummaryAt = in.Now.Add(-5 * 24 * time.Hour)
	if got := lfBuildPlanInput(in).Summary; got.Kind != salesbrain.SummaryPrevious || got.AgeDays != 5 {
		t.Errorf("5-day-old summary = %+v", got)
	}
	in.SummaryAt = in.Now.Add(-31 * 24 * time.Hour)
	if got := lfBuildPlanInput(in).Summary; got.Kind != salesbrain.SummaryNone {
		t.Errorf("31-day-old summary must be omitted, got %+v", got)
	}
	in.SummaryAt = time.Time{}
	if got := lfBuildPlanInput(in).Summary; got.Kind != salesbrain.SummaryNone {
		t.Errorf("no summary row must be omitted, got %+v", got)
	}
}

// The summary reaches the model as background, but a price that only the summary mentions is
// rejected: planTurn validates against catalog/KB/STATE facts, never the summary.
func TestPlanTurnSummaryIsBackgroundNotAFactSource(t *testing.T) {
	in := lfPlanBase("How much is the tuna steak?")
	in.Summary = salesbrain.SummaryContext{Kind: salesbrain.SummaryPrevious, Text: "Dulu tuna Rp199.000.", AgeDays: 12}
	fake := lfTexts(
		`{"intent":"question","reply":"The tuna steak is Rp199.000 per portion."}`,
		`{"intent":"question","reply":"The tuna steak is Rp350.000 per portion."}`,
	)
	plan := planTurn(context.Background(), in, fake.chat)

	if !strings.Contains(fake.userOf(0), "[KUNJUNGAN SEBELUMNYA (12 hari lalu)") {
		t.Fatalf("the summary must reach the model:\n%s", fake.userOf(0))
	}
	if plan.Fallback || plan.Attempts != 2 || !strings.Contains(plan.Reply, "Rp350.000") || strings.Contains(plan.Reply, "Rp199.000") {
		t.Fatalf("the stale summary price must be rejected and corrected: attempts=%d reply=%q fallback=%v", plan.Attempts, plan.Reply, plan.Fallback)
	}
}
