package codesim

import (
	"context"

	"encore.dev/beta/auth"

	"encore.app/wabantu/shared/types"
)

// optionalAccountID returns the caller account when authenticated, or "" for anonymous.
func optionalAccountID(ctx context.Context) string {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return ""
	}
	return u.AccountID
}
