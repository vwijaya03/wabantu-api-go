package chatengine

import (
	"context"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/kbcontext"
)

// Input is one visitor turn for web chat.
type Input struct {
	TenantID     string
	TenantSchema string
	SessionID    string
	UserText     string
	Enabled      bool
	Welcome      string
	PersonaName  string
	Degraded     kbcontext.DegradedMode
}

// Output is the grounded assistant reply for one turn.
type Output struct {
	Body         string
	Path         string
	Retrieval    kbcontext.Trace
	DegradedMode kbcontext.DegradedMode
	TokensUsed   int
}

// Engine processes web chat messages using shared retrieval + FAQ fallback.
type Engine struct {
	KB *KBProvider
}

// ProcessMessage returns a deterministic FAQ/greeting reply (LLM wiring in Epic 1b).
func (e *Engine) ProcessMessage(ctx context.Context, ts appdb.TenantScope, in Input) (Output, error) {
	out := Output{Path: PathGreeting, DegradedMode: in.Degraded}
	if !in.Enabled {
		out.Path = PathDisabled
		out.Body = "Chat sedang tidak aktif."
		return out, nil
	}
	text := strings.TrimSpace(in.UserText)
	if text == "" {
		out.Body = strings.TrimSpace(in.Welcome)
		if out.Body == "" {
			out.Body = "Halo! Ada yang bisa kami bantu?"
		}
		return out, nil
	}

	kb, err := loadKB(ctx, ts, 40)
	if err != nil {
		return out, err
	}

	if e.KB != nil {
		bundle, err := e.KB.Retrieve(ctx, kbcontext.Query{
			TenantID: in.TenantID, TenantSchema: in.TenantSchema, Text: text,
		})
		if err == nil {
			out.Retrieval = bundle.Trace
		}
	}

	if reply, ok := bestFAQReply(text, kb); ok {
		out.Path = PathFAQ
		out.Body = reply
		return out, nil
	}

	name, _ := loadBusinessName(ctx, ts)
	persona := strings.TrimSpace(in.PersonaName)
	if persona == "" {
		persona = "Asisten " + name
	}
	out.Path = PathGreeting
	out.Body = persona + " siap membantu. Untuk pertanyaan spesifik, tim kami akan membalas segera."
	return out, nil
}
