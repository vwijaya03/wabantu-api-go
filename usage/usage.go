package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/cron"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

// ---------- plan quotas ----------

var planQuotas = map[string]map[string]int{
	// Trial: all features unlocked (see entitlement); quotas far below paid Starter.
	"trial": {
		"ai_conversation":   60,
		"ai_token":          100_000,
		"broadcast_contact": 20,
		"storage_byte":      52_428_800, // 50 MB
		"admin_seat":        1,
		"workflow_exec":     8,
	},
	"starter": {
		"ai_conversation":   1_500,
		"ai_token":          2_000_000,
		"broadcast_contact": 0,
		"storage_byte":      268_435_456,
		"admin_seat":        1,
		"workflow_exec":     50,
	},
	"business": {
		"ai_conversation":   6_000,
		"ai_token":          8_000_000,
		"broadcast_contact": 500,
		"storage_byte":      2_147_483_648,
		"admin_seat":        3,
		"workflow_exec":     500,
	},
	"basic": { // legacy alias
		"ai_conversation":   6_000,
		"ai_token":          8_000_000,
		"broadcast_contact": 500,
		"storage_byte":      2_147_483_648,
		"admin_seat":        3,
		"workflow_exec":     500,
	},
	"pro": {
		"ai_conversation":   20_000,
		"ai_token":          30_000_000,
		"broadcast_contact": 10_000,
		"storage_byte":      10_737_418_240,
		"admin_seat":        10,
		"workflow_exec":     5_000,
	},
}

// ---------- types ----------

type UsageEvent struct {
	ID        string         `json:"id"`
	EventType string         `json:"eventType"`
	Quantity  int            `json:"quantity"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type UsageAggregate struct {
	EventType string `json:"eventType"`
	Period    string `json:"period"`
	Quantity  int64  `json:"quantity"`
}

type QuotaItem struct {
	EventType string `json:"eventType"`
	Used      int64  `json:"used"`
	Limit     int    `json:"limit"`
	Remaining int    `json:"remaining"`
}

type UsageSummary struct {
	Period string      `json:"period"`
	Plan   string      `json:"plan"`
	Quotas []QuotaItem `json:"quotas"`
}

type RecordEventParams struct {
	TenantSchema string         `json:"tenantSchema"`
	EventType    string         `json:"eventType"`
	Quantity     int            `json:"quantity"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

type SummaryParams struct {
	Period string `query:"period"`
}

// ---------- cron ----------

var _ = cron.NewJob("reset-monthly-usage", cron.JobConfig{
	Title:    "Monthly Usage Period Rotation",
	Schedule: "0 0 1 * *",
	Endpoint: ResetMonthlyUsage,
})

// ---------- endpoints ----------

//encore:api auth method=GET path=/api/v1/usage/summary
func Summary(ctx context.Context, p *SummaryParams) (*UsageSummary, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}

	period := p.Period
	if period == "" {
		period = time.Now().Format("2006-01")
	}
	return GetSummary(ctx, u.TenantSchema, period)
}

//encore:api private method=POST path=/api/v1/usage/record
func Record(ctx context.Context, p *RecordEventParams) error {
	return RecordEvent(ctx, p.TenantSchema, p.EventType, p.Quantity, p.Metadata)
}

//encore:api private method=POST path=/api/v1/usage/reset
func ResetMonthlyUsage(ctx context.Context) error {
	rlog.Info("monthly usage period rotated", "period", time.Now().Format("2006-01"))
	return nil
}

// ---------- exported helpers ----------

func RecordEvent(ctx context.Context, tenantSchema, eventType string, quantity int, metadata json.RawMessage) error {
	if err := validateSchema(tenantSchema); err != nil {
		return err
	}

	metaJSON := metadata
	if len(metaJSON) == 0 {
		metaJSON = []byte("{}")
	}
	period := time.Now().Format("2006-01")

	_, err := db.Exec(ctx, fmt.Sprintf(
		`INSERT INTO "%s".usage_event (event_type, quantity, metadata) VALUES ($1,$2,$3)`,
		tenantSchema), eventType, quantity, metaJSON)
	if err != nil {
		return fmt.Errorf("insert usage_event: %w", err)
	}

	_, err = db.Exec(ctx, fmt.Sprintf(
		`INSERT INTO "%s".usage_aggregate (event_type, period, quantity)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (event_type, period)
		 DO UPDATE SET quantity = "%s".usage_aggregate.quantity + EXCLUDED.quantity,
		              updated_at = NOW()`,
		tenantSchema, tenantSchema), eventType, period, quantity)
	if err != nil {
		return fmt.Errorf("upsert usage_aggregate: %w", err)
	}

	return nil
}

func CheckQuota(ctx context.Context, tenantSchema, eventType string) (allowed bool, remaining int, limit int) {
	plan := getTenantPlan(ctx, tenantSchema)
	quotas, ok := planQuotas[plan]
	if !ok {
		quotas = planQuotas["starter"]
	}
	limit, hasLimit := quotas[eventType]
	if !hasLimit {
		return true, -1, -1
	}

	period := time.Now().Format("2006-01")
	var used int64
	_ = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COALESCE(quantity,0) FROM "%s".usage_aggregate
		 WHERE event_type=$1 AND period=$2`, tenantSchema),
		eventType, period).Scan(&used)

	rem := limit - int(used)
	if rem < 0 {
		rem = 0
	}
	return int(used) < limit, rem, limit
}

func GetSummary(ctx context.Context, tenantSchema, period string) (*UsageSummary, error) {
	if err := validateSchema(tenantSchema); err != nil {
		return nil, err
	}

	plan := getTenantPlan(ctx, tenantSchema)
	quotas, ok := planQuotas[plan]
	if !ok {
		quotas = planQuotas["starter"]
	}

	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT event_type, COALESCE(quantity,0)
		 FROM "%s".usage_aggregate WHERE period=$1`, tenantSchema), period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	usedMap := map[string]int64{}
	for rows.Next() {
		var et string
		var q int64
		if err := rows.Scan(&et, &q); err != nil {
			return nil, err
		}
		usedMap[et] = q
	}

	items := make([]QuotaItem, 0, len(quotas))
	for et, lim := range quotas {
		used := usedMap[et]
		rem := int(lim) - int(used)
		if rem < 0 {
			rem = 0
		}
		items = append(items, QuotaItem{
			EventType: et,
			Used:      used,
			Limit:     lim,
			Remaining: rem,
		})
	}

	return &UsageSummary{Period: period, Plan: plan, Quotas: items}, nil
}

// ---------- internal ----------

// TenantPlan returns the active subscription plan code for a tenant schema.
func TenantPlan(ctx context.Context, tenantSchema string) string {
	return getTenantPlan(ctx, tenantSchema)
}

func getTenantPlan(ctx context.Context, tenantSchema string) string {
	var plan string
	var isTrial bool
	err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COALESCE(plan_code,'starter'), COALESCE(is_trial,false)
		 FROM "%s".subscription
		 WHERE status = 'active'
		 ORDER BY updated_at DESC LIMIT 1`, tenantSchema),
	).Scan(&plan, &isTrial)
	if err != nil || plan == "" {
		return "trial"
	}
	if isTrial {
		return "trial"
	}
	return normalizePlanCode(plan)
}

func normalizePlanCode(plan string) string {
	if plan == "basic" {
		return "business"
	}
	return plan
}

func validateSchema(schema string) error {
	for _, c := range schema {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return appErrs.BadRequest("invalid tenant schema")
		}
	}
	return nil
}
