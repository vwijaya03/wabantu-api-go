package admin

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
		t.Fatal("in-flight job must not retry until reclaimed")
	}
	if !canRetryBehaviorJob(behaviorStatusPRReady, 1) {
		t.Fatal("pr_ready under max should retry")
	}
}

func TestBehaviorJobStuck_noCallback(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 50, 0, 0, time.UTC)
	updated := now.Add(-4 * time.Minute)
	if !behaviorJobStuck(behaviorStatusFixRunning, "", updated, now) {
		t.Fatal("fix_running without Actions URL after 3m is stuck")
	}
	if behaviorJobStuck(behaviorStatusFixRunning, "", now.Add(-time.Minute), now) {
		t.Fatal("fresh dispatch is not stuck")
	}
	if behaviorJobStuck(behaviorStatusFailed, "", updated, now) {
		t.Fatal("failed is not stuck")
	}
	if !behaviorJobStuck(behaviorStatusPlanning, "", updated, now) {
		t.Fatal("planning without callback after 3m is stuck")
	}
}

func TestBehaviorJobStuck_withCallback(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 50, 0, 0, time.UTC)
	url := "https://github.com/vwijaya03/wabantu-api-go/actions/runs/1"
	if behaviorJobStuck(behaviorStatusFixRunning, url, now.Add(-10*time.Minute), now) {
		t.Fatal("Composer GHA can run ~45m")
	}
	if !behaviorJobStuck(behaviorStatusFixRunning, url, now.Add(-51*time.Minute), now) {
		t.Fatal("fix_running with URL after 50m is stuck")
	}
}

func TestComposerDispatchUnavailableMessage(t *testing.T) {
	msg := composerDispatchUnavailableMessage(fmt.Errorf("github workflow_dispatch 404: belum terdaftar"))
	if !strings.Contains(msg, "GitHub Actions") {
		t.Fatalf("got %s", msg)
	}
}

func TestBehaviorJobRetryBlockedMessage(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 50, 0, 0, time.UTC)
	job := AITriageBehaviorJob{
		Status:       behaviorStatusFixRunning,
		AttemptCount: 0,
		UpdatedAt:    now.Add(-time.Minute),
	}
	msg := behaviorJobRetryBlockedMessage(job, now)
	if !strings.Contains(msg, "masih berjalan") {
		t.Fatalf("fresh in-flight should say masih berjalan, got %s", msg)
	}
	job.UpdatedAt = now.Add(-4 * time.Minute)
	msg = behaviorJobRetryBlockedMessage(job, now)
	if msg != "" {
		t.Fatalf("stuck job should reclaim then retry, got %s", msg)
	}
	job.Status = behaviorStatusFailed
	job.AttemptCount = behaviorMaxAttempts
	msg = behaviorJobRetryBlockedMessage(job, now)
	if !strings.Contains(msg, "batas percobaan") {
		t.Fatalf("max attempts: %s", msg)
	}
}
