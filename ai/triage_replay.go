package ai

import (
	"context"
	"encoding/json"
	"fmt"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/triageassert"
	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

// ReplayMode is a cold-path, no-send evaluation.
type ReplayMode string

const (
	ReplayBuyerflow ReplayMode = "buyerflow"
	ReplayGrounded  ReplayMode = "grounded"
)

// ReplayRequest is a no-send simulator/retrieval replay.
type ReplayRequest struct {
	Mode     ReplayMode
	Contract BehaviorContract
	Evidence interactionevidence.TurnEvidence
	Snapshot json.RawMessage
}

// ReplayResult is deterministic assertion output plus optional semantic note.
type ReplayResult struct {
	DeterministicOK bool     `json:"deterministicOk"`
	Failures        []string `json:"failures,omitempty"`
	SemanticNote    string   `json:"semanticNote,omitempty"`
}

// ReplayNoSend never publishes ai-jobs, never sends WhatsApp/SSE, never persists orders.
func ReplayNoSend(ctx context.Context, req ReplayRequest) ReplayResult {
	_ = ctx
	var failures []string
	switch req.Contract.Lane {
	case "buyerflow", "draft_order":
		sim, err := simulatorFromReplaySnapshot(req.Snapshot)
		if err != nil {
			return ReplayResult{Failures: []string{err.Error()}}
		}
		out := sim.Turn(req.Evidence.UserText)
		spec := triageassert.CommerceSpec{
			WantPath:     req.Contract.Assertions.WantPath,
			WantStep:     req.Contract.Assertions.WantStep,
			WantSubstr:   req.Contract.Assertions.ReplyContains,
			WantNot:      req.Contract.Assertions.ReplyExcludes,
			ExcludeNames: req.Contract.Assertions.CartExclude,
		}
		for _, it := range req.Contract.Assertions.CartInclude {
			spec.Include = append(spec.Include, triageassert.CartItem{
				CatalogItemID: it.CatalogItemID,
				ExternalCode:  it.ExternalCode,
				NameContains:  it.NameContains,
				Qty:           it.Qty,
			})
		}
		if err := triageassert.CheckTurn(out, spec); err != nil {
			failures = append(failures, err.Error())
		}
	default:
		if err := triageassert.CheckPayload(req.Evidence, triageassert.PayloadSpec{
			RequiredFacts:    req.Contract.Assertions.RequiredFacts,
			ForbiddenClaims:  req.Contract.Assertions.ForbiddenClaims,
			CardIDs:          req.Contract.Assertions.ProductCardIDs,
			WantQuickReplies: req.Contract.Assertions.QuickReplies,
			DegradedMode:     req.Contract.DegradedMode,
		}); err != nil {
			failures = append(failures, err.Error())
		}
		if err := triageassert.CheckTrace(req.Evidence.Retrieval, triageassert.RetrievalSpec{
			KBEntryIDs:   req.Contract.Assertions.RetrievalKBIDs,
			CatalogIDs:   req.Contract.Assertions.RetrievalCatalogIDs,
			Mode:         req.Contract.Assertions.RetrievalMode,
			DegradedMode: kbcontext.DegradedMode(req.Contract.DegradedMode),
		}); err != nil {
			failures = append(failures, err.Error())
		}
		if len(req.Contract.Assertions.SearchOrderedIDs) > 0 {
			failures = append(failures, "storefront_search replay skipped: lane fail-closed until catalog search exists")
		}
	}
	return ReplayResult{DeterministicOK: len(failures) == 0, Failures: failures}
}

func simulatorFromReplaySnapshot(raw json.RawMessage) (*bf.Simulator, error) {
	if len(raw) == 0 {
		return bf.NewOmahSimulator(), nil
	}
	sim, err := bf.SimulatorFromSnapshotJSON(string(raw))
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return sim, nil
}
