package reqctx

import (
	"context"
	"time"
)

const (
	DefaultHandlerTimeout = 30 * time.Second
	LongHandlerTimeout    = 2 * time.Minute
	DBQueryTimeout        = 15 * time.Second
)

// WithTimeout wraps the request context with a deadline when parent has none or is longer.
func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		d = DefaultHandlerTimeout
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < d {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
