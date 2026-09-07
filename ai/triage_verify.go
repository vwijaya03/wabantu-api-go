package ai

import (
	"context"
	"fmt"
	"strings"
)

// VerifyNoteDeployedCode is shown in UI after a verify run.
const VerifyNoteDeployedCode = "Verifikasi memakai kode Encore yang sudah di-deploy di environment ini, bukan GitHub master. Tunggu deploy setelah merge."

// VerifyGoldenResult is simulator-vs-golden (not WhatsApp history).
type VerifyGoldenResult struct {
	TurnsChecked int                       `json:"turnsChecked"`
	Failures     []TriageRegressionFailure `json:"failures,omitempty"`
	AllPassed    bool                      `json:"allPassed"`
}

// VerifyGoldenCases replays priorTurns+input on a fresh simulator per case.
func VerifyGoldenCases(simFactory func() *ConversationSimulator, mismatches []TriageMismatch) *VerifyGoldenResult {
	result := &VerifyGoldenResult{
		Failures: make([]TriageRegressionFailure, 0),
	}
	if simFactory == nil {
		return result
	}
	for _, m := range mismatches {
		if m.Skipped || strings.TrimSpace(m.UserText) == "" {
			continue
		}
		want, untrusted := SuggestWantPath(m)
		if untrusted || want == "" {
			continue
		}
		sim := simFactory()
		if sim == nil {
			result.Failures = append(result.Failures, TriageRegressionFailure{
				CaseName: regressionCaseName(m.InboundID, result.TurnsChecked),
				WantPath: want,
				GotPath:  "",
			})
			continue
		}
		for _, prior := range m.PriorTurns {
			sim.Turn(prior)
		}
		out := sim.Turn(m.UserText)
		result.TurnsChecked++
		if out.Path != want {
			result.Failures = append(result.Failures, TriageRegressionFailure{
				CaseName:     regressionCaseName(m.InboundID, result.TurnsChecked-1),
				GotPath:      out.Path,
				WantPath:     want,
				ReplyPreview: previewText(out.Reply, 80),
			})
		}
	}
	result.AllPassed = result.TurnsChecked > 0 && len(result.Failures) == 0
	return result
}

// NewVerifySimFactory rebuilds the job snapshot; falls back to live tenant catalog.
func NewVerifySimFactory(ctx context.Context, tenantSchema string, snap *TriageSimulatorSnapshot) (factory func() *ConversationSimulator, usedLive bool, err error) {
	if snap != nil {
		return func() *ConversationSimulator {
			sim, snapErr := SimulatorFromSnapshot(snap)
			if snapErr != nil {
				return nil
			}
			return sim
		}, false, nil
	}
	tenantSchema = strings.TrimSpace(tenantSchema)
	if tenantSchema == "" {
		return nil, false, fmt.Errorf("tenantSchema required for live catalog verify")
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return nil, false, err
	}
	base, err := BuildSimulatorFromTenant(ctx, ts)
	if err != nil {
		return nil, false, err
	}
	frozen := SimulatorToSnapshot(base, tenantSchema)
	return func() *ConversationSimulator {
		sim, snapErr := SimulatorFromSnapshot(frozen)
		if snapErr != nil {
			return nil
		}
		return sim
	}, true, nil
}

// ApplyVerifyResult stores verify outcome on the frozen analysis JSON.
func ApplyVerifyResult(analysis *AnalyzeConversationResult, verify *VerifyGoldenResult, usedLive bool) {
	if analysis == nil || verify == nil {
		return
	}
	analysis.VerifyFailures = verify.Failures
	analysis.VerifyNote = VerifyNoteDeployedCode
	analysis.VerifyUsedLiveCatalog = usedLive
	passed := verify.AllPassed
	analysis.VerifyPassed = &passed
}
