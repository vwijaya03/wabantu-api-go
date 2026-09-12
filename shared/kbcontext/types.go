// Package kbcontext is the channel-agnostic retrieval facade for WhatsApp, web chat,
// storefront search, and triage replay. Callers must not copy KB/catalog loaders.
package kbcontext

import "encore.app/wabantu/shared/retrieval"

// CaptureVersion is the frozen evidence schema for retrieval traces.
const CaptureVersion = 1

// DegradedMode records a correct (non-regression) fallback.
type DegradedMode string

const (
	DegradedNone           DegradedMode = ""
	DegradedFAQOnly        DegradedMode = "faq_only"
	DegradedEmbedQuota     DegradedMode = "embed_quota"
	DegradedPlatformBudget DegradedMode = "platform_budget"
	DegradedKillSwitch     DegradedMode = "kill_switch"
)

// Trace is the ordered retrieval outcome for one query.
type Trace struct {
	Version         int          `json:"version"`
	Query           string       `json:"query,omitempty"`
	Mode            string       `json:"mode,omitempty"`
	KBEntryIDs      []string     `json:"kbEntryIds,omitempty"`
	CatalogIDs      []string     `json:"catalogIds,omitempty"`
	UsedVector      bool         `json:"usedVector,omitempty"`
	LexicalFallback bool         `json:"lexicalFallback,omitempty"`
	FallbackReason  string       `json:"fallbackReason,omitempty"`
	DegradedMode    DegradedMode `json:"degradedMode,omitempty"`
	Hash            string       `json:"hash,omitempty"`
}

// Bundle is the frozen context a reply was grounded on.
type Bundle struct {
	Trace Trace `json:"trace"`
}

// FromRetrieveKBResult projects a retrieval.RetrieveKBResult into a Trace.
func FromRetrieveKBResult(query string, mode retrieval.RetrievalMode, res *retrieval.RetrieveKBResult, degraded DegradedMode) Trace {
	tr := Trace{
		Version:      CaptureVersion,
		Query:        query,
		Mode:         string(mode),
		DegradedMode: degraded,
	}
	if res == nil {
		tr.Hash = HashTrace(tr)
		return tr
	}
	tr.UsedVector = res.UsedVector
	tr.LexicalFallback = res.LexicalFallback
	tr.FallbackReason = string(res.FallbackReason)
	tr.KBEntryIDs = orderedIDs(res.Entries, retrieval.SourceKB)
	tr.CatalogIDs = orderedIDs(res.Entries, retrieval.SourceCatalog)
	tr.Hash = HashTrace(tr)
	return tr
}

func orderedIDs(entries []retrieval.ScoredEntry, src retrieval.Source) []string {
	out := make([]string, 0, len(entries))
	seen := map[string]struct{}{}
	for _, e := range entries {
		if e.Source != src {
			continue
		}
		id := e.EntryID
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
