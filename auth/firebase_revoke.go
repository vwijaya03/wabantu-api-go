package auth

import (
	"context"
	"sync"

	"encore.dev/rlog"

	"encore.app/wabantu/internal/realtime"
)

// firebaseRevoker is nil (no-op) when FIRESTORE_PROJECT_ID / FIREBASE_SERVICE_ACCOUNT_JSON
// are not set. Tests may replace it.
var firebaseRevoker = sync.OnceValue(realtime.DefaultRevoker)

// revokeFirebaseAsync revokes the uid's Firebase refresh tokens in the background
// (best-effort, 2s cap). Logout/stop-impersonation never wait for or fail on it.
func revokeFirebaseAsync(ctx context.Context, uid string) {
	realtime.RevokeAsync(ctx, firebaseRevoker(), uid, rlog.Warn)
}
