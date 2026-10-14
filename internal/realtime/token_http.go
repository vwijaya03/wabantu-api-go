package realtime

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Session is the server-side auth session (never the request body).
type Session struct {
	AccountID string
	TenantID  string
	Role      string
}

type FirebaseTokenResponse struct {
	Token     string `json:"token"`
	TenantID  string `json:"tenantId"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expiresAt"`
	ProjectID string `json:"projectId"`
}

var allowedTokenRoles = map[string]bool{"owner": true, "staff": true, "super_admin": true}

// ServeFirebaseToken implements POST /api/v1/auth/firebase-token. The request body is
// ignored entirely: tenantId/role claims come only from sess.
func ServeFirebaseToken(w http.ResponseWriter, _ *http.Request, sess *Session, cfgFn func() (*Config, error), now func() time.Time) {
	if sess == nil || strings.TrimSpace(sess.AccountID) == "" {
		writeJSONErr(w, http.StatusUnauthorized, "unauthenticated", "not authenticated")
		return
	}
	if !allowedTokenRoles[sess.Role] {
		writeJSONErr(w, http.StatusForbidden, "permission_denied", "role tidak diizinkan")
		return
	}
	if strings.TrimSpace(sess.TenantID) == "" {
		writeJSONErr(w, http.StatusForbidden, "permission_denied", "sesi tidak punya tenant (superadmin: pilih tenant dulu)")
		return
	}
	cfg, err := cfgFn()
	if err != nil || cfg == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "unavailable", "realtime belum dikonfigurasi")
		return
	}
	t := now()
	tok, err := MintCustomToken(cfg, sess.AccountID, map[string]any{"tenantId": sess.TenantID, "role": sess.Role}, t)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "internal", "gagal membuat token")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(FirebaseTokenResponse{
		Token: tok, TenantID: sess.TenantID, Role: sess.Role,
		ExpiresAt: t.Add(CustomTokenTTL).UTC().Format(time.RFC3339), ProjectID: cfg.ProjectID,
	})
}

func writeJSONErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}
