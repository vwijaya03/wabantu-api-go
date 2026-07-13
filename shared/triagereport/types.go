package triagereport

import "time"

// Report is one human-submitted AI reply report.
type Report struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenantId"`
	TenantSchema      string     `json:"tenantSchema"`
	ConversationID    string     `json:"conversationId"`
	InboundID         string     `json:"inboundId,omitempty"`
	OutboundMessageID string     `json:"outboundMessageId"`
	UserText          string     `json:"userText,omitempty"`
	ReplyText         string     `json:"replyText,omitempty"`
	Path              string     `json:"path,omitempty"`
	Category          string     `json:"category"`
	ReporterNote      string     `json:"reporterNote,omitempty"`
	Status            string     `json:"status"`
	ReportedBy        string     `json:"reportedBy"`
	ReporterRole      string     `json:"reporterRole"`
	JudgeFlagged      *bool      `json:"judgeFlagged,omitempty"`
	JudgeCategory     string     `json:"judgeCategory,omitempty"`
	JudgeReason       string     `json:"judgeReason,omitempty"`
	ReviewedBy        string     `json:"reviewedBy,omitempty"`
	ReviewNote        string     `json:"reviewNote,omitempty"`
	ReviewedAt        *time.Time `json:"reviewedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	TenantName        string     `json:"tenantName,omitempty"`
}

type InsertParams struct {
	TenantID          string
	TenantSchema      string
	ConversationID    string
	InboundID         string
	OutboundMessageID string
	UserText          string
	ReplyText         string
	Path              string
	Category          string
	ReporterNote      string
	ReportedBy        string
	ReporterRole      string
}
