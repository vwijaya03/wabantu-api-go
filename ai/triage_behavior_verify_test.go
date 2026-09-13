package ai

import (
	"context"
	"testing"

	"encore.app/wabantu/shared/interactionevidence"
)

func TestVerifyBehaviorContractRejectsUnknownDeploy(t *testing.T) {
	got := VerifyBehaviorContract(context.Background(), BehaviorVerifyRequest{
		ExpectedRevision: "deadbeefcafebabe",
		Contract:         BehaviorContract{Lane: "buyerflow", Assertions: BehaviorAssertions{WantPath: "order_flow"}},
	})
	if got.Passed || got.DeploymentOK {
		t.Fatalf("unknown deploy must fail: %+v", got)
	}
}

func TestReplayNoSendBuyerflow(t *testing.T) {
	rep := ReplayNoSend(context.Background(), ReplayRequest{
		Contract: BehaviorContract{
			Lane: "buyerflow",
			Assertions: BehaviorAssertions{
				WantPath:      "consulting",
				ReplyContains: []string{"xyz-not-in-reply"},
			},
		},
		Evidence: interactionevidence.TurnEvidence{UserText: "halo"},
	})
	if rep.DeterministicOK {
		t.Fatal("missing substr should fail")
	}
}
