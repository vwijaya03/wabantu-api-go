package admin

import (
	"context"
	"database/sql"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/triagereport"
	"encore.app/wabantu/system"
)

func listTriageReports(ctx context.Context, tenantID, status string, limit int) ([]triagereport.Report, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	status = strings.TrimSpace(status)
	tenantID = strings.TrimSpace(tenantID)

	var rows interface {
		Close()
		Next() bool
		Scan(dest ...any) error
		Err() error
	}
	var err error
	switch {
	case tenantID != "" && status != "":
		rows, err = system.DB.Query(ctx, reportSelectSQL+`
			WHERE r.tenant_id = $1::uuid AND r.status = $2
			ORDER BY r.created_at DESC LIMIT $3`, tenantID, status, limit)
	case tenantID != "":
		rows, err = system.DB.Query(ctx, reportSelectSQL+`
			WHERE r.tenant_id = $1::uuid
			ORDER BY r.created_at DESC LIMIT $2`, tenantID, limit)
	case status != "":
		rows, err = system.DB.Query(ctx, reportSelectSQL+`
			WHERE r.status = $1
			ORDER BY r.created_at DESC LIMIT $2`, status, limit)
	default:
		rows, err = system.DB.Query(ctx, reportSelectSQL+`
			ORDER BY r.created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTriageReportRows(rows)
}

func loadTriageReport(ctx context.Context, id string) (triagereport.Report, error) {
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
	fillTriageReportFields(&r, inboundID, judgeFlagged, judgeCat, judgeReason, reviewBy, reviewNote, reviewedAt, tenantName)
	return r, nil
}

func updateTriageReportReview(ctx context.Context, id, status, reviewNote, reviewedBy string) (triagereport.Report, error) {
	if !triagereport.ValidStatuses[status] || status == triagereport.StatusOpen {
		return triagereport.Report{}, &errs.Error{Code: errs.InvalidArgument, Message: "status harus confirmed atau dismissed"}
	}
	res, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_report
		SET status = $2,
		    review_note = NULLIF($3, ''),
		    reviewed_by = $4::uuid,
		    reviewed_at = now(),
		    updated_at = now()
		WHERE id = $1::uuid AND status = $5`,
		id, status, reviewNote, reviewedBy, triagereport.StatusOpen,
	)
	if err != nil {
		return triagereport.Report{}, err
	}
	n := res.RowsAffected()
	if n == 0 {
		return triagereport.Report{}, &errs.Error{Code: errs.NotFound, Message: "laporan tidak ditemukan atau sudah direview"}
	}
	return loadTriageReport(ctx, id)
}

func updateTriageReportJudge(ctx context.Context, reportID string, flagged bool, category, reason string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_report
		SET judge_flagged = $2,
		    judge_category = NULLIF($3, ''),
		    judge_reason = NULLIF($4, ''),
		    updated_at = now()
		WHERE id = $1::uuid`,
		reportID, flagged, category, reason,
	)
	return err
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
	       COALESCE(t.name, '')
	FROM ai_triage_report r
	LEFT JOIN tenant t ON t.id = r.tenant_id`

func scanTriageReportRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]triagereport.Report, error) {
	out := make([]triagereport.Report, 0)
	for rows.Next() {
		var r triagereport.Report
		var inboundID, judgeCat, judgeReason, reviewBy, reviewNote, tenantName string
		var judgeFlagged sql.NullBool
		var reviewedAt sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.TenantSchema, &r.ConversationID,
			&inboundID, &r.OutboundMessageID,
			&r.UserText, &r.ReplyText, &r.Path,
			&r.Category, &r.ReporterNote, &r.Status,
			&r.ReportedBy, &r.ReporterRole,
			&judgeFlagged, &judgeCat, &judgeReason,
			&reviewBy, &reviewNote, &reviewedAt,
			&r.CreatedAt, &r.UpdatedAt, &tenantName,
		); err != nil {
			return nil, err
		}
		fillTriageReportFields(&r, inboundID, judgeFlagged, judgeCat, judgeReason, reviewBy, reviewNote, reviewedAt, tenantName)
		out = append(out, r)
	}
	return out, rows.Err()
}

func fillTriageReportFields(
	r *triagereport.Report,
	inboundID string,
	judgeFlagged sql.NullBool,
	judgeCat, judgeReason, reviewBy, reviewNote string,
	reviewedAt sql.NullTime,
	tenantName string,
) {
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
}
