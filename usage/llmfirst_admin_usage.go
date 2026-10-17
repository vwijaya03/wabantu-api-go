package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// DefaultIDRPerUSD is the fixed rate used for rupiah figures (same as docs/LLM_FIRST_PIPELINE.md §5.1).
const DefaultIDRPerUSD = 16500

// jakartaZone: Indonesia has no DST, so a fixed +07:00 zone is exact and needs no tzdata.
var jakartaZone = time.FixedZone("Asia/Jakarta", 7*3600)

// LLMFirstUsageRow is one ai_token_llm_first usage_event row.
type LLMFirstUsageRow struct {
	CreatedAt time.Time
	Metadata  []byte
}

type LLMFirstTotals struct {
	Messages            int     `json:"messages"`
	LiveMessages        int     `json:"liveMessages"`
	ShadowMessages      int     `json:"shadowMessages"`
	Fallbacks           int     `json:"fallbacks"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	ThoughtsTokens      int64   `json:"thoughtsTokens"`
	EstimatedCostUSD    float64 `json:"estimatedCostUsd"`
	UnknownCostMessages int     `json:"unknownCostMessages"`
}

type LLMFirstAverages struct {
	InputTokens    float64 `json:"inputTokens"`
	OutputTokens   float64 `json:"outputTokens"`
	ThoughtsTokens float64 `json:"thoughtsTokens"`
	CostUSD        float64 `json:"costUsd"`
	CostIDR        float64 `json:"costIdr"`
}

type LLMFirstDayUsage struct {
	Date                string  `json:"date"` // YYYY-MM-DD, Asia/Jakarta
	Mode                string  `json:"mode"` // "on" | "shadow"
	Messages            int     `json:"messages"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	ThoughtsTokens      int64   `json:"thoughtsTokens"`
	EstimatedCostUSD    float64 `json:"estimatedCostUsd"`
	UnknownCostMessages int     `json:"unknownCostMessages"`
}

type LLMFirstModelUsage struct {
	Model            string  `json:"model"`
	Messages         int     `json:"messages"`
	EstimatedCostUSD float64 `json:"estimatedCostUsd"`
	CostKnown        bool    `json:"costKnown"` // false when any message of the model has no known cost
}

// LLMFirstUsageReport is the response body of GET /api/v1/admin/ai/llm-first/usage.
type LLMFirstUsageReport struct {
	TenantID      string               `json:"tenantId"`
	Days          int                  `json:"days"`
	Totals        LLMFirstTotals       `json:"totals"`
	AvgPerMessage LLMFirstAverages     `json:"avgPerMessage"`
	ByDay         []LLMFirstDayUsage   `json:"byDay"`
	ByModel       []LLMFirstModelUsage `json:"byModel"`
	IDRPerUSD     float64              `json:"idrPerUsd"`
}

type llmFirstMeta struct {
	Mode             string   `json:"mode"`
	Pipeline         string   `json:"pipeline"`
	Model            string   `json:"model"`
	Fallback         bool     `json:"fallback"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	ThoughtsTokens   int64    `json:"thoughtsTokens"`
	CostKnown        *bool    `json:"costKnown"`
	EstimatedCostUSD *float64 `json:"estimatedCostUsd"`
}

// AggregateLLMFirstUsage folds ai_token_llm_first rows into the report (pure; no I/O). A message
// whose cost is unknown is left out of every cost sum and counted in unknownCostMessages; legacy
// events without costKnown count as known only when they carry estimatedCostUsd. Unparseable
// metadata still counts as one message with unknown cost.
func AggregateLLMFirstUsage(rows []LLMFirstUsageRow, idrPerUSD float64) LLMFirstUsageReport {
	rep := LLMFirstUsageReport{ByDay: []LLMFirstDayUsage{}, ByModel: []LLMFirstModelUsage{}, IDRPerUSD: idrPerUSD}
	type dayKey struct{ date, mode string }
	days := map[dayKey]*LLMFirstDayUsage{}
	models := map[string]*LLMFirstModelUsage{}
	knownCount := 0

	for _, row := range rows {
		var m llmFirstMeta
		_ = json.Unmarshal(row.Metadata, &m) // bad JSON leaves m zero: unknown cost, mode on

		mode := "on"
		if m.Mode == "shadow" || (m.Mode == "" && m.Pipeline == "llm_first_shadow") {
			mode = "shadow"
		}
		known := m.EstimatedCostUSD != nil
		if m.CostKnown != nil {
			known = known && *m.CostKnown
		}
		cost := 0.0
		if known {
			cost = *m.EstimatedCostUSD
		}
		model := m.Model
		if model == "" {
			model = "unknown"
		}

		t := &rep.Totals
		t.Messages++
		if mode == "shadow" {
			t.ShadowMessages++
		} else {
			t.LiveMessages++
		}
		if m.Fallback {
			t.Fallbacks++
		}
		t.InputTokens += m.InputTokens
		t.OutputTokens += m.OutputTokens
		t.ThoughtsTokens += m.ThoughtsTokens

		dk := dayKey{row.CreatedAt.In(jakartaZone).Format("2006-01-02"), mode}
		d := days[dk]
		if d == nil {
			d = &LLMFirstDayUsage{Date: dk.date, Mode: mode}
			days[dk] = d
		}
		d.Messages++
		d.InputTokens += m.InputTokens
		d.OutputTokens += m.OutputTokens
		d.ThoughtsTokens += m.ThoughtsTokens

		mu := models[model]
		if mu == nil {
			mu = &LLMFirstModelUsage{Model: model, CostKnown: true}
			models[model] = mu
		}
		mu.Messages++

		if known {
			knownCount++
			t.EstimatedCostUSD += cost
			d.EstimatedCostUSD += cost
			mu.EstimatedCostUSD += cost
		} else {
			t.UnknownCostMessages++
			d.UnknownCostMessages++
			mu.CostKnown = false
		}
	}

	for _, d := range days {
		rep.ByDay = append(rep.ByDay, *d)
	}
	sort.Slice(rep.ByDay, func(i, j int) bool {
		a, b := rep.ByDay[i], rep.ByDay[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.Mode == "on" && b.Mode != "on"
	})
	for _, mu := range models {
		rep.ByModel = append(rep.ByModel, *mu)
	}
	sort.Slice(rep.ByModel, func(i, j int) bool {
		a, b := rep.ByModel[i], rep.ByModel[j]
		if a.Messages != b.Messages {
			return a.Messages > b.Messages
		}
		return a.Model < b.Model
	})

	if n := float64(rep.Totals.Messages); n > 0 {
		rep.AvgPerMessage.InputTokens = float64(rep.Totals.InputTokens) / n
		rep.AvgPerMessage.OutputTokens = float64(rep.Totals.OutputTokens) / n
		rep.AvgPerMessage.ThoughtsTokens = float64(rep.Totals.ThoughtsTokens) / n
	}
	if knownCount > 0 {
		rep.AvgPerMessage.CostUSD = rep.Totals.EstimatedCostUSD / float64(knownCount)
		rep.AvgPerMessage.CostIDR = rep.AvgPerMessage.CostUSD * idrPerUSD
	}
	return rep
}

// FetchLLMFirstUsage reads the last `days` days of ai_token_llm_first events of one tenant schema
// and aggregates them. tenantSchema MUST come from the trusted tenant lookup (it is quoted into
// the SQL, validated against the schema-name rules here as defense in depth); everything else is
// a bound parameter.
func FetchLLMFirstUsage(ctx context.Context, tenantSchema, tenantID string, days int) (*LLMFirstUsageReport, error) {
	if !validTenantSchemaIdent(tenantSchema) {
		return nil, appErrs.Internal("invalid tenant schema")
	}
	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT created_at, metadata
		 FROM "%s".usage_event
		 WHERE event_type = $1
		   AND created_at >= now() - make_interval(days => $2::int)
		 ORDER BY created_at`, tenantSchema),
		EventAITokenLLMFirst, days)
	if err != nil {
		return nil, appErrs.Internal("gagal memuat pemakaian LLM-first")
	}
	defer rows.Close()
	var data []LLMFirstUsageRow
	for rows.Next() {
		var r LLMFirstUsageRow
		if err := rows.Scan(&r.CreatedAt, &r.Metadata); err != nil {
			return nil, appErrs.Internal("gagal membaca pemakaian LLM-first")
		}
		data = append(data, r)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal("gagal membaca pemakaian LLM-first")
	}
	rep := AggregateLLMFirstUsage(data, DefaultIDRPerUSD)
	rep.TenantID, rep.Days = tenantID, days
	return &rep, nil
}

func validTenantSchemaIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, c := range s {
		switch {
		case c == '_' || (c >= 'a' && c <= 'z'):
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
