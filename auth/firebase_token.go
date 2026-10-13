package auth

import (
	"net/http"
	"time"

	"encore.app/wabantu/internal/realtime"
)

// FirebaseToken mints a Firebase Auth custom token for the realtime (Firestore) judge UI.
// Claims tenantId/role come ONLY from the server session; the request body is ignored.
// 401 without a session, 403 for other roles or no tenant, 503 when FIRESTORE_PROJECT_ID /
// FIREBASE_SERVICE_ACCOUNT_JSON are not configured. Contract: docs/API_AI_CONFIG.md.
//
//encore:api public raw method=POST path=/api/v1/auth/firebase-token
func FirebaseToken(w http.ResponseWriter, req *http.Request) {
	var sess *realtime.Session
	if u, err := AuthenticateHTTP(req.Context(), req); err == nil && u != nil {
		sess = &realtime.Session{AccountID: u.AccountID, TenantID: u.TenantID, Role: u.Role}
	}
	realtime.ServeFirebaseToken(w, req, sess, realtime.EnvConfig, time.Now)
}
