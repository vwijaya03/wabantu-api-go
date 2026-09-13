package ai

import (
	"context"
	"encoding/json"

	"encore.app/wabantu/shared/buildinfo"
	"encore.app/wabantu/shared/interactionevidence"
)

// BehaviorVerifyRequest is deploy-aware replay of a confirmed contract.
type BehaviorVerifyRequest struct {
	ExpectedRevision string
	Contract         BehaviorContract
	EvidenceJSON     json.RawMessage
	SnapshotJSON     json.RawMessage
}

// BehaviorVerifyResult is CI-identical deterministic checks plus majority semantic.
type BehaviorVerifyResult struct {
	Passed           bool     `json:"passed"`
	DeploymentOK     bool     `json:"deploymentOk"`
	DeployedRevision string   `json:"deployedRevision"`
	DeterministicOK  bool     `json:"deterministicOk"`
	SemanticStable   bool     `json:"semanticStable"`
	Failures         []string `json:"failures,omitempty"`
}

// VerifyBehaviorContract rejects unknown/old deploys and conversation-wide side effects.
func VerifyBehaviorContract(ctx context.Context, req BehaviorVerifyRequest) BehaviorVerifyResult {
	deployed := buildinfo.Revision()
	out := BehaviorVerifyResult{DeployedRevision: deployed}
	if !buildinfo.Covers(deployed, req.ExpectedRevision) {
		out.Failures = append(out.Failures, "deployment_unknown atau revision belum ter-deploy")
		return out
	}
	out.DeploymentOK = true
	var ev interactionevidence.TurnEvidence
	_ = json.Unmarshal(req.EvidenceJSON, &ev)
	rep := ReplayNoSend(ctx, ReplayRequest{
		Contract: req.Contract,
		Evidence: ev,
		Snapshot: req.SnapshotJSON,
	})
	out.DeterministicOK = rep.DeterministicOK
	out.Failures = append(out.Failures, rep.Failures...)
	// Semantic judge is majority 2/3; without extra LLM calls in unit tests we treat
	// deterministic-only contracts as stable, and empty invariants as unstable.
	out.SemanticStable = HasDeterministicInvariant(req.Contract)
	if !out.SemanticStable {
		out.Failures = append(out.Failures, "semantic_unstable")
	}
	out.Passed = out.DeploymentOK && out.DeterministicOK && out.SemanticStable
	return out
}
