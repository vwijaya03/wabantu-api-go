package webhook

import (
	"context"
	"os"

	"encore.dev/rlog"

	"encore.app/wabantu/ai"
)

// enqueueInboundAI publishes to ai-jobs, or runs inline when INLINE_INBOUND_AI=1
// (Cloud Run lomba: Pub/Sub push JWT validation can fail while webhook still works).
func enqueueInboundAI(ctx context.Context, job *ai.InboundAIJob) {
	if os.Getenv("INLINE_INBOUND_AI") == "1" {
		runCtx := context.WithoutCancel(ctx)
		go func() {
			if err := ai.ProcessInboundAIJob(runCtx, job); err != nil {
				rlog.Error("inline inbound AI failed",
					"err", err,
					"conversationId", job.ConversationID,
					"inboundId", job.InboundMessageID,
				)
			}
		}()
		return
	}
	publishJobWithRetry(ctx, "ai-inbound", func(ctx context.Context) error {
		return ai.PublishInboundJob(ctx, job)
	})
}
