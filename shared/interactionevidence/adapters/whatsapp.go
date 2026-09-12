package adapters

import (
	"strings"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

// WhatsAppInput is the live WA turn after the assistant message is persisted.
type WhatsAppInput struct {
	ConversationID string
	InboundID      string
	OutboundID     string
	InboundReplyTo string
	UserText       string
	FinalText      string
	Path           string
	Model          string
	Author         string
	Retrieval      kbcontext.Trace
	Commerce       *interactionevidence.CommerceSnapshot
	PriorTurns     []interactionevidence.PriorTurn
	DegradedMode   kbcontext.DegradedMode
	Aborted        bool
}

// FromWhatsApp builds TurnEvidence for channel=whatsapp. Delta/SSE is never used.
func FromWhatsApp(in WhatsAppInput) interactionevidence.TurnEvidence {
	ev := interactionevidence.TurnEvidence{
		Version:        interactionevidence.CaptureVersion,
		Channel:        interactionevidence.ChannelWhatsApp,
		ThreadRef:      strings.TrimSpace(in.ConversationID),
		InboundRef:     strings.TrimSpace(in.InboundID),
		OutboundRef:    strings.TrimSpace(in.OutboundID),
		InboundReplyTo: strings.TrimSpace(in.InboundReplyTo),
		UserText:       in.UserText,
		FinalText:      in.FinalText,
		Path:           strings.TrimSpace(in.Path),
		Model:          strings.TrimSpace(in.Model),
		Author:         strings.TrimSpace(in.Author),
		Retrieval:      in.Retrieval,
		Commerce:       in.Commerce,
		PriorTurns:     in.PriorTurns,
		DegradedMode:   in.DegradedMode,
		Aborted:        in.Aborted,
		IsStaff:        strings.EqualFold(in.Author, "staff"),
	}
	if ev.InboundReplyTo == "" {
		ev.InboundReplyTo = ev.InboundRef
	}
	ev.InteractionRef = interactionevidence.TokenizeRef(string(ev.Channel) + ":" + ev.ThreadRef + ":" + ev.InboundRef)
	ev.EvidenceHash = interactionevidence.HashEvidence(ev)
	ev.KnowledgeHash = in.Retrieval.Hash
	return ev
}
