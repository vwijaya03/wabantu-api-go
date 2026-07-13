package inbox

import (
	"context"
	"database/sql"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/triagereport"
	"encore.app/wabantu/system"
)

func countReportsToday(ctx context.Context, reportedBy string) (int, error) {
	var n int
	err := system.DB.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM ai_triage_report
		WHERE reported_by = $1::uuid
		  AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC')`,
		reportedBy,
	).Scan(&n)
	return n, err
}

func existsReportForOutbound(ctx context.Context, outboundMessageID string) (bool, string, error) {
	var id string
	err := system.DB.QueryRow(ctx, `
		SELECT id::text FROM ai_triage_report
		WHERE outbound_message_id = $1::uuid`,
		outboundMessageID,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, id, nil
}

func insertTriageReport(ctx context.Context, p triagereport.InsertParams) (string, error) {
	var id string
	var inbound any
	if strings.TrimSpace(p.InboundID) != "" {
		inbound = p.InboundID
	}
	err := system.DB.QueryRow(ctx, `
		INSERT INTO ai_triage_report (
			tenant_id, tenant_schema, conversation_id, inbound_id, outbound_message_id,
			user_text, reply_text, path, category, reporter_note,
			status, reported_by, reporter_role
		) VALUES (
			$1::uuid, $2, $3::uuid, $4::uuid, $5::uuid,
			NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, NULLIF($10, ''),
			$11, $12::uuid, $13
		)
		RETURNING id::text`,
		p.TenantID, p.TenantSchema, p.ConversationID, inbound, p.OutboundMessageID,
		p.UserText, p.ReplyText, p.Path, p.Category, p.ReporterNote,
		triagereport.StatusOpen, p.ReportedBy, p.ReporterRole,
	).Scan(&id)
	return id, err
}

func loadTriageReportByID(ctx context.Context, id string) (triagereport.Report, error) {
	return scanTriageReportRow(ctx, id)
}

func scanTriageReportRow(ctx context.Context, id string) (triagereport.Report, error) {
	var r triagereport.Report
	var inboundID, judgeCat, judgeReason, reviewBy, reviewNote, tenantName string
	var judgeFlagged sql.NullBool
	var reviewedAt sql.NullTime
	err := system.DB.QueryRow(ctx, reportSelectSQL+` WHERE r.id = $1::uuid`, id).Scan(
		&r.ID, &r.TenantID, &r.TenantSchema, &r.ConversationID,
		&inboundID, &r.OutboundMessageID,
		&r.UserText, &r.ReplyText, &r.Path,
		&r.Category, &r.ReporterNote, &r.Status,
		&r.ReportedBy, &r.ReporterRole,
		&judgeFlagged, &judgeCat, &judgeReason,
		&reviewBy, &reviewNote, &reviewedAt,
		&r.CreatedAt, &r.UpdatedAt, &tenantName,
	)
	if err == sql.ErrNoRows {
		return triagereport.Report{}, &errs.Error{Code: errs.NotFound, Message: "laporan tidak ditemukan"}
	}
	if err != nil {
		return triagereport.Report{}, err
	}
	r.InboundID = inboundID
	if judgeFlagged.Valid {
		v := judgeFlagged.Bool
		r.JudgeFlagged = &v
	}
	r.JudgeCategory = judgeCat
	r.JudgeReason = judgeReason
	r.ReviewedBy = reviewBy
	r.ReviewNote = reviewNote
	if reviewedAt.Valid {
		t := reviewedAt.Time
		r.ReviewedAt = &t
	}
	r.TenantName = tenantName
	return r, nil
}

const reportSelectSQL = `
	SELECT r.id::text, r.tenant_id::text, r.tenant_schema, r.conversation_id::text,
	       COALESCE(r.inbound_id::text, ''), r.outbound_message_id::text,
	       COALESCE(r.user_text, ''), COALESCE(r.reply_text, ''), COALESCE(r.path, ''),
	       r.category, COALESCE(r.reporter_note, ''), r.status,
	       r.reported_by::text, r.reporter_role,
	       r.judge_flagged, COALESCE(r.judge_category, ''), COALESCE(r.judge_reason, ''),
	       COALESCE(r.reviewed_by::text, ''), COALESCE(r.review_note, ''),
	       r.reviewed_at, r.created_at, r.updated_at,
	       COALESCE(tc.company_name, '')
	FROM ai_triage_report r
	LEFT JOIN tenant_company tc ON tc.tenant_id = r.tenant_id`
