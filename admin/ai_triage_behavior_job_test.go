package admin

import (
	"fmt"
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

func TestWrapWorkflowDispatchError_NotFound(t *testing.T) {
	err := wrapWorkflowDispatchError("ai-triage-behavior-fix.yml", "master", 404, `{"message":"Not Found"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ai-triage-behavior-fix.yml") {
		t.Fatalf("filename missing: %s", msg)
	}
	if !strings.Contains(msg, "master") {
		t.Fatalf("ref missing: %s", msg)
	}
	if !strings.Contains(msg, "belum terdaftar") {
		t.Fatalf("operator hint missing: %s", msg)
	}
}

func TestWrapWorkflowDispatchError_OtherStatus(t *testing.T) {
	err := wrapWorkflowDispatchError("ai-triage-behavior-fix.yml", "master", 422, `{"message":"Unprocessable"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Fatalf("got %s", err.Error())
	}
	if strings.Contains(err.Error(), "belum terdaftar") {
		t.Fatalf("404 hint on non-404: %s", err.Error())
	}
}

func TestCanRetryBehaviorJob(t *testing.T) {
	if !canRetryBehaviorJob(behaviorStatusFailed, 0) {
		t.Fatal("failed job with 0 attempts should retry")
	}
	if !canRetryBehaviorJob(behaviorStatusFailed, 1) {
		t.Fatal("failed job under max attempts should retry")
	}
	if canRetryBehaviorJob(behaviorStatusFailed, behaviorMaxAttempts) {
		t.Fatal("max attempts must block retry")
	}
	if canRetryBehaviorJob(behaviorStatusFixRunning, 0) {
		t.Fatal("in-flight job must not retry")
	}
	if !canRetryBehaviorJob(behaviorStatusPRReady, 1) {
		t.Fatal("pr_ready under max should retry")
	}
}

func TestComposerDispatchUnavailableMessage(t *testing.T) {
	msg := composerDispatchUnavailableMessage(fmt.Errorf("github workflow_dispatch 404: belum terdaftar"))
	if !strings.Contains(msg, "GitHub Actions") {
		t.Fatalf("got %s", msg)
	}
}
