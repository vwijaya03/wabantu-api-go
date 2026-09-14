package admin

import (
	"testing"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/shared/triageincident"
)

func TestConfirmHoldReason(t *testing.T) {
	if got := confirmHoldReason(ai.BehaviorContract{
		Lane:       triageincident.LaneBuyerflow,
		Assertions: ai.BehaviorAssertions{NeedCustomerInput: true, WantPath: "order_flow"},
	}); got != holdNeedCustomerInput {
		t.Fatalf("ambiguous SKU: got %q", got)
	}
	if got := confirmHoldReason(ai.BehaviorContract{
		Lane: triageincident.LaneGroundedContent,
	}); got != holdContractNotDeterministic {
		t.Fatalf("empty contract: got %q", got)
	}
	if got := confirmHoldReason(ai.BehaviorContract{
		Lane:       triageincident.LanePresentation,
		Assertions: ai.BehaviorAssertions{ReplyContains: []string{"x"}},
	}); got != holdLaneFailClosed {
		t.Fatalf("presentation: got %q", got)
	}
	if got := confirmHoldReason(ai.BehaviorContract{
		Lane:       triageincident.LaneBuyerflow,
		Assertions: ai.BehaviorAssertions{WantPath: "order_flow"},
	}); got != "" {
		t.Fatalf("composer-ready: got %q", got)
	}
}
