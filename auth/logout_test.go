package auth

import (
	"context"
	"testing"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/shared/types"
)

func mustSession(t *testing.T, ctx context.Context, d SessionData) string {
	t.Helper()
	s, err := createSession(ctx, d)
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	return s.SessionID
}

func alive(t *testing.T, ctx context.Context, acc, sid string) bool {
	t.Helper()
	s, err := getSession(ctx, acc, sid)
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	return s != nil
}

// Logout revokes only the presented session: impersonation logout keeps the parent admin
// session; admin and client logout revoke their own session.
func TestLogoutRevokesOnlyUsedSession(t *testing.T) {
	ctx := context.Background()
	if !RedisAvailable(ctx) {
		t.Skip("redis not available")
	}
	const adminAcc, clientAcc = "test-logout-admin", "test-logout-client"
	adminSID := mustSession(t, ctx, SessionData{AccountID: adminAcc, Role: roleSuperAdmin, Kind: adminauth.KindAdmin})
	impSID := mustSession(t, ctx, SessionData{AccountID: adminAcc, Role: roleSuperAdmin, Kind: adminauth.KindImpersonation, Impersonating: true})
	clientSID := mustSession(t, ctx, SessionData{AccountID: clientAcc, Kind: adminauth.KindClient})
	defer func() {
		_ = destroySession(ctx, adminAcc, adminSID)
		_ = destroySession(ctx, adminAcc, impSID)
		_ = destroySession(ctx, clientAcc, clientSID)
	}()

	if err := logoutSession(ctx, &types.AuthUser{AccountID: adminAcc, SessionID: impSID, SessionKind: adminauth.KindImpersonation}); err != nil {
		t.Fatalf("impersonation logout: %v", err)
	}
	if alive(t, ctx, adminAcc, impSID) {
		t.Error("impersonation session still alive after its logout")
	}
	if !alive(t, ctx, adminAcc, adminSID) {
		t.Error("parent admin session revoked by impersonation logout")
	}

	if err := logoutSession(ctx, &types.AuthUser{AccountID: adminAcc, SessionID: adminSID, SessionKind: adminauth.KindAdmin}); err != nil {
		t.Fatalf("admin logout: %v", err)
	}
	if alive(t, ctx, adminAcc, adminSID) {
		t.Error("admin session still alive after logout")
	}

	if err := logoutSession(ctx, &types.AuthUser{AccountID: clientAcc, SessionID: clientSID, SessionKind: adminauth.KindClient}); err != nil {
		t.Fatalf("client logout: %v", err)
	}
	if alive(t, ctx, clientAcc, clientSID) {
		t.Error("client session still alive after logout")
	}

	if err := logoutSession(ctx, nil); err == nil {
		t.Error("nil user must be rejected")
	}
}
