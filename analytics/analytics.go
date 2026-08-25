package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	e "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
)

var tenantDB = sqldb.Named("tenant")

func openTenantScope(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	return appdb.OpenTenantScope(tenantDB.Stdlib(), schema), nil
}

// ─── Types ───────────────────────────────────────────────────────────────────

type OverviewRequest struct {
	Days int `query:"days"`
}

type Totals struct {
	TotalMessages       int `json:"totalMessages"`
	InboundMessages     int `json:"inboundMessages"`
	AIReplies           int `json:"aiReplies"`
	HumanReplies        int `json:"humanReplies"`
	LeadsGenerated      int `json:"leadsGenerated"`
	UnreadConversations int `json:"unreadConversations"`
}

type TodayStats struct {
	Inbound       int `json:"inbound"`
	AIReplies     int `json:"aiReplies"`
	AICoveragePct int `json:"aiCoveragePct"`
}

type OverviewMeta struct {
	OpenRatePct         *int     `json:"openRatePct"`
	AvgFirstResponseSec *float64 `json:"avgFirstResponseSec"`
}

type KPIs struct {
	AICoveragePct         int `json:"aiCoveragePct"`
	HandoffRatePct        int `json:"handoffRatePct"`
	ConversionEstimatePct int `json:"conversionEstimatePct"`
}

type TopQuestion struct {
	Question string `json:"question"`
	Count    int    `json:"count"`
}

type OverviewResponse struct {
	WindowDays        int           `json:"windowDays"`
	Totals            Totals        `json:"totals"`
	Today             TodayStats    `json:"today"`
	ReportingTimezone string        `json:"reportingTimezone"`
	Overview          OverviewMeta  `json:"overview"`
	KPIs              KPIs          `json:"kpis"`
	TopQuestions      []TopQuestion `json:"topQuestions"`
}

// ─── Auth helper ─────────────────────────────────────────────────────────────

func currentUser(ctx context.Context) (*types.AuthUser, error) {
	uid, ok := auth.UserID()
	data := auth.Data()
	if !ok || uid == "" || data == nil {
		return nil, e.Unauthenticated("not authenticated")
	}
	u, valid := data.(*types.AuthUser)
	if !valid {
		return nil, e.Unauthenticated("invalid auth data")
	}
	return u, nil
}

// ─── Endpoint ────────────────────────────────────────────────────────────────

//encore:api auth method=GET path=/api/v1/analytics/overview
func Overview(ctx context.Context, req *OverviewRequest) (*OverviewResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	if err := tenant.EnsureTenantSchemaProvisioned(ctx, u.TenantSchema); err != nil {
		return nil, e.Internal("tenant schema not ready")
	}

	days := req.Days
	if days < 1 || days > 90 {
		days = 30
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	reportingTz := resolveReportingTimezone(ctx, ts)

	totalMessages, err := countMessages(ctx, ts, since, "", "")
	if err != nil {
		return nil, err
	}
	inbound, err := countMessages(ctx, ts, since, "in", "")
	if err != nil {
		return nil, err
	}
	aiReplies, err := countMessages(ctx, ts, since, "out", "ai")
	if err != nil {
		return nil, err
	}
	humanReplies, err := countMessages(ctx, ts, since, "out", "human")
	if err != nil {
		return nil, err
	}
	leads, err := countSince(ctx, ts, "lead", since)
	if err != nil {
		return nil, err
	}
	unread, err := sumUnread(ctx, ts)
	if err != nil {
		return nil, err
	}

	todayInbound, err := countTodayMessages(ctx, ts, reportingTz, "in", "")
	if err != nil {
		return nil, err
	}
	todayAI, err := countTodayMessages(ctx, ts, reportingTz, "out", "ai")
	if err != nil {
		return nil, err
	}

	outWindow, err := countMessages(ctx, ts, since, "out", "")
	if err != nil {
		return nil, err
	}
	outRead, err := countOutboundRead(ctx, ts, since)
	if err != nil {
		return nil, err
	}

	topQ, err := topQuestions(ctx, ts, since, 5)
	if err != nil {
		return nil, err
	}

	aiCovPct := 0
	if inbound > 0 {
		aiCovPct = int(math.Round(float64(aiReplies) / float64(inbound) * 100))
	}
	handoffPct := 0
	if inbound > 0 {
		handoffPct = int(math.Round(float64(humanReplies) / float64(inbound) * 100))
	}
	convEstPct := int(math.Min(100, math.Round(float64(leads)/math.Max(float64(inbound), 1)*100*1.4)))

	todayAICov := 0
	if todayInbound > 0 {
		todayAICov = int(math.Round(float64(todayAI) / float64(todayInbound) * 100))
	} else if todayAI > 0 {
		todayAICov = 100
	}

	var openRate *int
	if outWindow > 0 {
		v := int(math.Round(float64(outRead) / float64(outWindow) * 100))
		openRate = &v
	}

	avgResp := avgFirstResponse(ctx, ts, since)

	return &OverviewResponse{
		WindowDays: days,
		Totals: Totals{
			TotalMessages:       totalMessages,
			InboundMessages:     inbound,
			AIReplies:           aiReplies,
			HumanReplies:        humanReplies,
			LeadsGenerated:      leads,
			UnreadConversations: unread,
		},
		Today: TodayStats{
			Inbound:       todayInbound,
			AIReplies:     todayAI,
			AICoveragePct: todayAICov,
		},
		ReportingTimezone: reportingTz,
		Overview: OverviewMeta{
			OpenRatePct:         openRate,
			AvgFirstResponseSec: avgResp,
		},
		KPIs: KPIs{
			AICoveragePct:         aiCovPct,
			HandoffRatePct:        handoffPct,
			ConversionEstimatePct: convEstPct,
		},
		TopQuestions: topQ,
	}, nil
}

// ─── DB helpers ──────────────────────────────────────────────────────────────

func resolveReportingTimezone(ctx context.Context, ts appdb.TenantScope) string {
	var tz sql.NullString
	_ = ts.QueryRowContext(ctx, `
		SELECT reporting_timezone FROM business_profile
		ORDER BY created_at ASC LIMIT 1`).Scan(&tz)
	if tz.Valid && tz.String != "" {
		return tz.String
	}
	return "Asia/Jakarta"
}

func countMessages(ctx context.Context, ts appdb.TenantScope, since time.Time, direction, author string) (int, error) {
	q := "SELECT COUNT(*) FROM message WHERE created_at >= $1"
	args := []any{since}
	n := 2
	if direction != "" {
		q += fmt.Sprintf(" AND direction = $%d", n)
		args = append(args, direction)
		n++
	}
	if author != "" {
		q += fmt.Sprintf(" AND author = $%d", n)
		args = append(args, author)
	}
	var count int
	err := ts.QueryRowContext(ctx, q, args...).Scan(&count)
	return count, err
}

func countTodayMessages(ctx context.Context, ts appdb.TenantScope, tz, direction, author string) (int, error) {
	q := `SELECT COUNT(*) FROM message
		WHERE direction = $1
		AND created_at >= ((CURRENT_TIMESTAMP AT TIME ZONE $2)::date AT TIME ZONE $2)
		AND created_at < (((CURRENT_TIMESTAMP AT TIME ZONE $3)::date + interval '1 day') AT TIME ZONE $3)`
	args := []any{direction, tz, tz}
	if author != "" {
		q += " AND author = $4"
		args = append(args, author)
	}
	var count int
	err := ts.QueryRowContext(ctx, q, args...).Scan(&count)
	return count, err
}

func countSince(ctx context.Context, ts appdb.TenantScope, table string, since time.Time) (int, error) {
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE created_at >= $1", table)
	var count int
	err := ts.QueryRowContext(ctx, q, since).Scan(&count)
	return count, err
}

func sumUnread(ctx context.Context, ts appdb.TenantScope) (int, error) {
	var sum int
	err := ts.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(unread_count), 0) FROM conversation`).Scan(&sum)
	return sum, err
}

func countOutboundRead(ctx context.Context, ts appdb.TenantScope, since time.Time) (int, error) {
	var count int
	err := ts.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM message
		WHERE direction = 'out' AND status = 'read' AND created_at >= $1`, since).Scan(&count)
	return count, err
}

func topQuestions(ctx context.Context, ts appdb.TenantScope, since time.Time, limit int) ([]TopQuestion, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT LOWER(TRIM(body)) AS question, COUNT(1) AS cnt
		FROM message
		WHERE direction = 'in' AND type = 'text'
		  AND created_at >= $1
		  AND body IS NOT NULL AND char_length(TRIM(body)) >= 4
		GROUP BY LOWER(TRIM(body))
		ORDER BY cnt DESC
		LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []TopQuestion
	for rows.Next() {
		var q TopQuestion
		if err := rows.Scan(&q.Question, &q.Count); err != nil {
			return nil, err
		}
		result = append(result, q)
	}
	if result == nil {
		result = []TopQuestion{}
	}
	return result, rows.Err()
}

func avgFirstResponse(ctx context.Context, ts appdb.TenantScope, since time.Time) *float64 {
	var avg sql.NullFloat64
	_ = ts.QueryRowContext(ctx, `
		SELECT AVG(EXTRACT(EPOCH FROM (fo.first_out - fi.first_in)))::float AS avg_sec
		FROM (
			SELECT conversation_id, MIN(created_at) AS first_in
			FROM message
			WHERE direction = 'in' AND created_at >= $1
			GROUP BY conversation_id
		) fi
		INNER JOIN (
			SELECT conversation_id, MIN(created_at) AS first_out
			FROM message
			WHERE direction = 'out' AND author IN ('ai', 'human') AND created_at >= $1
			GROUP BY conversation_id
		) fo ON fo.conversation_id = fi.conversation_id
		WHERE fo.first_out > fi.first_in`, since).Scan(&avg)
	if avg.Valid {
		return &avg.Float64
	}
	return nil
}
