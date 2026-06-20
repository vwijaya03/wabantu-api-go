package ai

import "strings"

// mediaTypesWithCaption are inbound WhatsApp types whose caption is stored in message.body.
var mediaTypesWithCaption = map[string]struct{}{
	"image":    {},
	"video":    {},
	"document": {},
}

// inboundTextForAutoReply returns sanitized user text when the inbound message can be
// handled by the text-based auto-reply pipeline (Fase 3a: caption-as-text).
func inboundTextForAutoReply(msgType, body string) (string, bool) {
	msgType = strings.ToLower(strings.TrimSpace(msgType))
	text := SanitizeForPrompt(body)
	if msgType == "text" {
		return text, text != ""
	}
	if _, ok := mediaTypesWithCaption[msgType]; ok && text != "" {
		return text, true
	}
	return "", false
}

func isMediaTypeWithOptionalCaption(msgType string) bool {
	_, ok := mediaTypesWithCaption[strings.ToLower(strings.TrimSpace(msgType))]
	return ok
}
