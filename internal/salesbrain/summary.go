package salesbrain

import (
	"strings"
	"time"
)

// Session rule for the rolling conversation summary (see docs/LLM_FIRST_PIPELINE.md).
const (
	// SummarySessionWindow: a summary updated within this window belongs to the current session
	// and is shown as the conversation background. It is also the gap between two buyer messages
	// that starts a new visit (ai.ShouldTriggerSummary uses the same number).
	SummarySessionWindow = 72 * time.Hour
	// SummaryMaxAge: a summary older than this is stale and is not shown at all.
	SummaryMaxAge = 30 * 24 * time.Hour
	// MaxSummaryRunes caps the summary text that reaches the prompt.
	MaxSummaryRunes = 600
)

// SummaryKind says how a stored summary is presented to the model.
type SummaryKind string

const (
	SummaryNone     SummaryKind = ""         // omit the block
	SummaryCurrent  SummaryKind = "current"  // "RINGKASAN PERCAKAPAN"
	SummaryPrevious SummaryKind = "previous" // "KUNJUNGAN SEBELUMNYA (N hari lalu)"
)

// SummaryContext is the decision for one turn: what to show and under which label.
type SummaryContext struct {
	Kind    SummaryKind
	Text    string // trimmed stored summary; redaction and the rune cap happen when it is rendered
	AgeDays int    // whole days since the summary was updated; only meaningful for SummaryPrevious
}

// DecideSummary applies the session rule to a stored summary and the time it was last updated:
//
//	age <= 72h            -> SummaryCurrent
//	72h < age <= 30 days  -> SummaryPrevious (with AgeDays)
//	age > 30 days         -> SummaryNone
//
// An empty summary or a zero updatedAt (no summary row) is SummaryNone. A timestamp in the future
// (clock skew) counts as current.
func DecideSummary(summary string, updatedAt, now time.Time) SummaryContext {
	text := strings.TrimSpace(summary)
	if text == "" || updatedAt.IsZero() {
		return SummaryContext{}
	}
	age := now.Sub(updatedAt)
	switch {
	case age <= SummarySessionWindow:
		return SummaryContext{Kind: SummaryCurrent, Text: text}
	case age <= SummaryMaxAge:
		return SummaryContext{Kind: SummaryPrevious, Text: text, AgeDays: int(age / (24 * time.Hour))}
	default:
		return SummaryContext{}
	}
}
