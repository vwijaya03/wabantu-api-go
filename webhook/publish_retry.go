package webhook

import (
	"context"
	"time"

	"encore.dev/rlog"
)

const publishJobMaxAttempts = 3

func publishJobWithRetry(ctx context.Context, label string, publish func(context.Context) error) {
	var lastErr error
	for attempt := 1; attempt <= publishJobMaxAttempts; attempt++ {
		if err := publish(ctx); err == nil {
			return
		} else {
			lastErr = err
			if attempt < publishJobMaxAttempts {
				time.Sleep(time.Duration(50*attempt) * time.Millisecond)
			}
		}
	}
	rlog.Error("publish job failed after retries",
		"label", label,
		"attempts", publishJobMaxAttempts,
		"err", lastErr,
	)
}
