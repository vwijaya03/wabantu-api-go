package admin

import (
	"context"
	"encoding/json"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/shared/interactionevidence"
	ieadapters "encore.app/wabantu/shared/interactionevidence/adapters"
	"encore.app/wabantu/shared/kbcontext"
	"encore.app/wabantu/shared/triageincident"
)

type ListAITriageIncidentsParams struct {
	TenantID string `query:"tenantId"`
	Channel  string `query:"channel"`
	Status   string `query:"status"`
	Limit    int    `query:"limit"`
}

type ListAITriageIncidentsResponse struct {
	Incidents []triageincident.Incident `json:"incidents"`
}

type GetAITriageIncidentResponse struct {
	Incident triageincident.Incident `json:"incident"`
}

type ConfirmAITriageIncidentParams struct {
	Contract json.RawMessage `json:"contract"`
}

type ConfirmAITriageIncidentResponse struct {
	Incident    triageincident.Incident `json:"incident"`
	BehaviorJob *AITriageBehaviorJob    `json:"behaviorJob,omitempty"`
	HoldReason  string                  `json:"holdReason,omitempty"`
}

type DismissAITriageIncidentParams struct {
	Note string `json:"note,omitempty"`
}

type IngestTriageIncidentParams struct {
	TenantID       string `json:"tenantId"`
	TenantSchema   string `json:"tenantSchema"`
	SourceType     string `json:"sourceType"`
	SourceID       string `json:"sourceId"`
	Channel        string `json:"channel"`
	ConversationID string `json:"conversationId"`
	InboundID      string `json:"inboundId"`
	OutboundID     string `json:"outboundId"`
	UserText       string `json:"userText"`
	ReplyText      string `json:"replyText"`
	Path           string `json:"path"`
	Category       string `json:"category"`
	Lane           string `json:"lane"`
}

// ListAITriageIncidents lists unified incidents (superadmin).
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/incidents tag:super_admin
func ListAITriageIncidents(ctx context.Context, p *ListAITriageIncidentsParams) (*ListAITriageIncidentsResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if p == nil {
		p = &ListAITriageIncidentsParams{}
	}
	if p.Channel != "" && !interactionevidence.ValidChannels[interactionevidence.Channel(p.Channel)] {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "channel tidak valid"}
	}
	maybeReclaimStaleBehaviorJobs(ctx)
	items, err := listIncidents(ctx, p.TenantID, p.Channel, p.Status, p.Limit)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal memuat insiden"}
	}
	return &ListAITriageIncidentsResponse{Incidents: items}, nil
}

// GetAITriageIncident returns one incident.
//
//encore:api auth method=GET path=/api/v1/admin/ai-triage/incidents/:id tag:super_admin
func GetAITriageIncident(ctx context.Context, id string) (*GetAITriageIncidentResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	maybeReclaimStaleBehaviorJobs(ctx)
	inc, err := loadIncident(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return &GetAITriageIncidentResponse{Incident: inc}, nil
}

// ConfirmAITriageIncident confirms the behavior contract. Confirm ≠ fixed.
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/incidents/:id/confirm tag:super_admin
func ConfirmAITriageIncident(ctx context.Context, id string, p *ConfirmAITriageIncidentParams) (*ConfirmAITriageIncidentResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil || len(p.Contract) == 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "contract required"}
	}
	inc, err := loadIncident(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if inc.ReviewStatus == triageincident.ReviewDismissed {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "insiden sudah diabaikan"}
	}
	var contract ai.BehaviorContract
	if err := json.Unmarshal(p.Contract, &contract); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "contract JSON tidak valid"}
	}
	if rebuilt := rebuildContractFromIncident(inc); ai.HasDeterministicInvariant(rebuilt) {
		contract = rebuilt
	}
	if err := ai.ValidateBehaviorContract(contract); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	raw, err := ai.MarshalBehaviorContract(contract)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	inc, err = updateIncidentReview(ctx, inc.ID, triageincident.ReviewConfirmed, raw, user.AccountID)
	if err != nil {
		return nil, err
	}
	resp := &ConfirmAITriageIncidentResponse{Incident: inc}
	if reason := confirmHoldReason(contract); reason != "" {
		if reason == holdNeedCustomerInput {
			_, _ = updateIncidentReview(ctx, inc.ID, triageincident.ReviewNeedsHuman, raw, user.AccountID)
			_ = setIncidentResolution(ctx, inc.ID, triageincident.ResolutionNeedsCust)
		} else {
			_, _ = updateIncidentReview(ctx, inc.ID, triageincident.ReviewNeedsHuman, raw, user.AccountID)
		}
		held, loadErr := loadIncident(ctx, inc.ID)
		if loadErr == nil {
			inc = held
		} else {
			inc.ReviewStatus = triageincident.ReviewNeedsHuman
			if reason == holdNeedCustomerInput {
				inc.ResolutionStatus = triageincident.ResolutionNeedsCust
			}
		}
		resp.Incident = inc
		resp.HoldReason = reason
		return resp, nil
	}
	if !laneFilesExist(contract.Lane) {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "lane Composer fail-closed: file allowlist belum ada"}
	}
	if existingID := strings.TrimSpace(inc.BehaviorJobID); existingID != "" {
		existing, loadErr := loadBehaviorJob(ctx, existingID)
		if loadErr != nil {
			return nil, loadErr
		}
		if canRetryBehaviorJob(existing.Status, existing.AttemptCount) {
			if err := dispatchBehaviorFixWorkflow(ctx, existing.ID, inc.TenantSchema); err != nil {
				return nil, &errs.Error{Code: errs.Unavailable, Message: composerDispatchUnavailableMessage(err)}
			}
			ready, readyErr := loadBehaviorJob(ctx, existing.ID)
			if readyErr != nil {
				return nil, readyErr
			}
			resp.BehaviorJob = &ready
			return resp, nil
		}
		resp.BehaviorJob = &existing
		return resp, nil
	}
	job, err := createBehaviorJobFromIncident(ctx, inc, contract, user.AccountID)
	if err != nil {
		rlog.Warn("create behavior job failed", "incidentId", inc.ID, "err", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal membuat behavior job"}
	}
	_ = setIncidentBehaviorJob(ctx, inc.ID, job.ID)
	inc.BehaviorJobID = job.ID
	resp.Incident = inc
	if err := dispatchBehaviorFixWorkflow(ctx, job.ID, inc.TenantSchema); err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: composerDispatchUnavailableMessage(err)}
	}
	ready, loadErr := loadBehaviorJob(ctx, job.ID)
	if loadErr != nil {
		resp.BehaviorJob = job
		return resp, nil
	}
	resp.BehaviorJob = &ready
	return resp, nil
}

func rebuildContractFromIncident(inc triageincident.Incident) ai.BehaviorContract {
	ev, _ := triageincident.EvidenceFromJSON(inc.Evidence)
	return buildDraftContract(inc.Channel, ev.Path, "", ev.UserText, ev.FinalText)
}

// DismissAITriageIncident ignores an open incident.
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/incidents/:id/dismiss tag:super_admin
func DismissAITriageIncident(ctx context.Context, id string, p *DismissAITriageIncidentParams) (*GetAITriageIncidentResponse, error) {
	user, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	_ = p
	inc, err := updateIncidentReview(ctx, strings.TrimSpace(id), triageincident.ReviewDismissed, nil, user.AccountID)
	if err != nil {
		return nil, err
	}
	return &GetAITriageIncidentResponse{Incident: inc}, nil
}

type DeleteAITriageIncidentsParams struct {
	IDs []string `json:"ids"`
}

type DeleteAITriageIncidentsResponse struct {
	Deleted int `json:"deleted"`
}

// DeleteAITriageIncidents permanently removes incidents (and linked jobs/plans).
//
//encore:api auth method=POST path=/api/v1/admin/ai-triage/incident-deletes tag:super_admin
func DeleteAITriageIncidents(ctx context.Context, p *DeleteAITriageIncidentsParams) (*DeleteAITriageIncidentsResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "ids required"}
	}
	ids, err := parseTriageUUIDList(p.IDs, 50)
	if err != nil {
		return nil, err
	}
	n, err := deleteIncidents(ctx, ids)
	if err != nil {
		rlog.Error("delete incidents failed", "err", err)
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal menghapus insiden"}
	}
	return &DeleteAITriageIncidentsResponse{Deleted: n}, nil
}

// IngestTriageIncident upserts an incident from a report or finding (private).
//
//encore:api private method=POST path=/api/v1/internal/ai-triage/ingest
func IngestTriageIncident(ctx context.Context, p *IngestTriageIncidentParams) error {
	_, err := upsertIncidentFromParams(ctx, p)
	return err
}

func upsertIncidentFromParams(ctx context.Context, p *IngestTriageIncidentParams) (triageincident.Incident, error) {
	if p == nil {
		return triageincident.Incident{}, nil
	}
	channel := strings.TrimSpace(p.Channel)
	if channel == "" {
		channel = string(interactionevidence.ChannelWhatsApp)
	}
	if p.SourceType == triageincident.SourceWebChatNegativeFeedback {
		// Public feedback never dispatches Composer; confirm-first only.
		channel = string(interactionevidence.ChannelWebChat)
	}
	ev := ieadapters.FromIngest(ieadapters.IngestInput{
		Channel:        channel,
		ConversationID: p.ConversationID,
		InboundID:      p.InboundID,
		OutboundID:     p.OutboundID,
		UserText:       p.UserText,
		ReplyText:      p.ReplyText,
		Path:           p.Path,
		DegradedMode:   kbcontext.DegradedNone,
	})
	if !ev.Valid() {
		return triageincident.Incident{}, nil
	}
	raw, _ := json.Marshal(ev)
	c := buildDraftContract(channel, p.Path, p.Category, p.UserText, p.ReplyText)
	lane := strings.TrimSpace(p.Lane)
	if lane == "" {
		lane = c.Lane
	} else {
		c.Lane = lane
	}
	draft, err := ai.MarshalBehaviorContract(c)
	if err != nil {
		draft = nil
	}
	fp := triageincident.Compute(triageincident.FingerprintInput{
		TenantID:    p.TenantID,
		Channel:     channel,
		Lane:        lane,
		FailureKind: strings.TrimSpace(p.Category),
		Path:        p.Path,
	})
	return insertOrLinkIncident(ctx, triageincident.Incident{
		TenantID:        p.TenantID,
		TenantSchema:    p.TenantSchema,
		Channel:         channel,
		Fingerprint:     fp,
		CrossChannelKey: triageincident.CrossChannelKey(triageincident.FingerprintInput{TenantID: p.TenantID, Lane: lane, FailureKind: p.Category, Path: p.Path}),
		Lane:            lane,
		EvidenceVersion: interactionevidence.CaptureVersion,
		Evidence:        raw,
		DraftContract:   draft,
	}, p.SourceType, p.SourceID, channel)
}

func systemExecIncidentStatus(ctx context.Context, id, status string) (triageincident.Incident, error) {
	return updateIncidentReview(ctx, id, status, nil, "")
}

const (
	holdNeedCustomerInput        = "need_customer_input"
	holdContractNotDeterministic = "contract_not_deterministic"
	holdLaneFailClosed           = "lane_fail_closed"
	holdShadowChannel            = "shadow_channel"
)

func confirmHoldReason(c ai.BehaviorContract) string {
	if c.Assertions.NeedCustomerInput {
		return holdNeedCustomerInput
	}
	if !ai.HasDeterministicInvariant(c) {
		return holdContractNotDeterministic
	}
	if isShadowTriageChannel(c.Channel) {
		return holdShadowChannel
	}
	if c.Lane == triageincident.LanePresentation {
		return holdLaneFailClosed
	}
	return ""
}

// isShadowTriageChannel — rollout step 3: intake + contract OK, Composer/repair blocked until chatbot Alpha.
func isShadowTriageChannel(channel string) bool {
	switch interactionevidence.Channel(strings.TrimSpace(channel)) {
	case interactionevidence.ChannelWebChat, interactionevidence.ChannelStorefrontSearch:
		return true
	default:
		return false
	}
}

func laneFilesExist(lane string) bool {
	paths := laneAllowlist(lane)
	if len(paths) == 0 {
		return false
	}
	if lane == triageincident.LaneChatengineGrounding || lane == triageincident.LaneStorefrontSearch || lane == triageincident.LaneStreamingSSE {
		return false // fail-closed until chatbot files exist
	}
	return true
}
