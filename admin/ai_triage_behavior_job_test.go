package admin

import (
	"strings"
	"testing"

	"encore.app/wabantu/shared/triageincident"
)

func TestLaneAllowlistBuyerflow(t *testing.T) {
	got := laneAllowlist(triageincident.LaneBuyerflow)
	if len(got) == 0 {
		t.Fatal("buyerflow allowlist empty")
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "internal/buyerflow/") {
		t.Fatalf("missing buyerflow: %v", got)
	}
}

func TestLaneFilesExistFailClosedForChatengine(t *testing.T) {
	if laneFilesExist(triageincident.LaneChatengineGrounding) {
		t.Fatal("chatengine lane must be fail-closed until files exist")
	}
	if laneFilesExist(triageincident.LaneStorefrontSearch) {
		t.Fatal("storefront_search lane must be fail-closed")
	}
	if !laneFilesExist(triageincident.LaneBuyerflow) {
		t.Fatal("buyerflow should be dispatchable")
	}
}

func TestTriageJobVerifyNoLongerImpliesReportsResolvedAlways(t *testing.T) {
	if !triageJobCanVerify(triageJobStatusPRReady) {
		t.Fatal("legacy verify still allowed for routing jobs")
	}
}
