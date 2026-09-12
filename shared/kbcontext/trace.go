package kbcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type traceHashPayload struct {
	Version         int      `json:"v"`
	Query           string   `json:"q"`
	Mode            string   `json:"m"`
	KBEntryIDs      []string `json:"kb"`
	CatalogIDs      []string `json:"cat"`
	UsedVector      bool     `json:"uv"`
	LexicalFallback bool     `json:"lf"`
	FallbackReason  string   `json:"fr"`
	DegradedMode    string   `json:"dm"`
}

// HashTrace is stable for the same ordered IDs + mode (WA/web parity gate).
func HashTrace(tr Trace) string {
	payload := traceHashPayload{
		Version:         tr.Version,
		Query:           strings.TrimSpace(tr.Query),
		Mode:            strings.TrimSpace(tr.Mode),
		KBEntryIDs:      append([]string{}, tr.KBEntryIDs...),
		CatalogIDs:      append([]string{}, tr.CatalogIDs...),
		UsedVector:      tr.UsedVector,
		LexicalFallback: tr.LexicalFallback,
		FallbackReason:  strings.TrimSpace(tr.FallbackReason),
		DegradedMode:    string(tr.DegradedMode),
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SameOrderedHits reports whether two traces retrieved the same KB/catalog IDs in order.
func SameOrderedHits(a, b Trace) bool {
	return equalIDs(a.KBEntryIDs, b.KBEntryIDs) && equalIDs(a.CatalogIDs, b.CatalogIDs)
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
