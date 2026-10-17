package usage

import (
	"context"
	"database/sql"
	"fmt"

	"encore.dev/rlog"
)

// Pricing per 1M tokens (USD) — Claude model family
type modelPricing struct {
	InputPer1M  float64
	OutputPer1M float64
}

var pricingTable = map[string]modelPricing{
	"claude-haiku-4-5":           {InputPer1M: 1.00, OutputPer1M: 5.00},
	"claude-haiku-4-5-20251001":  {InputPer1M: 1.00, OutputPer1M: 5.00},
	"claude-sonnet-4-5":          {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-sonnet-4-6":          {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-opus-4-7":            {InputPer1M: 5.00, OutputPer1M: 25.00},
	"claude-sonnet-4-5-20250929": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-sonnet-4-5-20250514": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-3-5-sonnet-20241022": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-3-5-haiku-20241022":  {InputPer1M: 0.80, OutputPer1M: 4.00},
	"claude-3-opus-20240229":     {InputPer1M: 15.00, OutputPer1M: 75.00},
	"claude-3-haiku-20240307":    {InputPer1M: 0.25, OutputPer1M: 1.25},
	// Gemini Developer API paid tier, Standard, text (ai.google.dev/gemini-api/docs/pricing, 2026-10-09).
	// 2.5 Pro uses the <=200k prompt tier; the long-context tier is not modeled.
	"gemini-3.1-flash-lite": {InputPer1M: 0.25, OutputPer1M: 1.50},
	"gemini-2.5-flash-lite": {InputPer1M: 0.10, OutputPer1M: 0.40},
	"gemini-2.5-flash":      {InputPer1M: 0.30, OutputPer1M: 2.50},
	"gemini-2.5-pro":        {InputPer1M: 1.25, OutputPer1M: 10.00},
	// Gemini 3.x, Standard paid tier, text, from https://ai.google.dev/gemini-api/docs/pricing (read 2026-10-17);
	// thinking tokens are billed as output.
	//   3.6 Flash / 3.8 Flash: promotional price through 2026-12-31 ($0.75 in / $3.75 out); the page
	//   lists $1.50 / $7.50 from 2027-01-01, so update these two rows then.
	//   3.5 Flash-Lite: $0.30 in / $2.50 out.
	//   3.1 Pro Preview: $2.00 in / $12.00 out for prompts <= 200k tokens (the > 200k tier is not modeled).
	"gemini-3.8-flash":       {InputPer1M: 0.75, OutputPer1M: 3.75},
	"gemini-3.6-flash":       {InputPer1M: 0.75, OutputPer1M: 3.75},
	"gemini-3.5-flash-lite":  {InputPer1M: 0.30, OutputPer1M: 2.50},
	"gemini-3.1-pro-preview": {InputPer1M: 2.00, OutputPer1M: 12.00},
	// OpenAI embeddings: input only (platform.openai.com/docs/pricing).
	"text-embedding-3-small": {InputPer1M: 0.02, OutputPer1M: 0},
	// Gemini embedding (ai.google.dev/gemini-api/docs/pricing): input only, paid tier.
	"gemini-embedding-001": {InputPer1M: 0.15, OutputPer1M: 0},
}

// ModelPriced reports whether pricingTable has a price for model.
func ModelPriced(model string) bool {
	_, ok := pricingTable[model]
	return ok
}

// EstimateTokenCost returns the estimated USD cost for a given model and token counts. A model
// without a price returns 0 (and logs once per call): the cost is unknown, never guessed from
// another model's price. Use ModelPriced to tell "free" from "unknown".
func EstimateTokenCost(model string, inputTokens, outputTokens int) float64 {
	cost, _ := EstimateTokenCostKnown(model, inputTokens, outputTokens)
	return cost
}

// EstimateTokenCostKnown is EstimateTokenCost with an explicit flag: known=false means the model
// has no entry in pricingTable and the returned cost (0) is not an estimate.
func EstimateTokenCostKnown(model string, inputTokens, outputTokens int) (cost float64, known bool) {
	pricing, ok := pricingTable[model]
	if !ok {
		rlog.Warn("unknown model for cost estimation, cost not estimated", "model", model)
		return 0, false
	}
	inputCost := float64(inputTokens) / 1_000_000 * pricing.InputPer1M
	outputCost := float64(outputTokens) / 1_000_000 * pricing.OutputPer1M
	return inputCost + outputCost, true
}

// UsagePeriod represents a time period for aggregation.
type UsagePeriod string

const (
	PeriodDay   UsagePeriod = "day"
	PeriodWeek  UsagePeriod = "week"
	PeriodMonth UsagePeriod = "month"
)

// TenantAICostEstimate holds the cost summary for a tenant.
type TenantAICostEstimate struct {
	TotalInputTokens  int     `json:"totalInputTokens"`
	TotalOutputTokens int     `json:"totalOutputTokens"`
	TotalRequests     int     `json:"totalRequests"`
	EstimatedCostUSD  float64 `json:"estimatedCostUsd"`
	// UnpricedModels lists the models that had tokens in the period but no pricingTable entry.
	// Their tokens are counted above but add nothing to EstimatedCostUSD (cost unknown, not guessed).
	UnpricedModels []string `json:"unpricedModels,omitempty"`
}

// GetTenantAICostEstimate aggregates the tenant's AI activity (usage_event "ai_activity" rows, which
// carry the model and the input/output split) and prices each model with pricingTable. A model
// without a price is listed in UnpricedModels instead of being priced as another model.
func GetTenantAICostEstimate(ctx context.Context, tenantSchema string, period UsagePeriod) (*TenantAICostEstimate, error) {
	if err := validateSchema(tenantSchema); err != nil {
		return nil, err
	}
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return nil, fmt.Errorf("get tenant DB: %w", err)
	}

	interval := "1 day"
	switch period {
	case PeriodWeek:
		interval = "7 days"
	case PeriodMonth:
		interval = "30 days"
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(metadata->>'model',''),
		        COALESCE(SUM(NULLIF(metadata->>'inputTokens','')::bigint),0),
		        COALESCE(SUM(NULLIF(metadata->>'outputTokens','')::bigint),0)
		 FROM %q.usage_event
		 WHERE event_type = $1 AND metadata->>'llmUsed' = 'true'
		   AND created_at >= NOW() - INTERVAL '%s'
		 GROUP BY 1`,
		tenantSchema, interval), EventTypeAIActivity)
	if err != nil {
		return nil, fmt.Errorf("query AI activity: %w", err)
	}
	defer rows.Close()

	var result TenantAICostEstimate
	for rows.Next() {
		var model string
		var in, out int64
		if err := rows.Scan(&model, &in, &out); err != nil {
			return nil, fmt.Errorf("scan AI activity: %w", err)
		}
		result.TotalInputTokens += int(in)
		result.TotalOutputTokens += int(out)
		cost, known := EstimateTokenCostKnown(model, int(in), int(out))
		if !known {
			if in+out > 0 {
				result.UnpricedModels = append(result.UnpricedModels, model)
			}
			continue
		}
		result.EstimatedCostUSD += cost
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read AI activity: %w", err)
	}

	var convCount int64
	err = db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(SUM(quantity),0) FROM %q.usage_event
		 WHERE event_type = 'ai_conversation' AND created_at >= NOW() - INTERVAL '%s'`,
		tenantSchema, interval)).Scan(&convCount)
	if err != nil {
		return nil, fmt.Errorf("query AI conversations: %w", err)
	}
	result.TotalRequests = int(convCount)
	return &result, nil
}

func getTenantDB(_ context.Context, _ string) (*sql.DB, error) {
	return db.Stdlib(), nil
}
