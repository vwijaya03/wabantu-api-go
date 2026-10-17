package usage

import (
	"math"
	"testing"
	"time"
)

func lfRow(ts string, meta string) LLMFirstUsageRow {
	tm, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return LLMFirstUsageRow{CreatedAt: tm, Metadata: []byte(meta)}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAggregateLLMFirstUsageTotalsAndUnknownCost(t *testing.T) {
	rows := []LLMFirstUsageRow{
		lfRow("2026-10-10T03:00:00Z", `{"mode":"on","model":"m-a","inputTokens":1000,"outputTokens":100,"thoughtsTokens":10,"costKnown":true,"estimatedCostUsd":0.002}`),
		lfRow("2026-10-10T04:00:00Z", `{"mode":"shadow","model":"m-a","fallback":true,"inputTokens":3000,"outputTokens":200,"thoughtsTokens":30,"costKnown":true,"estimatedCostUsd":0.004}`),
		lfRow("2026-10-10T05:00:00Z", `{"mode":"on","model":"m-x","inputTokens":2000,"outputTokens":300,"thoughtsTokens":0,"costKnown":false,"costNote":"harga belum diketahui"}`),
	}
	r := AggregateLLMFirstUsage(rows, 16500)
	tt := r.Totals
	if tt.Messages != 3 || tt.LiveMessages != 2 || tt.ShadowMessages != 1 || tt.Fallbacks != 1 {
		t.Fatalf("counts = %+v", tt)
	}
	if tt.InputTokens != 6000 || tt.OutputTokens != 600 || tt.ThoughtsTokens != 40 {
		t.Fatalf("tokens = %+v", tt)
	}
	if !near(tt.EstimatedCostUSD, 0.006) || tt.UnknownCostMessages != 1 {
		t.Fatalf("cost = %+v", tt)
	}
	if !near(r.AvgPerMessage.InputTokens, 2000) || !near(r.AvgPerMessage.OutputTokens, 200) || !near(r.AvgPerMessage.ThoughtsTokens, 40.0/3) {
		t.Fatalf("avg = %+v", r.AvgPerMessage)
	}
	// cost average only over the 2 known-cost messages
	if !near(r.AvgPerMessage.CostUSD, 0.003) || !near(r.AvgPerMessage.CostIDR, 0.003*16500) {
		t.Fatalf("avg cost = %+v", r.AvgPerMessage)
	}
	if r.IDRPerUSD != 16500 {
		t.Fatalf("idr = %v", r.IDRPerUSD)
	}
}

func TestAggregateLLMFirstUsageByDayUsesJakartaDateAndSplitsMode(t *testing.T) {
	rows := []LLMFirstUsageRow{
		// 2026-10-10 17:30 UTC = 2026-10-11 00:30 WIB
		lfRow("2026-10-10T17:30:00Z", `{"mode":"on","model":"m-a","inputTokens":10,"outputTokens":1,"thoughtsTokens":0,"costKnown":true,"estimatedCostUsd":0.1}`),
		lfRow("2026-10-10T16:30:00Z", `{"mode":"on","model":"m-a","inputTokens":20,"outputTokens":2,"thoughtsTokens":0,"costKnown":true,"estimatedCostUsd":0.2}`),
		lfRow("2026-10-10T16:40:00Z", `{"mode":"shadow","model":"m-a","inputTokens":5,"outputTokens":1,"thoughtsTokens":1,"costKnown":false}`),
	}
	r := AggregateLLMFirstUsage(rows, 16500)
	if len(r.ByDay) != 3 {
		t.Fatalf("byDay = %+v", r.ByDay)
	}
	want := []struct {
		date, mode string
		msgs       int
	}{{"2026-10-10", "on", 1}, {"2026-10-10", "shadow", 1}, {"2026-10-11", "on", 1}}
	for i, w := range want {
		g := r.ByDay[i]
		if g.Date != w.date || g.Mode != w.mode || g.Messages != w.msgs {
			t.Errorf("byDay[%d] = %+v, want %+v", i, g, w)
		}
	}
	if r.ByDay[1].UnknownCostMessages != 1 || r.ByDay[1].EstimatedCostUSD != 0 {
		t.Errorf("shadow day = %+v", r.ByDay[1])
	}
}

func TestAggregateLLMFirstUsageByModelAndLegacyEvents(t *testing.T) {
	rows := []LLMFirstUsageRow{
		lfRow("2026-10-10T03:00:00Z", `{"mode":"on","model":"m-a","costKnown":true,"estimatedCostUsd":0.5}`),
		lfRow("2026-10-10T03:01:00Z", `{"mode":"on","model":"m-a","costKnown":true,"estimatedCostUsd":0.25}`),
		lfRow("2026-10-10T03:02:00Z", `{"mode":"on","model":"m-x","costKnown":false}`),
		// legacy: no mode, no costKnown; pipeline tells shadow; cost present => known
		lfRow("2026-10-10T03:03:00Z", `{"pipeline":"llm_first_shadow","model":"m-old","estimatedCostUsd":0.1}`),
		// legacy without cost => unknown; no model
		lfRow("2026-10-10T03:04:00Z", `{}`),
		lfRow("2026-10-10T03:05:00Z", `not json`),
	}
	r := AggregateLLMFirstUsage(rows, 16500)
	if r.Totals.ShadowMessages != 1 || r.Totals.LiveMessages != 5 || r.Totals.Messages != 6 {
		t.Fatalf("totals = %+v", r.Totals)
	}
	if r.Totals.UnknownCostMessages != 3 || !near(r.Totals.EstimatedCostUSD, 0.85) {
		t.Fatalf("cost = %+v", r.Totals)
	}
	got := map[string]LLMFirstModelUsage{}
	for _, m := range r.ByModel {
		got[m.Model] = m
	}
	if m := got["m-a"]; m.Messages != 2 || !near(m.EstimatedCostUSD, 0.75) || !m.CostKnown {
		t.Errorf("m-a = %+v", m)
	}
	if m := got["m-x"]; m.Messages != 1 || m.EstimatedCostUSD != 0 || m.CostKnown {
		t.Errorf("m-x = %+v", m)
	}
	if m := got["m-old"]; !m.CostKnown || !near(m.EstimatedCostUSD, 0.1) {
		t.Errorf("m-old = %+v", m)
	}
	if m, ok := got["unknown"]; !ok || m.Messages != 2 || m.CostKnown {
		t.Errorf("unknown = %+v", m)
	}
	if r.ByModel[0].Model != "m-a" {
		t.Errorf("byModel must be ordered by messages desc: %+v", r.ByModel)
	}
}

func TestAggregateLLMFirstUsageEmptyHasEmptySlicesAndZeroAverages(t *testing.T) {
	r := AggregateLLMFirstUsage(nil, 16500)
	if r.ByDay == nil || r.ByModel == nil || len(r.ByDay) != 0 || len(r.ByModel) != 0 {
		t.Fatalf("slices must be non-nil empty: %+v", r)
	}
	if r.AvgPerMessage != (LLMFirstAverages{}) || r.Totals != (LLMFirstTotals{}) {
		t.Fatalf("zero expected: %+v", r)
	}
}
