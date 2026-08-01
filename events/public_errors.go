package events

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	encoreerrs "encore.dev/beta/errs"
	"encore.dev/rlog"

	appErrs "encore.app/wabantu/shared/errs"
)

const (
	errCodePublicUnavailable = "EVT_PUBLIC_UNAVAILABLE"
	errCodeNotFound          = "EVT_NOT_FOUND"
	errCodePublicInternal    = "EVT_PUBLIC_INTERNAL"
)

const (
	msgPublicUnavailable = "Jadwal sementara tidak tersedia. Coba muat ulang sebentar lagi."
	msgPublicNotFound    = "Acara tidak ditemukan"
	msgPublicInternal    = "Terjadi gangguan. Coba lagi nanti."
)

// publicEventErrorDetails is exposed to clients as structured Details (not SQL text).
type publicEventErrorDetails struct {
	ErrorCode string `json:"errorCode"`
}

func (publicEventErrorDetails) ErrDetails() {}

func publicErrText(err error) string {
	if err == nil {
		return ""
	}
	var ee *encoreerrs.Error
	if errors.As(err, &ee) && ee.Message != "" {
		return ee.Message
	}
	return err.Error()
}

// isPublicTransientDBErr reports bad connection / missing relation style failures
// that are worth a single retry and map to HTTP 503.
func isPublicTransientDBErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(publicErrText(err))
	if strings.Contains(msg, "bad connection") {
		return true
	}
	// e.g. pq: relation "evt_event" does not exist
	if strings.Contains(msg, "does not exist") && strings.Contains(msg, "relation") {
		return true
	}
	return false
}

func publicEventError(ctx context.Context, errorCode, userMessage string, code encoreerrs.ErrCode, err error, tenantSlug, eventSlug string) error {
	rlog.Error("public event failed",
		"errorCode", errorCode,
		"tenantSlug", tenantSlug,
		"eventSlug", eventSlug,
		"err", err,
	)
	_ = ctx
	return &encoreerrs.Error{
		Code:    code,
		Message: userMessage,
		Details: publicEventErrorDetails{ErrorCode: errorCode},
	}
}

// classifyPublicEventErr maps public-event failures to safe client errors.
// Validation (InvalidArgument) is passed through unchanged.
func classifyPublicEventErr(ctx context.Context, err error, tenantSlug, eventSlug string) error {
	if err == nil {
		return nil
	}

	var ee *encoreerrs.Error
	if errors.As(err, &ee) {
		switch ee.Code {
		case encoreerrs.InvalidArgument, encoreerrs.FailedPrecondition, encoreerrs.AlreadyExists, encoreerrs.ResourceExhausted:
			return err
		case encoreerrs.NotFound:
			return publicEventError(ctx, errCodeNotFound, msgPublicNotFound, encoreerrs.NotFound, err, tenantSlug, eventSlug)
		case encoreerrs.Unavailable:
			if ee.Message == msgPublicUnavailable {
				return err
			}
			return publicEventError(ctx, errCodePublicUnavailable, msgPublicUnavailable, encoreerrs.Unavailable, err, tenantSlug, eventSlug)
		}
	}

	if errors.Is(err, sql.ErrNoRows) {
		return publicEventError(ctx, errCodeNotFound, msgPublicNotFound, encoreerrs.NotFound, err, tenantSlug, eventSlug)
	}
	if isPublicTransientDBErr(err) {
		return publicEventError(ctx, errCodePublicUnavailable, msgPublicUnavailable, encoreerrs.Unavailable, err, tenantSlug, eventSlug)
	}
	return publicEventError(ctx, errCodePublicInternal, msgPublicInternal, encoreerrs.Internal, err, tenantSlug, eventSlug)
}

// runPublicEvent runs fn, retries once on transient DB errors, then classifies
// any remaining error into a safe public response.
func runPublicEvent[T any](ctx context.Context, tenantSlug, eventSlug string, fn func() (T, error)) (T, error) {
	var zero T
	out, err := fn()
	if err != nil && isPublicTransientDBErr(err) {
		out, err = fn()
	}
	if err != nil {
		return zero, classifyPublicEventErr(ctx, err, tenantSlug, eventSlug)
	}
	return out, nil
}

// publicNotFound returns a NotFound that classifyPublicEventErr will normalize.
func publicNotFound() error {
	return appErrs.NotFound(msgPublicNotFound)
}
