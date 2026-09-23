package adapters

import (
	"strings"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

// StorefrontSearchInput is one semantic search request on storefront ?q=.
type StorefrontSearchInput struct {
	TenantID        string
	Query           string
	OrderedCatalogIDs []string
	Retrieval       kbcontext.Trace
	DegradedMode    kbcontext.DegradedMode
}

// FromStorefrontSearch builds TurnEvidence for channel=storefront_search.
func FromStorefrontSearch(in StorefrontSearchInput) interactionevidence.TurnEvidence {
	query := strings.TrimSpace(in.Query)
	ev := interactionevidence.TurnEvidence{
		Version:      interactionevidence.CaptureVersion,
		Channel:      interactionevidence.ChannelStorefrontSearch,
		ThreadRef:    strings.TrimSpace(in.TenantID),
		UserText:     query,
		Path:         "storefront_search",
		Retrieval:    in.Retrieval,
		DegradedMode: in.DegradedMode,
	}
	if len(in.OrderedCatalogIDs) > 0 {
		cards := make([]interactionevidence.ProductCard, 0, len(in.OrderedCatalogIDs))
		for _, id := range in.OrderedCatalogIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			cards = append(cards, interactionevidence.ProductCard{
				CatalogItemID:     id,
				StorefrontVisible: true,
			})
		}
		ev.ProductCards = cards
	}
	ev.InteractionRef = interactionevidence.TokenizeRef(string(ev.Channel) + ":" + ev.ThreadRef + ":" + query)
	ev.EvidenceHash = interactionevidence.HashEvidence(ev)
	ev.KnowledgeHash = in.Retrieval.Hash
	return ev
}
