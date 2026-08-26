package ai

import bf "encore.app/wabantu/internal/buyerflow"

// TriageSimulatorSnapshot freezes tenant profile/catalog/KB for auto-gen regression tests.
type TriageSimulatorSnapshot = bf.TriageSimulatorSnapshot

// SimulatorToSnapshot copies simulator state used during triage analyze.
func SimulatorToSnapshot(sim *ConversationSimulator, tenantSchema string) *TriageSimulatorSnapshot {
	return bf.SimulatorToSnapshot(sim, tenantSchema)
}

// SimulatorFromSnapshot rebuilds a ConversationSimulator from a frozen snapshot.
func SimulatorFromSnapshot(snap *TriageSimulatorSnapshot) (*ConversationSimulator, error) {
	return bf.SimulatorFromSnapshot(snap)
}

// SimulatorFromSnapshotJSON parses embedded JSON from auto-gen regression file.
func SimulatorFromSnapshotJSON(jsonLiteral string) (*ConversationSimulator, error) {
	return bf.SimulatorFromSnapshotJSON(jsonLiteral)
}

// FormatSnapshotGoConst returns a Go string literal for embedding in generated tests.
func FormatSnapshotGoConst(snap *TriageSimulatorSnapshot) (string, error) {
	return bf.FormatSnapshotGoConst(snap)
}
