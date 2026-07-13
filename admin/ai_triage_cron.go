package admin

import (
	"context"
	"database/sql"
	"time"

	"encore.dev/cron"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenant"
)

var _ = cron.NewJob("ai-triage-anomaly-scan", cron.JobConfig{
	Title:    "AI Triage Anomaly Scan",
	Schedule: "0 * * * *",
	Endpoint: ScanAITriageAnomalies,
})

// ScanAITriageAnomalies refreshes the system snapshot of recent AI activity per tenant (read-only).
//
//encore:api private method=POST path=/api/v1/admin/ai-triage/scan
func ScanAITriageAnomalies(ctx context.Context) error {
	start := time.Now()
	tenants, err := tenant.ListActiveTenantSchemas(ctx)
	if err != nil {
		rlog.Error("ai-triage scan: list tenants", "err", err)
		return err
	}

	if _, err := system.DB.Exec(ctx, `TRUNCATE ai_triage_anomaly`); err != nil {
		rlog.Error("ai-triage scan: truncate", "err", err)
		return err
	}

	scannedAt := time.Now().UTC()
	remaining := ai.TriageAnomalyGlobalCap()
	tenantsScanned := 0
	tenantsFailed := 0
	rowsCollected := 0

	for _, t := range tenants {
		if remaining <= 0 {
			break
		}
		limit := ai.TriageScanLimitForTenant(remaining)
		if limit <= 0 {
			break
		}

		entries, err := ai.FetchRecentAIActivityAnomalies(ctx, t.SchemaName, limit)
		if err != nil {
			tenantsFailed++
			rlog.Warn("ai-triage scan: tenant failed", "tenantId", t.TenantID, "schema", t.SchemaName, "err", err)
			continue
		}
		tenantsScanned++

		for _, e := range entries {
			if err := insertAnomalySnapshot(ctx, t.TenantID, t.SchemaName, e, scannedAt); err != nil {
				rlog.Warn("ai-triage scan: insert row", "tenantId", t.TenantID, "err", err)
				continue
			}
			rowsCollected++
			remaining--
			if remaining <= 0 {
				break
			}
		}
	}

	if _, err := system.DB.Exec(ctx, `
		DELETE FROM ai_triage_anomaly
		WHERE scanned_at < now() - interval '24 hours'`); err != nil {
		rlog.Warn("ai-triage scan: cleanup old rows", "err", err)
	}

	rlog.Info("ai-triage scan complete",
		"tenants", len(tenants),
		"tenantsScanned", tenantsScanned,
		"tenantsFailed", tenantsFailed,
		"rows", rowsCollected,
		"durationMs", time.Since(start).Milliseconds(),
	)
	return nil
}

func insertAnomalySnapshot(ctx context.Context, tenantID, schema string, e ai.TriageAnomalyEntry, scannedAt time.Time) error {
	var conv, inbound any
	if e.ConversationID != "" {
		conv = e.ConversationID
	}
	if e.InboundID != "" {
		inbound = e.InboundID
	}
	_, err := system.DB.Exec(ctx, `
		INSERT INTO ai_triage_anomaly (
			tenant_id, tenant_schema, conversation_id, inbound_id,
			path, reason, user_text, review_suggested, source_created_at, scanned_at
		) VALUES (
			$1::uuid, $2, $3::uuid, $4::uuid,
			$5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10
		)`,
		tenantID, schema, conv, inbound,
		e.Path, e.Reason, e.UserText, e.ReviewSuggested, e.CreatedAt, scannedAt,
	)
	return err
}

func listAnomaliesFromSnapshot(ctx context.Context, tenantID string, limit int) ([]AITriageAnomaly, error) {
	rows, err := system.DB.Query(ctx, `
		SELECT tenant_id::text, tenant_schema, conversation_id::text, inbound_id::text,
		       path, reason, user_text, review_suggested, source_created_at
		FROM ai_triage_anomaly
		WHERE tenant_id = $1::uuid
		ORDER BY source_created_at DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AITriageAnomaly, 0)
	for rows.Next() {
		var a AITriageAnomaly
		var conv, inbound, reason, userText sql.NullString
		if err := rows.Scan(
			&a.TenantID, &a.TenantSchema, &conv, &inbound,
			&a.Path, &reason, &userText, &a.ReviewSuggested, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		if conv.Valid {
			a.ConversationID = conv.String
		}
		if inbound.Valid {
			a.InboundID = inbound.String
		}
		if reason.Valid {
			a.Reason = reason.String
		}
		if userText.Valid {
			a.UserText = userText.String
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
