package admin

import (
	"context"

	"encore.dev/beta/errs"

	"encore.app/wabantu/usage"
)

// ListTenantAIActivity returns AI model/path logs for any tenant (platform super_admin only).
//
//encore:api auth method=GET path=/api/v1/admin/tenant/:id/ai-activity tag:super_admin
func ListTenantAIActivity(ctx context.Context, id string, p *usage.ListAIActivityParams) (*usage.ListAIActivityResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	schema, err := resolveTenantSchema(ctx, id)
	if err != nil {
		return nil, err
	}
	limit := 100
	if p != nil {
		limit = p.Limit
	}
	period := ""
	if p != nil {
		period = p.Period
	}
	return usage.FetchAIActivityList(ctx, schema, period, limit)
}

// GetTenantAIActivitySummary returns monthly AI usage rollups for a tenant (platform super_admin only).
//
//encore:api auth method=GET path=/api/v1/admin/tenant/:id/ai-activity/summary tag:super_admin
func GetTenantAIActivitySummary(ctx context.Context, id string, p *usage.ListAIActivityParams) (*usage.AIActivitySummary, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	schema, err := resolveTenantSchema(ctx, id)
	if err != nil {
		return nil, err
	}
	period := ""
	if p != nil {
		period = p.Period
	}
	return usage.FetchAIActivitySummary(ctx, schema, period)
}

func resolveTenantSchema(ctx context.Context, tenantID string) (string, error) {
	var schema string
	err := db.QueryRow(ctx, `
		SELECT tc.schema_name
		FROM tenant t
		JOIN tenant_company tc ON tc.tenant_id = t.id
		WHERE t.id = $1 AND t.deleted_at IS NULL`,
		tenantID,
	).Scan(&schema)
	if err != nil {
		return "", &errs.Error{Code: errs.NotFound, Message: "tenant not found"}
	}
	if schema == "" {
		return "", &errs.Error{Code: errs.NotFound, Message: "tenant schema not found"}
	}
	return schema, nil
}
