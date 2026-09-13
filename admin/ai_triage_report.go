package admin

import (
	"context"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/triageincident"
	"encore.app/wabantu/shared/triagereport"
)

type ListAITriageReportsParams struct {
	TenantID string `query:"tenantId"`
	Status   string `query:"status"`
	Limit    int    `query:"limit"`
}

type ListAITriageReportsResponse struct {
	Reports []triagereport.Report `json:"reports"`
}

type GetAITriageReportResponse struct {
	Report triagereport.Report `json:"report"`
}

type UpdateAITriageReportParams struct {
	Status     string `json:"status"`
	ReviewNote string `json:"reviewNote,omitempty"`
}

type UpdateAITriageReportResponse struct {
	Report triagereport.Report `json:"report"`
}

// ListAITriageReports lists human-submitted AI reply reports (superadmin only).
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/reports tag:super_admin
func ListAITriageReports(ctx context.Context, p *ListAITriageReportsParams) (*ListAITriageReportsResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if p == nil {
		p = &ListAITriageReportsParams{}
	}
	status := strings.TrimSpace(p.Status)
	if status != "" && !triagereport.ValidStatuses[status] {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "status tidak valid"}
	}
	reports, err := listTriageReports(ctx, p.TenantID, status, p.Limit)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal memuat laporan"}
	}
	return &ListAITriageReportsResponse{Reports: reports}, nil
}

// GetAITriageReport returns one report by id (superadmin only).
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/reports/:id tag:super_admin
func GetAITriageReport(ctx context.Context, id string) (*GetAITriageReportResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	report, err := loadTriageReport(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return &GetAITriageReportResponse{Report: report}, nil
}

// UpdateAITriageReport confirms or dismisses an open report (superadmin only).
//
//encore:api auth method=PATCH path=/api/v1/admin/ai-triage/reports/:id tag:super_admin
func UpdateAITriageReport(ctx context.Context, id string, p *UpdateAITriageReportParams) (*UpdateAITriageReportResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil || strings.TrimSpace(p.Status) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "status required"}
	}
	report, err := updateTriageReportReview(ctx, strings.TrimSpace(id), strings.TrimSpace(p.Status), strings.TrimSpace(p.ReviewNote), user.AccountID)
	if err != nil {
		return nil, err
	}
	if report.Status == triagereport.StatusConfirmed {
		_ = ingestIncidentFromReport(ctx, report)
	}
	return &UpdateAITriageReportResponse{Report: report}, nil
}

type OpenAITriageIncidentFromReportResponse struct {
	Incident triageincident.Incident `json:"incident"`
}

// OpenAITriageIncidentFromReport upserts an incident from a human report so tab
// Insiden shows THIS inbound/outbound pair (does not run the routing loop).
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/reports/:id/incident tag:super_admin
func OpenAITriageIncidentFromReport(ctx context.Context, id string) (*OpenAITriageIncidentFromReportResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	rep, err := loadTriageReport(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if err := ingestIncidentFromReport(ctx, rep); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal membuka insiden dari laporan"}
	}
	inc, err := loadIncidentBySource(ctx, triageincident.SourceHumanReport, rep.ID)
	if err != nil {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "laporan tidak bisa jadi insiden (conversation kosong atau evidence tidak valid)"}
	}
	return &OpenAITriageIncidentFromReportResponse{Incident: inc}, nil
}

func ingestIncidentFromReport(ctx context.Context, rep triagereport.Report) error {
	return IngestTriageIncident(ctx, &IngestTriageIncidentParams{
		TenantID:       rep.TenantID,
		TenantSchema:   rep.TenantSchema,
		SourceType:     triageincident.SourceHumanReport,
		SourceID:       rep.ID,
		Channel:        "whatsapp",
		ConversationID: rep.ConversationID,
		InboundID:      rep.InboundID,
		OutboundID:     rep.OutboundMessageID,
		UserText:       rep.UserText,
		ReplyText:      rep.ReplyText,
		Path:           rep.Path,
		Category:       rep.Category,
	})
}
