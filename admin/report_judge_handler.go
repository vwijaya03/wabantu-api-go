package admin

import (
	"context"
	"strings"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/inbox"
)

var _ = pubsub.NewSubscription(inbox.TriageReportJudgeTopic, "ai-triage-report-judge-handler", pubsub.SubscriptionConfig[*inbox.TriageReportJudgeJob]{
	Handler:     handleTriageReportJudgeJob,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 2},
})

func handleTriageReportJudgeJob(ctx context.Context, job *inbox.TriageReportJudgeJob) error {
	if job == nil || strings.TrimSpace(job.ReportID) == "" || strings.TrimSpace(job.TenantSchema) == "" {
		return nil
	}
	turn := ai.AITriageTurn{
		ConversationID: job.ConversationID,
		InboundID:      job.InboundID,
		UserText:       job.UserText,
		ReplyText:      job.ReplyText,
		Path:           job.Path,
		InboundAt:      job.InboundAt,
	}
	result, err := ai.JudgeReportTurn(ctx, job.TenantSchema, turn)
	if err != nil {
		rlog.Warn("triage report judge failed", "reportId", job.ReportID, "err", err)
		return err
	}
	if err := updateTriageReportJudge(ctx, job.ReportID, result.Flagged, result.Category, result.Reason); err != nil {
		rlog.Warn("triage report judge persist failed", "reportId", job.ReportID, "err", err)
		return err
	}
	return nil
}
