package publictenant

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	encoreerrs "encore.dev/beta/errs"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

const (
	errCodePublicUnavailable = "PUBLIC_UNAVAILABLE"
	errCodeNotFound          = "PUBLIC_NOT_FOUND"
	errCodePublicInternal    = "PUBLIC_INTERNAL"
	msgPublicUnavailable     = "Layanan sementara tidak tersedia. Coba muat ulang sebentar lagi."
	msgPublicNotFound        = "Toko tidak ditemukan"
	msgPublicInternal        = "Terjadi gangguan. Coba lagi nanti."
)

type publicErrorDetails struct {
	ErrorCode string `json:"errorCode"`
}

func (publicErrorDetails) ErrDetails() {}

func isPublicTransientDBErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "bad connection") {
		return true
	}
	return strings.Contains(msg, "does not exist") && strings.Contains(msg, "relation")
}

func classifyPublicErr(ctx context.Context, err error, tenantSlug string) error {
	if err == nil {
		return nil
	}
	var ee *encoreerrs.Error
	if errors.As(err, &ee) {
		switch ee.Code {
		case encoreerrs.InvalidArgument, encoreerrs.FailedPrecondition, encoreerrs.Unauthenticated, encoreerrs.PermissionDenied:
			return err
		case encoreerrs.NotFound:
			return publicErr(errCodeNotFound, msgPublicNotFound, encoreerrs.NotFound)
		case encoreerrs.Unavailable:
			if ee.Message == msgPublicUnavailable {
				return err
			}
			return publicErr(errCodePublicUnavailable, msgPublicUnavailable, encoreerrs.Unavailable)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return publicErr(errCodeNotFound, msgPublicNotFound, encoreerrs.NotFound)
	}
	if isPublicTransientDBErr(err) {
		return publicErr(errCodePublicUnavailable, msgPublicUnavailable, encoreerrs.Unavailable)
	}
	_ = ctx
	_ = tenantSlug
	return publicErr(errCodePublicInternal, msgPublicInternal, encoreerrs.Internal)
}

func publicErr(code, msg string, c encoreerrs.ErrCode) error {
	return &encoreerrs.Error{Code: c, Message: msg, Details: publicErrorDetails{ErrorCode: code}}
}

// ResolveFunc resolves a public tenant slug to TenantRef.
type ResolveFunc func(ctx context.Context, slug string) (TenantRef, error)

// RunPublicTenant resolves slug, ensures schema access, then runs fn with tenant schema.
func RunPublicTenant[T any](ctx context.Context, tenantSlug string, resolve ResolveFunc, fn func(ref TenantRef) (T, error)) (T, error) {
	var zero T
	ref, err := resolve(ctx, tenantSlug)
	if err != nil {
		return zero, classifyPublicErr(ctx, err, tenantSlug)
	}
	if err := tenant.PrepareTenantAccess(ctx, ref.TenantSchema); err != nil {
		return zero, classifyPublicErr(ctx, err, tenantSlug)
	}
	out, err := fn(ref)
	if err != nil && isPublicTransientDBErr(err) {
		out, err = fn(ref)
	}
	if err != nil {
		return zero, classifyPublicErr(ctx, err, tenantSlug)
	}
	return out, nil
}

// NotFound returns a typed not-found for public handlers.
func NotFound() error {
	return appErrs.NotFound(msgPublicNotFound)
}
