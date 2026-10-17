package ai

import (
	"fmt"
	"strings"

	"encore.app/wabantu/shared/strutil"
	"github.com/anthropics/anthropic-sdk-go"
)

const (
	anthropicReplyMaxBytes = 1200
	historyTurnLimit       = 6
)

// SalesReplyRequest is the structured payload for Anthropic sales completions.
type SalesReplyRequest struct {
	System   string
	Business string
	KB       string
	Summary  string
	History  []HistoryMessage
	UserText string
	TaskHint string // optional extra instruction appended to static system block
}

func buildSalesSystemBlocks(req SalesReplyRequest) []anthropic.TextBlockParam {
	system := strings.TrimSpace(req.System)
	if hint := strings.TrimSpace(req.TaskHint); hint != "" {
		if system != "" {
			system += "\n"
		}
		system += hint
	}

	blocks := make([]anthropic.TextBlockParam, 0, 4)
	if system != "" {
		blocks = append(blocks, anthropic.TextBlockParam{Text: system})
	}
	if business := strings.TrimSpace(req.Business); business != "" {
		blocks = append(blocks, anthropic.TextBlockParam{
			Text:         "Konteks bisnis dan katalog:\n" + business,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		})
	}
	if kb := strings.TrimSpace(req.KB); kb != "" {
		blocks = append(blocks, anthropic.TextBlockParam{Text: kb})
	}
	if summary := strings.TrimSpace(req.Summary); summary != "" {
		blocks = append(blocks, anthropic.TextBlockParam{
			Text: "Ringkasan percakapan sebelumnya:\n" + summary,
		})
	}
	return blocks
}

// HistoryMessagesToAnthropic converts stored chat history into Anthropic message turns.
func HistoryMessagesToAnthropic(messages []HistoryMessage, maxTurns int) []anthropic.MessageParam {
	if maxTurns <= 0 {
		maxTurns = historyTurnLimit
	}
	start := 0
	if len(messages) > maxTurns {
		start = len(messages) - maxTurns
	}

	var out []anthropic.MessageParam
	for _, m := range messages[start:] {
		body := strings.TrimSpace(m.Body)
		if body == "" {
			body = fmt.Sprintf("[%s]", m.Type)
		}
		switch m.Author {
		case "contact":
			out = appendAnthropicTurn(out, anthropic.MessageParamRoleUser, body)
		case "ai", "human":
			out = appendAnthropicTurn(out, anthropic.MessageParamRoleAssistant, body)
		default:
			out = appendAnthropicTurn(out, anthropic.MessageParamRoleUser, "Sistem: "+body)
		}
	}
	return out
}

func appendAnthropicTurn(msgs []anthropic.MessageParam, role anthropic.MessageParamRole, text string) []anthropic.MessageParam {
	text = strings.TrimSpace(text)
	if text == "" {
		return msgs
	}
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == role {
		// Merge consecutive same-role turns to satisfy Anthropic alternation rules.
		prev := msgs[len(msgs)-1]
		merged := text
		if len(prev.Content) > 0 && prev.Content[0].OfText != nil {
			if prevText := strings.TrimSpace(prev.Content[0].OfText.Text); prevText != "" {
				merged = prevText + "\n" + text
			}
		}
		if role == anthropic.MessageParamRoleAssistant {
			msgs[len(msgs)-1] = anthropic.NewAssistantMessage(anthropic.NewTextBlock(merged))
		} else {
			msgs[len(msgs)-1] = anthropic.NewUserMessage(anthropic.NewTextBlock(merged))
		}
		return msgs
	}
	if role == anthropic.MessageParamRoleAssistant {
		return append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(text)))
	}
	return append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))
}

// dropTrailingCurrentInbound removes the current inbound message from the end of the history.
// The message that triggered the reply is already stored, so it is the last history entry, and
// appending UserText on top showed the model the same text twice. It drops the last element when it
// is a contact message whose trimmed body equals the trimmed userText; for a coalesced burst
// (UserText is the trailing contact bodies joined by newlines) it drops that whole run. Anything
// else, including a differing text or a non-contact last message, is returned unchanged.
func dropTrailingCurrentInbound(history []HistoryMessage, userText string) []HistoryMessage {
	want := strings.TrimSpace(userText)
	if want == "" || len(history) == 0 {
		return history
	}
	joined := ""
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		body := strings.TrimSpace(m.Body)
		if m.Author != "contact" || body == "" {
			break
		}
		if joined == "" {
			joined = body
		} else {
			joined = body + "\n" + joined
		}
		if joined == want {
			return history[:i]
		}
		if len(joined) >= len(want) {
			break
		}
	}
	return history
}

func buildSalesMessages(req SalesReplyRequest) []anthropic.MessageParam {
	userText := strings.TrimSpace(req.UserText)
	history := req.History
	if userText != "" {
		// Before the turn window is applied, so the window keeps real history only.
		history = dropTrailingCurrentInbound(history, userText)
	}
	messages := HistoryMessagesToAnthropic(history, historyTurnLimit)
	if userText == "" {
		return messages
	}
	return appendAnthropicTurn(messages, anthropic.MessageParamRoleUser, userText)
}

func usageFromResponse(resp *anthropic.Message) CompletionUsage {
	return CompletionUsage{
		InputTokens:              int(resp.Usage.InputTokens),
		OutputTokens:             int(resp.Usage.OutputTokens),
		CacheReadInputTokens:     int(resp.Usage.CacheReadInputTokens),
		CacheCreationInputTokens: int(resp.Usage.CacheCreationInputTokens),
	}
}

func addUsage(dst *CompletionUsage, src CompletionUsage) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadInputTokens += src.CacheReadInputTokens
	dst.CacheCreationInputTokens += src.CacheCreationInputTokens
}

func finalizeAnthropicReply(text string, stopReason anthropic.StopReason) string {
	out := strings.TrimSpace(text)
	if out == "" {
		return out
	}
	if stopReason == anthropic.StopReasonMaxTokens {
		out = strutil.TruncateUTF8Ellipsis(out, anthropicReplyMaxBytes-20)
		if !strings.HasSuffix(out, "…") {
			out += " …"
		}
		out += " (balasan dipersingkat — boleh lanjut tanya ya kak)"
	} else if len(out) > anthropicReplyMaxBytes {
		out = strutil.TruncateUTF8(out, anthropicReplyMaxBytes)
	}
	return out
}
