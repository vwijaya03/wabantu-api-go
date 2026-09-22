package adapters

import (
	"strings"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

// IngestInput is the minimal cross-channel payload for triage incident intake.
type IngestInput struct {
	Channel        string
	ConversationID string
	InboundID      string
	OutboundID     string
	UserText       string
	ReplyText      string
	Path           string
	DegradedMode   kbcontext.DegradedMode
}

// FromIngest picks the channel adapter. Unknown channels fall back to WhatsApp shape.
func FromIngest(in IngestInput) interactionevidence.TurnEvidence {
	ch := interactionevidence.Channel(strings.TrimSpace(in.Channel))
	if ch == "" {
		ch = interactionevidence.ChannelWhatsApp
	}
	switch ch {
	case interactionevidence.ChannelWebChat:
		return FromWebChat(WebChatInput{
			SessionID:  in.ConversationID,
			InboundID:  in.InboundID,
			OutboundID: in.OutboundID,
			UserText:   in.UserText,
			FinalText:  in.ReplyText,
			Path:       in.Path,
			Author:     "ai",
			DegradedMode: in.DegradedMode,
		})
	case interactionevidence.ChannelStorefrontSearch:
		return FromStorefrontSearch(StorefrontSearchInput{
			TenantID: in.ConversationID,
			Query:    in.UserText,
			Retrieval: kbcontext.Trace{Version: kbcontext.CaptureVersion},
			DegradedMode: in.DegradedMode,
		})
	default:
		return FromWhatsApp(WhatsAppInput{
			ConversationID: in.ConversationID,
			InboundID:      in.InboundID,
			OutboundID:     in.OutboundID,
			UserText:       in.UserText,
			FinalText:      in.ReplyText,
			Path:           in.Path,
			Author:         "ai",
			DegradedMode:   in.DegradedMode,
		})
	}
}
