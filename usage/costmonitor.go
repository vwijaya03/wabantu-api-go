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
	"claude-sonnet-4-5-20250514":  {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-3-5-sonnet-20241022":  {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-3-5-haiku-20241022":   {InputPer1M: 0.80, OutputPer1M: 4.00},
	"claude-3-opus-20240229":      {InputPer1M: 15.00, OutputPer1M: 75.00},
	"claude-3-haiku-20240307":     {InputPer1M: 0.25, OutputPer1M: 1.25},
}

// EstimateTokenCost returns the estimated USD cost for a given model and token counts.
func EstimateTokenCost(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := pricingTable[model]
	if !ok {
		rlog.Warn("unknown model for cost estimation, using sonnet default", "model", model)
		pricing = pricingTable["claude-sonnet-4-5-20250514"]
	}
	inputCost := float64(inputTokens) / 1_000_000 * pricing.InputPer1M
	outputCost := float64(outputTokens) / 1_000_000 * pricing.OutputPer1M
	return inputCost + outputCost
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
}

// GetTenantAICostEstimate aggregates AI usage and returns estimated cost.
func GetTenantAICostEstimate(ctx context.Context, tenantSchema string, period UsagePeriod) (*TenantAICostEstimate, error) {
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

	q := fmt.Sprintf(`SELECT
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COUNT(*)
	FROM %q.ai_usage_log
	WHERE created_at >= NOW() - INTERVAL '%s'`, tenantSchema, interval)

	var result TenantAICostEstimate
	err = db.QueryRowContext(ctx, q).Scan(
		&result.TotalInputTokens,
		&result.TotalOutputTokens,
		&result.TotalRequests,
	)
	if err != nil {
		return nil, fmt.Errorf("query AI usage: %w", err)
	}

	result.EstimatedCostUSD = EstimateTokenCost(
		"claude-sonnet-4-5-20250514",
		result.TotalInputTokens,
		result.TotalOutputTokens,
	)

	return &result, nil
}

func getTenantDB(_ context.Context, _ string) (*sql.DB, error) {
	return nil, fmt.Errorf("tenant DB resolver not yet wired — implement shared/tenantdb")
}
