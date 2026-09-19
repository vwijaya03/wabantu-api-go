package ai

import (
	"strings"
	"testing"
	"time"
)

func TestStitchInboundBodies_joinsNonEmptyInOrder(t *testing.T) {
	got := stitchInboundBodies([]string{"halo", "  ", "saya mau beli", "magi", "bisa ga ?"})
	want := "halo\nsaya mau beli\nmagi\nbisa ga ?"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAppendBurstIDs_dedupAndCap(t *testing.T) {
	ids := appendBurstIDs(nil, "a", inboundMaxBurst)
	ids = appendBurstIDs(ids, "b", inboundMaxBurst)
	ids = appendBurstIDs(ids, "a", inboundMaxBurst)
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("dedup failed: %#v", ids)
	}
	cur := make([]string, 0, inboundMaxBurst)
	for i := 0; i < inboundMaxBurst+5; i++ {
		cur = appendBurstIDs(cur, string(rune('A'+i)), inboundMaxBurst)
	}
	if len(cur) != inboundMaxBurst {
		t.Fatalf("cap=%d want %d", len(cur), inboundMaxBurst)
	}
}

func TestRemainingWait_trailingQuiet(t *testing.T) {
	last := time.Date(2026, 9, 19, 16, 18, 20, 253000000, time.UTC)
	now := last
	got := remainingWait(now, last.Add(-5*time.Second), last, inboundQuiet, inboundMaxWait)
	if got != inboundQuiet {
		t.Fatalf("remaining=%s want %s", got, inboundQuiet)
	}
	got = remainingWait(last.Add(inboundQuiet), last.Add(-5*time.Second), last, inboundQuiet, inboundMaxWait)
	if got != 0 {
		t.Fatalf("after quiet remaining=%s want 0", got)
	}
}

func TestRemainingWait_maxWaitCapsStarvation(t *testing.T) {
	burst := time.Date(2026, 9, 19, 16, 18, 15, 0, time.UTC)
	last := burst.Add(14 * time.Second)
	now := burst.Add(14 * time.Second)
	got := remainingWait(now, burst, last, inboundQuiet, inboundMaxWait)
	if got != time.Second {
		t.Fatalf("trailing 3s capped by maxWait 15s: remaining=%s want 1s", got)
	}
	now = burst.Add(inboundMaxWait)
	got = remainingWait(now, burst, last, inboundQuiet, inboundMaxWait)
	if got != 0 {
		t.Fatalf("at maxWait remaining=%s want 0", got)
	}
}

func TestJobSuperseded_newerInboundWins(t *testing.T) {
	halo := time.Date(2026, 9, 19, 16, 18, 15, 79000000, time.UTC)
	beli := time.Date(2026, 9, 19, 16, 18, 16, 689000000, time.UTC)
	if !jobSuperseded(halo, beli) {
		t.Fatal("halo job must bail after beli arrives")
	}
	if jobSuperseded(beli, beli) {
		t.Fatal("same timestamp is not superseded")
	}
}

func TestOmahBurst_quiet3sFlushesAfterBisaBeforePutus(t *testing.T) {
	halo := time.Date(2026, 9, 19, 16, 18, 15, 79000000, time.UTC)
	magi := time.Date(2026, 9, 19, 16, 18, 17, 900000000, time.UTC)
	bisa := time.Date(2026, 9, 19, 16, 18, 20, 253000000, time.UTC)
	putus := time.Date(2026, 9, 19, 16, 18, 25, 74000000, time.UTC)

	// Job magi wakes at magi+3s = 20.900; bisa already there → superseded.
	wakeMagi := magi.Add(inboundQuiet)
	if !jobSuperseded(magi, bisa) {
		t.Fatal("magi job superseded by bisa")
	}
	if remainingWait(wakeMagi, halo, bisa, inboundQuiet, inboundMaxWait) != 0 &&
		remainingWait(wakeMagi, halo, bisa, inboundQuiet, inboundMaxWait) > inboundQuiet {
		t.Fatalf("unexpected wait after magi wake")
	}

	// Job bisa wakes at 23.253; putus is at 25.074 → flush niat beli, not superseded.
	wakeBisa := bisa.Add(inboundQuiet)
	if jobSuperseded(bisa, bisa) {
		t.Fatal("bisa job must flush when it is still last")
	}
	if !wakeBisa.Before(putus) {
		t.Fatalf("3s quiet after bisa (%s) must be before putus (%s)", wakeBisa, putus)
	}
	if remainingWait(wakeBisa, halo, bisa, inboundQuiet, inboundMaxWait) != 0 {
		t.Fatalf("bisa job should be ready to flush at %s", wakeBisa)
	}
}

func TestOmahBurst_quiet2sCutsBeforeBisa(t *testing.T) {
	magi := time.Date(2026, 9, 19, 16, 18, 17, 900000000, time.UTC)
	bisa := time.Date(2026, 9, 19, 16, 18, 20, 253000000, time.UTC)
	wake2s := magi.Add(2 * time.Second)
	if !wake2s.Before(bisa) {
		t.Fatal("document: 2s quiet would flush magi before bisa arrives")
	}
	if remainingWait(wake2s, magi.Add(-2*time.Second), magi, 2*time.Second, inboundMaxWait) != 0 {
		t.Fatal("2s window already closed at magi+2s")
	}
}

func TestIDsAfterWatermark(t *testing.T) {
	ids := []string{"halo", "beli", "magi", "bisa"}
	got := idsAfterWatermark(ids, "beli")
	if strings.Join(got, ",") != "magi,bisa" {
		t.Fatalf("got %#v", got)
	}
	if len(idsAfterWatermark(ids, "")) != 4 {
		t.Fatal("empty watermark keeps all")
	}
	if len(idsAfterWatermark(ids, "missing")) != 4 {
		t.Fatal("unknown watermark keeps all (new burst)")
	}
}

func TestEffectiveInboundText_overrideWins(t *testing.T) {
	got := effectiveInboundText("halo", "halo\nsaya mau beli\nmagi")
	if got != "halo\nsaya mau beli\nmagi" {
		t.Fatalf("got %q", got)
	}
	if effectiveInboundText("halo", "  ") != "halo" {
		t.Fatal("blank override falls back to body")
	}
}

func TestShouldCoalesceInboundType(t *testing.T) {
	if !shouldCoalesceInboundType("text") {
		t.Fatal("text must coalesce")
	}
	if shouldCoalesceInboundType("image") {
		t.Fatal("image stays immediate (payment proof)")
	}
}
