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
	}); got != holdContractNotDeterministic {
		t.Fatalf("wantPath-only buyerflow: got %q", got)
	}
	if got := confirmHoldReason(ai.BehaviorContract{
		Lane:    triageincident.LaneBuyerflow,
		Channel: "whatsapp",
		Assertions: ai.BehaviorAssertions{
			WantPath:    "order_flow",
			CartExclude: []string{"cadbury"},
		},
	}); got != "" {
		t.Fatalf("buyerflow with cart exclude: got %q", got)
	}
	if got := confirmHoldReason(ai.BehaviorContract{
		Lane:    triageincident.LaneBuyerflow,
		Channel: "web_chat",
		Assertions: ai.BehaviorAssertions{
			WantPath:      "catalog_db",
			ReplyContains: []string{"durian"},
		},
	}); got != holdShadowChannel {
		t.Fatalf("web_chat shadow: got %q", got)
	}
}
