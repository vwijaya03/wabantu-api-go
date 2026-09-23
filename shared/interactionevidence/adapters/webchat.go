package adapters

import (
	"strings"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

// WebChatInput is the final grounded web chat turn after assistant message persist.
// Delta SSE chunks are never evidence — only the assembled final message.
type WebChatInput struct {
	SessionID       string
	InboundID       string
	OutboundID      string
	ClientMessageID string
	UserText        string
	FinalText       string
	Path            string
	Model           string
	Author          string
	Retrieval       kbcontext.Trace
	ProductCards    []interactionevidence.ProductCard
	QuickReplies    []string
	PriorTurns      []interactionevidence.PriorTurn
	Commerce        *interactionevidence.CommerceSnapshot
	DegradedMode    kbcontext.DegradedMode
	Aborted         bool
	IsStaff         bool
}

// FromWebChat builds TurnEvidence for channel=web_chat.
func FromWebChat(in WebChatInput) interactionevidence.TurnEvidence {
	author := strings.TrimSpace(in.Author)
	if author == "" {
		author = "ai"
	}
	ev := interactionevidence.TurnEvidence{
		Version:         interactionevidence.CaptureVersion,
		Channel:         interactionevidence.ChannelWebChat,
		ThreadRef:       strings.TrimSpace(in.SessionID),
		InboundRef:      strings.TrimSpace(in.InboundID),
		OutboundRef:     strings.TrimSpace(in.OutboundID),
		ClientMessageID: strings.TrimSpace(in.ClientMessageID),
		UserText:        in.UserText,
		FinalText:       in.FinalText,
		Path:            strings.TrimSpace(in.Path),
		Model:           strings.TrimSpace(in.Model),
		Author:          author,
		Retrieval:       in.Retrieval,
		ProductCards:    in.ProductCards,
		QuickReplies:    in.QuickReplies,
		PriorTurns:      in.PriorTurns,
		Commerce:        in.Commerce,
		DegradedMode:    in.DegradedMode,
		Aborted:         in.Aborted,
		IsStaff:         in.IsStaff || strings.EqualFold(author, "staff"),
	}
	if ev.InboundRef != "" {
		ev.InboundReplyTo = ev.InboundRef
	}
	ev.InteractionRef = interactionevidence.TokenizeRef(string(ev.Channel) + ":" + ev.ThreadRef + ":" + ev.InboundRef + ":" + ev.ClientMessageID)
	ev.EvidenceHash = interactionevidence.HashEvidence(ev)
	ev.KnowledgeHash = in.Retrieval.Hash
	return ev
}
