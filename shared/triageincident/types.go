package triageincident

import (
	"encoding/json"
	"time"

	"encore.app/wabantu/shared/interactionevidence"
)

const (
	SourceHumanReport             = "human_report"
	SourceLLMFinding              = "llm_finding"
	SourceWebChatNegativeFeedback = "web_chat_negative_feedback"
	SourceStorefrontSearch        = "storefront_search"
)

const (
	ReviewOpen       = "open"
	ReviewConfirmed  = "confirmed"
	ReviewDismissed  = "dismissed"
	ReviewNeedsHuman = "needs_human_input"
)

const (
	ResolutionNone        = "none"
	ResolutionFixed       = "fixed"
	ResolutionBlocked     = "blocked"
	ResolutionNeedsCust   = "needs_customer_input"
	ResolutionRepairReady = "repair_ready"
)

const (
	LaneBuyerflow           = "buyerflow"
	LaneGroundedContent     = "grounded_content"
	LaneRetrieval           = "retrieval"
	LaneChatengineGrounding = "chatengine_grounding"
	LaneStorefrontSearch    = "storefront_search"
	LaneStreamingSSE        = "streaming_sse"
	LanePresentation        = "presentation"
	LaneTenantData          = "tenant_data"
	LaneDraftOrder          = "draft_order"
	LaneMixed               = "mixed"
)

// ValidLanes is the closed set for behavior contracts.
var ValidLanes = map[string]bool{
	LaneBuyerflow:           true,
	LaneGroundedContent:     true,
	LaneRetrieval:           true,
	LaneChatengineGrounding: true,
	LaneStorefrontSearch:    true,
	LaneStreamingSSE:        true,
	LanePresentation:        true,
	LaneTenantData:          true,
	LaneDraftOrder:          true,
	LaneMixed:               true,
}

// Incident is the unified self-healing case (system DB).
type Incident struct {
	ID                   string          `json:"id"`
	TenantID             string          `json:"tenantId"`
	TenantSchema         string          `json:"tenantSchema"`
	Channel              string          `json:"channel"`
	Fingerprint          string          `json:"fingerprint"`
	CrossChannelKey      string          `json:"crossChannelKey,omitempty"`
	ReviewStatus         string          `json:"reviewStatus"`
	ResolutionStatus     string          `json:"resolutionStatus"`
	Lane                 string          `json:"lane,omitempty"`
	DegradedMode         string          `json:"degradedMode,omitempty"`
	EvidenceVersion      int             `json:"evidenceVersion"`
	Evidence             json.RawMessage `json:"evidence,omitempty"`
	DraftContract        json.RawMessage `json:"draftContract,omitempty"`
	ConfirmedContract    json.RawMessage `json:"confirmedContract,omitempty"`
	BehaviorJobID        string          `json:"behaviorJobId,omitempty"`
	BehaviorJobStatus    string          `json:"behaviorJobStatus,omitempty"`
	BehaviorJobError     string          `json:"behaviorJobError,omitempty"`
	BehaviorJobRunURL    string          `json:"behaviorJobRunUrl,omitempty"`
	BehaviorJobUpdatedAt *time.Time      `json:"behaviorJobUpdatedAt,omitempty"`
	RepairPlanID         string          `json:"repairPlanId,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
	Sources              []Source        `json:"sources,omitempty"`
}

// Source is one intake row linked to an incident.
type Source struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incidentId"`
	SourceType string    `json:"sourceType"`
	SourceID   string    `json:"sourceId"`
	Channel    string    `json:"channel"`
	CreatedAt  time.Time `json:"createdAt"`
}

// EvidenceFromJSON unmarshals frozen TurnEvidence.
func EvidenceFromJSON(raw json.RawMessage) (interactionevidence.TurnEvidence, error) {
	var ev interactionevidence.TurnEvidence
	if len(raw) == 0 {
		return ev, nil
	}
	err := json.Unmarshal(raw, &ev)
	return ev, err
}
