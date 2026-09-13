package interactionevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type evidenceHashPayload struct {
	Version         int      `json:"v"`
	Channel         string   `json:"c"`
	ThreadRef       string   `json:"t"`
	InboundRef      string   `json:"in"`
	OutboundRef     string   `json:"out"`
	ClientMessageID string   `json:"cid"`
	UserText        string   `json:"u"`
	FinalText       string   `json:"f"`
	Path            string   `json:"p"`
	RetrievalHash   string   `json:"rh"`
	CardIDs         []string `json:"cards"`
	Degraded        string   `json:"dm"`
}

// HashEvidence is used to dedup SSE reconnect / duplicate clientMessageId.
func HashEvidence(e TurnEvidence) string {
	cards := make([]string, 0, len(e.ProductCards))
	for _, c := range e.ProductCards {
		if id := strings.TrimSpace(c.CatalogItemID); id != "" {
			cards = append(cards, id)
		}
	}
	payload := evidenceHashPayload{
		Version:         e.Version,
		Channel:         string(e.Channel),
		ThreadRef:       strings.TrimSpace(e.ThreadRef),
		InboundRef:      strings.TrimSpace(e.InboundRef),
		OutboundRef:     strings.TrimSpace(e.OutboundRef),
		ClientMessageID: strings.TrimSpace(e.ClientMessageID),
		UserText:        strings.TrimSpace(e.UserText),
		FinalText:       strings.TrimSpace(e.FinalText),
		Path:            strings.TrimSpace(e.Path),
		RetrievalHash:   strings.TrimSpace(e.Retrieval.Hash),
		CardIDs:         cards,
		Degraded:        string(e.DegradedMode),
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// TokenizeRef returns a non-reversible handle for cart/visitor/status tokens.
func TokenizeRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("wabantu-evidence-ref:" + raw))
	return hex.EncodeToString(sum[:8])
}
