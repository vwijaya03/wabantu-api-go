package ai

import (
	"context"
	"testing"
)

// Regression (incident 21 Oct 2026): the handler used to acquire the inflight key the publisher had
// just set, so every job was skipped. The handler must run the summarizer and release the key.
func TestHandleSummarizeRunsAndReleasesPublisherLock(t *testing.T) {
	// Success only: a permanent failure would also call recordSummarizeFailure, which needs Redis.
	for name, runErr := range map[string]error{"success": nil} {
		t.Run(name, func(t *testing.T) {
			var ran, released []string
			oldRun, oldRel := summarizeRunFn, summarizeReleaseFn
			defer func() { summarizeRunFn, summarizeReleaseFn = oldRun, oldRel }()
			summarizeRunFn = func(_ context.Context, schema, convo string) error {
				ran = append(ran, schema+"/"+convo)
				return runErr
			}
			summarizeReleaseFn = func(_ context.Context, convo string) { released = append(released, convo) }

			if err := handleSummarize(context.Background(), SummarizeRequest{TenantSchema: "t_x", ConversationID: "c1"}); err != nil {
				t.Fatalf("handleSummarize error: %v", err)
			}
			if len(ran) != 1 || ran[0] != "t_x/c1" {
				t.Fatalf("summarizer must run exactly once, ran=%v", ran)
			}
			if len(released) != 1 || released[0] != "c1" {
				t.Fatalf("inflight key must be released once, released=%v", released)
			}
		})
	}
}
