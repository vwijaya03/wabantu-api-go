package inbox

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/triagereport"
)

type ReportMessageParams struct {
	Category     string `json:"category"`
	ReporterNote string `json:"reporterNote,omitempty"`
}

type ReportMessageResponse struct {
	Report triagereport.Report `json:"report"`
}

type GetMessageReportResponse struct {
	Reported bool   `json:"reported"`
	ReportID string `json:"reportId,omitempty"`
}

// ReportMessage submits a human report for an AI/system outbound reply.
//
//encore:api auth method=POST path=/api/v1/inbox/messages/:id/report
func ReportMessage(ctx context.Context, id string, p *ReportMessageParams) (*ReportMessageResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanAccessInbox() {
		return nil, apperr.Forbidden("akses inbox ditolak")
	}
	if p == nil {
		return nil, apperr.BadRequest("body required")
	}
	category := strings.TrimSpace(p.Category)
	if !triagereport.ValidCategories[category] {
		return nil, apperr.BadRequest("kategori laporan tidak valid")
	}
	note := strings.TrimSpace(p.ReporterNote)
	if len(note) > triagereport.MaxReporterNoteLen {
		return nil, apperr.BadRequest("catatan terlalu panjang (maks 500 karakter)")
	}

	messageID := strings.TrimSpace(id)
	if messageID == "" {
		return nil, apperr.BadRequest("message id required")
	}

	exists, existingID, err := existsReportForOutbound(ctx, messageID)
	if err != nil {
		rlog.Error("check report exists failed", "err", err)
		return nil, apperr.Internal("gagal memeriksa laporan")
	}
	if exists {
		report, loadErr := loadTriageReportByID(ctx, existingID)
		if loadErr != nil {
			rlog.Error("load existing report failed", "err", loadErr, "reportId", existingID)
			return nil, apperr.AlreadyExists("balasan ini sudah dilapor")
		}
		return &ReportMessageResponse{Report: report}, nil
	}

	reporterRole := triagereport.ReporterRoleFromAuth(user.Role)
	count, err := countReportsToday(ctx, user.AccountID)
	if err != nil {
		rlog.Error("count reports today failed", "err", err)
		return nil, apperr.Internal("gagal memeriksa limit laporan")
	}
	limit := triagereport.DailyLimitForRole(reporterRole)
	if count >= limit {
		return nil, &errs.Error{
			Code:    errs.ResourceExhausted,
			Message: "limit laporan harian tercapai — coba lagi besok",
		}
	}

	conn, err := tenantConn(ctx, user)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	msg, err := loadReportableMessage(ctx, conn, messageID)
	if err != nil {
		return nil, err
	}

	inboundID, userText, err := triagereport.ResolveInboundBeforeOutbound(ctx, conn, msg.ConversationID, msg.ID)
	if err == sql.ErrNoRows {
		inboundID, userText = "", ""
	} else if err != nil {
		rlog.Error("resolve inbound for report failed", "err", err)
		return nil, apperr.Internal("gagal menemukan pesan masuk terkait")
	}

	reportID, err := insertTriageReport(ctx, triagereport.InsertParams{
		TenantID:          user.TenantID,
		TenantSchema:      user.TenantSchema,
		ConversationID:    msg.ConversationID,
		InboundID:         inboundID,
		OutboundMessageID: msg.ID,
		UserText:          userText,
		ReplyText:         msg.Body,
		Path:              msg.Path,
		Category:          category,
		ReporterNote:      note,
		ReportedBy:        user.AccountID,
		ReporterRole:      reporterRole,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "idx_ai_triage_report_outbound") {
			if exists, existingID, err2 := existsReportForOutbound(ctx, messageID); err2 == nil && exists {
				if report, loadErr := loadTriageReportByID(ctx, existingID); loadErr == nil {
					return &ReportMessageResponse{Report: report}, nil
				}
			}
			return nil, apperr.AlreadyExists("balasan ini sudah dilapor")
		}
		rlog.Error("insert triage report failed", "err", err)
		return nil, apperr.Internal("gagal menyimpan laporan")
	}

	_ = publishTriageReportJudgeJob(ctx, &TriageReportJudgeJob{
		ReportID:       reportID,
		TenantSchema:   user.TenantSchema,
		ConversationID: msg.ConversationID,
		InboundID:      inboundID,
		UserText:       userText,
		ReplyText:      msg.Body,
		Path:           msg.Path,
		InboundAt:      msg.CreatedAt,
	})

	report, err := loadTriageReportByID(ctx, reportID)
	if err != nil {
		rlog.Error("load triage report after insert failed", "err", err, "reportId", reportID)
		return nil, apperr.Internal("laporan tersimpan tetapi gagal dimuat")
	}
	return &ReportMessageResponse{Report: report}, nil
}

// GetMessageReport checks whether an outbound message was already reported.
//
//encore:api auth method=GET path=/api/v1/inbox/messages/:id/report
func GetMessageReport(ctx context.Context, id string) (*GetMessageReportResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanAccessInbox() {
		return nil, apperr.Forbidden("akses inbox ditolak")
	}
	messageID := strings.TrimSpace(id)
	if messageID == "" {
		return nil, apperr.BadRequest("message id required")
	}
	exists, reportID, err := existsReportForOutbound(ctx, messageID)
	if err != nil {
		return nil, apperr.Internal("gagal memeriksa laporan")
	}
	return &GetMessageReportResponse{Reported: exists, ReportID: reportID}, nil
}

type reportableMessage struct {
	ID             string
	ConversationID string
	Body           string
	Path           string
	CreatedAt      time.Time
}

func loadReportableMessage(ctx context.Context, conn *sql.Conn, messageID string) (reportableMessage, error) {
	var m reportableMessage
	var direction, author string
	var body sql.NullString
	var meta []byte
	var createdAt time.Time
	err := conn.QueryRowContext(ctx, `
		SELECT id::text, conversation_id::text, direction, author,
		       COALESCE(body, ''), metadata, created_at
		FROM message WHERE id = $1::uuid`, messageID,
	).Scan(&m.ID, &m.ConversationID, &direction, &author, &body, &meta, &createdAt)
	if err == sql.ErrNoRows {
		return reportableMessage{}, apperr.NotFound("pesan tidak ditemukan")
	}
	if err != nil {
		return reportableMessage{}, apperr.Internal("gagal memuat pesan")
	}
	if !strings.EqualFold(direction, "out") {
		return reportableMessage{}, apperr.BadRequest("hanya balasan keluar yang bisa dilapor")
	}
	author = strings.ToLower(strings.TrimSpace(author))
	if author != "ai" && author != "system" {
		return reportableMessage{}, apperr.BadRequest("hanya balasan AI/sistem yang bisa dilapor")
	}
	if body.Valid {
		m.Body = strings.TrimSpace(body.String)
	}
	m.Path = parseOutboundPath(meta)
	m.CreatedAt = createdAt
	return m, nil
}
