package interactionevidence

import "encore.app/wabantu/shared/kbcontext"

// CaptureVersion of the TurnEvidence envelope.
const CaptureVersion = 1

// Channel is a first-class interaction surface.
type Channel string

const (
	ChannelWhatsApp         Channel = "whatsapp"
	ChannelWebChat          Channel = "web_chat"
	ChannelStorefrontSearch Channel = "storefront_search"
)

// ValidChannels is the closed set stored on incidents.
var ValidChannels = map[Channel]bool{
	ChannelWhatsApp:         true,
	ChannelWebChat:          true,
	ChannelStorefrontSearch: true,
}

// ProductCard is a DB-authoritative catalog projection (never model-provided prices).
type ProductCard struct {
	CatalogItemID     string  `json:"catalogItemId"`
	Slug              string  `json:"slug,omitempty"`
	Name              string  `json:"name,omitempty"`
	Price             float64 `json:"price,omitempty"`
	ImageURL          string  `json:"imageUrl,omitempty"`
	StorefrontVisible bool    `json:"storefrontVisible,omitempty"`
}

// CommerceSnapshot is a bounded cart/order view (no cart/status tokens).
type CommerceSnapshot struct {
	OrderID        string         `json:"orderId,omitempty"`
	OrderRef       string         `json:"orderRef,omitempty"`
	Status         string         `json:"status,omitempty"`
	PaymentStatus  string         `json:"paymentStatus,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	ContactID      string         `json:"contactId,omitempty"`
	WebSessionID   string         `json:"webSessionId,omitempty"`
	Items          []CommerceItem `json:"items,omitempty"`
	CartPersisted  bool           `json:"cartPersisted,omitempty"`
}

// CommerceItem is one line in a frozen commerce snapshot.
type CommerceItem struct {
	CatalogItemID string  `json:"catalogItemId,omitempty"`
	ExternalCode  string  `json:"externalCode,omitempty"`
	Name          string  `json:"name,omitempty"`
	Qty           float64 `json:"qty"`
	UnitPrice     float64 `json:"unitPrice,omitempty"`
}

// PriorTurn is a bounded previous user/assistant pair (no PII tokens).
type PriorTurn struct {
	InboundRef  string `json:"inboundRef,omitempty"`
	OutboundRef string `json:"outboundRef,omitempty"`
	UserText    string `json:"userText,omitempty"`
	ReplyText   string `json:"replyText,omitempty"`
	Path        string `json:"path,omitempty"`
}

// TurnEvidence is the channel-neutral envelope consumed by self-healing triage.
type TurnEvidence struct {
	Version         int                    `json:"version"`
	Channel         Channel                `json:"channel"`
	ThreadRef       string                 `json:"threadRef"`
	InboundRef      string                 `json:"inboundRef,omitempty"`
	OutboundRef     string                 `json:"outboundRef,omitempty"`
	ClientMessageID string                 `json:"clientMessageId,omitempty"`
	InboundReplyTo  string                 `json:"inboundReplyTo,omitempty"`
	UserText        string                 `json:"userText,omitempty"`
	FinalText       string                 `json:"finalText,omitempty"`
	Path            string                 `json:"path,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Author          string                 `json:"author,omitempty"`
	Retrieval       kbcontext.Trace        `json:"retrieval"`
	ProductCards    []ProductCard          `json:"productCards,omitempty"`
	QuickReplies    []string               `json:"quickReplies,omitempty"`
	Commerce        *CommerceSnapshot      `json:"commerce,omitempty"`
	PriorTurns      []PriorTurn            `json:"priorTurns,omitempty"`
	DegradedMode    kbcontext.DegradedMode `json:"degradedMode,omitempty"`
	KnowledgeHash   string                 `json:"knowledgeHash,omitempty"`
	EvidenceHash    string                 `json:"evidenceHash,omitempty"`
	Aborted         bool                   `json:"aborted,omitempty"`
	IsStaff         bool                   `json:"isStaff,omitempty"`
	InteractionRef  string                 `json:"interactionRef,omitempty"`
}

// Valid reports whether the envelope can be stored as incident evidence.
func (e TurnEvidence) Valid() bool {
	if !ValidChannels[e.Channel] {
		return false
	}
	if e.ThreadRef == "" {
		return false
	}
	if e.Aborted {
		return false
	}
	if e.IsStaff {
		return false
	}
	return true
}
