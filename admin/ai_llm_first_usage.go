package admin

import (
	"context"
	"strconv"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/wabantu/usage"
)

const (
	llmFirstUsageDefaultDays = 7
	llmFirstUsageMaxDays     = 90
)

type LLMFirstUsageParams struct {
	TenantID string `query:"tenantId"`
	Days     string `query:"days"` // 1..90, default 7
}

// parseLLMFirstUsageDays: empty = default 7; otherwise an integer in 1..90.
func parseLLMFirstUsageDays(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return llmFirstUsageDefaultDays, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > llmFirstUsageMaxDays {
		return 0, &errs.Error{Code: errs.InvalidArgument, Message: "days harus berupa angka 1 sampai 90"}
	}
	return n, nil
}

// GetLLMFirstUsage reports the token and cost measurement (ai_token_llm_first events) of one
// tenant over the last `days` days. Read-only; super_admin only (admin session, role from the
// auth session). The tenant schema comes from the tenant lookup, never from the request.
//
//encore:api auth method=GET path=/api/v1/admin/ai/llm-first/usage tag:super_admin
func GetLLMFirstUsage(ctx context.Context, p *LLMFirstUsageParams) (*usage.LLMFirstUsageReport, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	days, err := parseLLMFirstUsageDays(p.Days)
	if err != nil {
		return nil, err
	}
	tenantID, err := requireTenant(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	schema, err := resolveTenantSchema(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return usage.FetchLLMFirstUsage(ctx, schema, tenantID, days)
}
