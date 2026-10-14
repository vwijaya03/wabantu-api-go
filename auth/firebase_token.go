package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/internal/realtime"
	"encore.app/wabantu/shared/types"
)

// FirebaseToken mints a Firebase Auth custom token for the realtime (Firestore) judge UI.
// Claims tenantId/role come ONLY from the server session; the request body is ignored.
// 401 without a session, 403 for other roles or no tenant, 503 when FIRESTORE_PROJECT_ID /
// FIREBASE_SERVICE_ACCOUNT_JSON are not configured. Contract: docs/API_AI_CONFIG.md.
//
//encore:api public raw method=POST path=/api/v1/auth/firebase-token
func FirebaseToken(w http.ResponseWriter, req *http.Request) {
	u, err := AuthenticateHTTP(req.Context(), req)
	if err != nil {
		u = nil
	}
	serveFirebaseTokenFor(w, req, u, realtime.EnvConfig)
}

// serveFirebaseTokenFor runs the session-kind gate BEFORE anything else in the realtime path
// (role/tenant checks and the Firebase config check), so impersonation gets 403 and admin 401
// even where Firebase is not configured (never 503 / 200 empty for those kinds).
func serveFirebaseTokenFor(w http.ResponseWriter, req *http.Request, u *types.AuthUser, cfgFn func() (*realtime.Config, error)) {
	var sess *realtime.Session
	if u != nil {
		if !firebaseTokenKindOK(w, u.SessionKind) {
			return
		}
		sess = &realtime.Session{AccountID: u.AccountID, TenantID: u.TenantID, Role: u.Role}
	}
	realtime.ServeFirebaseToken(w, req, sess, cfgFn, time.Now)
}

// firebaseTokenKindOK enforces the session kind in the handler (HandlerEnforcedRaw: Encore drops
// middleware errors on raw endpoints → 200 empty). Writes 401 (admin) / 403 (impersonation).
func firebaseTokenKindOK(w http.ResponseWriter, kind string) bool {
	switch err := adminauth.RawHandlerAccess("auth.FirebaseToken", kind); {
	case err == nil:
		return true
	case errors.Is(err, adminauth.ErrImpersonationForbidden):
		writeJSONErrCode(w, http.StatusForbidden, "permission_denied", err.Error())
	default:
		writeJSONErrCode(w, http.StatusUnauthorized, "unauthenticated", "not authenticated")
	}
	return false
}

func writeJSONErrCode(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}
