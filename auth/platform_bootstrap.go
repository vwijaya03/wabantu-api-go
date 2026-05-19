package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"encore.dev/rlog"
	"encore.app/wabantu/audit"
	"encore.app/wabantu/shared/ratelimit"
	"encore.app/wabantu/system"

	"golang.org/x/crypto/bcrypt"
)

const (
	roleSuperAdmin              = "super_admin"
	platformBootstrapHeader     = "X-Platform-Bootstrap-Secret"
	minPlatformBootstrapSecretLen = 32
)

// BootstrapPlatformAdminRequest creates the first internal platform operator (no customer tenant).
type BootstrapPlatformAdminRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// BootstrapPlatformAdmin creates a super_admin account without tenant_id.
// Requires header X-Platform-Bootstrap-Secret matching Encore secret PlatformAdminBootstrapSecret.
//
//encore:api public raw method=POST path=/api/v1/internal/platform-admin/bootstrap
func BootstrapPlatformAdmin(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if !allowPlatformBootstrapRate(ctx, req) {
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}

	secret := strings.TrimSpace(req.Header.Get(platformBootstrapHeader))
	if secret == "" || secret != strings.TrimSpace(secrets.PlatformAdminBootstrapSecret) {
		writeError(w, http.StatusUnauthorized, "invalid bootstrap secret")
		return
	}
	if len(strings.TrimSpace(secrets.PlatformAdminBootstrapSecret)) < minPlatformBootstrapSecretLen {
		writeError(w, http.StatusServiceUnavailable, "PlatformAdminBootstrapSecret not configured (min 32 chars)")
		return
	}

	body, err := parseBootstrapRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Email == "" || body.Password == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}
	if len(body.Password) < 10 {
		writeError(w, http.StatusBadRequest, "password must be at least 10 characters")
		return
	}

	emailLower := normalizeEmail(body.Email)
	emailHash := hashForLookup(emailLower)

	var existingID string
	err = system.DB.QueryRow(ctx,
		`SELECT id FROM tenant_account WHERE email_hash = $1 AND deleted_at IS NULL`,
		emailHash,
	).Scan(&existingID)
	if err == nil {
		writeError(w, http.StatusConflict, "Email sudah terdaftar")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcryptCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password hash failed")
		return
	}

	var accountID string
	var accountName sql.NullString
	err = system.DB.QueryRow(ctx, `
		INSERT INTO tenant_account (email, email_hash, password_hash, name, tenant_id, role)
		VALUES ($1, $2, $3, $4, NULL, $5)
		RETURNING id, name`,
		emailLower, emailHash, string(passwordHash), strings.TrimSpace(body.Name), roleSuperAdmin,
	).Scan(&accountID, &accountName)
	if err != nil {
		rlog.Error("platform admin bootstrap insert failed", "err", err)
		writeError(w, http.StatusInternalServerError, "create account failed")
		return
	}

	audit.Log(ctx, "", accountID, "platform_admin.bootstrap", "tenant_account", accountID, map[string]string{
		"email": emailLower,
	})

	completeLogin(w, req, ctx, accountID, emailLower, nullStr(accountName), roleSuperAdmin,
		"", "", "", "", http.StatusCreated)
}

func parseBootstrapRequest(r *http.Request) (BootstrapPlatformAdminRequest, error) {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "application/json") {
		return BootstrapPlatformAdminRequest{}, errors.New("Content-Type must be application/json")
	}
	var body BootstrapPlatformAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return BootstrapPlatformAdminRequest{}, errors.New("invalid JSON body")
	}
	return body, nil
}

func allowPlatformBootstrapRate(ctx context.Context, r *http.Request) bool {
	return ratelimit.Allow(ctx, getRedis(), ratelimit.Key("platform-bootstrap", ratelimit.ClientIP(r)), 5, time.Minute)
}
