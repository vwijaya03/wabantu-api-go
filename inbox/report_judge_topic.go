package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/pubsub"
)

// TriageReportJudgeJob queues async Haiku judge for a human report.
type TriageReportJudgeJob struct {
	ReportID     string    `json:"reportId"`
	TenantSchema string    `json:"tenantSchema"`
	ConversationID string  `json:"conversationId"`
	InboundID    string    `json:"inboundId,omitempty"`
	UserText     string    `json:"userText,omitempty"`
	ReplyText    string    `json:"replyText,omitempty"`
	Path         string    `json:"path,omitempty"`
	InboundAt    time.Time `json:"inboundAt"`
}

// TriageReportJudgeTopic runs LLM judge on reported turns (handler in admin service).
var TriageReportJudgeTopic = pubsub.NewTopic[*TriageReportJudgeJob]("ai-triage-report-judge", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

func publishTriageReportJudgeJob(ctx context.Context, job *TriageReportJudgeJob) error {
	if job == nil || strings.TrimSpace(job.ReportID) == "" || strings.TrimSpace(job.TenantSchema) == "" {
		return fmt.Errorf("invalid triage report judge job")
	}
	_, err := TriageReportJudgeTopic.Publish(ctx, job)
	return err
}

type aiReplyMeta struct {
	Path string `json:"path"`
}

func parseOutboundPath(metadata []byte) string {
	if len(metadata) == 0 {
		return ""
	}
	var meta aiReplyMeta
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Path)
}
