package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"encore.dev/rlog"
	"golang.org/x/crypto/bcrypt"

	"encore.app/wabantu/audit"
	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/shared/ratelimit"
	"encore.app/wabantu/system"
)

const (
	roleSuperAdmin          = "super_admin"
	platformBootstrapHeader = "X-Platform-Bootstrap-Secret"
	portalBootstrap         = "bootstrap"
)

// BootstrapPlatformAdminRequest creates the first internal platform operator (no customer tenant).
type BootstrapPlatformAdminRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// BootstrapPlatformAdminResponse: no token is issued — sign in via /api/v1/platform-admin/login.
type BootstrapPlatformAdminResponse struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
}

// BootstrapPlatformAdmin creates the FIRST super_admin (tenant_id NULL), once.
//   - secret: header X-Platform-Bootstrap-Secret vs Encore secret PlatformAdminBootstrapKey
//     (SHA-256 + constant-time compare). Unset/empty/<32 chars or mismatch → 403 (same body).
//   - one-time: advisory xact lock + "platform admin exists?" + insert in ONE transaction → 409.
//   - Redis unavailable/erroring → 503 (fail-closed, Security L-1).
//   - rate limited (5/min/IP + lockout after 5 failures/15 min); every attempt audited
//     (auth_login_attempt, portal=bootstrap) without the secret or password.
//
//encore:api public raw method=POST path=/api/v1/internal/platform-admin/bootstrap
func BootstrapPlatformAdmin(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	ip := ratelimit.ClientIP(req)
	// Fail-closed (Security L-1): Redis missing/erroring → 503, never an unthrottled attempt.
	guard := adminauth.Guard{S: redisCounters{getRedis()}, P: adminauth.BootstrapPolicy()}
	const acct = "bootstrap"
	ok, reason, gerr := guard.Check(ctx, ip, acct)
	if gerr != nil {
		rlog.Warn("bootstrap guard store error", "err", gerr)
	}
	switch guard.CheckStatus(ok, gerr) {
	case http.StatusServiceUnavailable:
		recordLoginAttempt(ctx, req, portalBootstrap, "", "", adminauth.ResultBlocked, "store_unavailable")
		writeError(w, http.StatusServiceUnavailable, "temporarily unavailable")
		return
	case http.StatusTooManyRequests:
		recordLoginAttempt(ctx, req, portalBootstrap, "", "", adminauth.ResultBlocked, reason)
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}

	if err := adminauth.CheckBootstrapSecret(secrets.PlatformAdminBootstrapKey, req.Header.Get(platformBootstrapHeader)); err != nil {
		r := "bootstrap_secret_invalid"
		if errors.Is(err, adminauth.ErrBootstrapDisabled) {
			r = "bootstrap_disabled"
		}
		_ = guard.Fail(ctx, ip, acct)
		recordLoginAttempt(ctx, req, portalBootstrap, "", "", adminauth.ResultFailure, r)
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	body, err := parseBootstrapRequest(req)
	in := adminauth.BootstrapInput{Email: normalizeEmail(body.Email), Name: strings.TrimSpace(body.Name), Password: body.Password}
	if err != nil || !in.Valid() {
		recordLoginAttempt(ctx, req, portalBootstrap, in.Email, "", adminauth.ResultFailure, adminauth.ReasonBadRequest)
		writeError(w, http.StatusBadRequest, adminauth.ErrBootstrapInput.Error())
		return
	}

	tx, err := system.DB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	id, err := adminauth.RunBootstrap(ctx, bootstrapTx{tx}, in)
	switch {
	case errors.Is(err, adminauth.ErrAlreadyBootstrapped):
		recordLoginAttempt(ctx, req, portalBootstrap, in.Email, "", adminauth.ResultFailure, "bootstrap_already_done")
		writeError(w, http.StatusConflict, "platform admin already exists")
		return
	case errors.Is(err, adminauth.ErrEmailTaken):
		recordLoginAttempt(ctx, req, portalBootstrap, in.Email, "", adminauth.ResultFailure, "email_taken")
		writeError(w, http.StatusConflict, "Email sudah terdaftar")
		return
	case err != nil:
		rlog.Error("platform admin bootstrap failed", "err", err)
		recordLoginAttempt(ctx, req, portalBootstrap, in.Email, "", adminauth.ResultFailure, adminauth.ReasonInternal)
		writeError(w, http.StatusInternalServerError, "create account failed")
		return
	}
	recordLoginAttempt(ctx, req, portalBootstrap, in.Email, id, adminauth.ResultSuccess, adminauth.ReasonOK)
	audit.Log(ctx, "", id, "platform_admin.bootstrap", "tenant_account", id, map[string]string{"email": in.Email})
	writeJSON(w, http.StatusCreated, BootstrapPlatformAdminResponse{AccountID: id, Email: in.Email})
}

// bootstrapTx implements adminauth.BootstrapTx over one *sql.Tx.
type bootstrapTx struct{ tx *sql.Tx }

func (b bootstrapTx) Lock(ctx context.Context) error {
	_, err := b.tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('wabantu.platform_admin_bootstrap'))`)
	return err
}

func (b bootstrapTx) PlatformAdminExists(ctx context.Context) (bool, error) {
	var exists bool
	err := b.tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tenant_account
		WHERE role = $1 AND tenant_id IS NULL AND deleted_at IS NULL)`, roleSuperAdmin).Scan(&exists)
	return exists, err
}

func (b bootstrapTx) EmailTaken(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := b.tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tenant_account
		WHERE email_hash = $1 AND deleted_at IS NULL)`, hashForLookup(email)).Scan(&exists)
	return exists, err
}

func (b bootstrapTx) CreateAdmin(ctx context.Context, email, name, password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	var id string
	err = b.tx.QueryRowContext(ctx, `
		INSERT INTO tenant_account (email, email_hash, password_hash, name, tenant_id, role)
		VALUES ($1, $2, $3, $4, NULL, $5) RETURNING id`,
		email, hashForLookup(email), string(hash), name, roleSuperAdmin).Scan(&id)
	return id, err
}

func (b bootstrapTx) Commit() error   { return b.tx.Commit() }
func (b bootstrapTx) Rollback() error { return b.tx.Rollback() }

func parseBootstrapRequest(r *http.Request) (BootstrapPlatformAdminRequest, error) {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "application/json") {
		return BootstrapPlatformAdminRequest{}, errors.New("Content-Type must be application/json")
	}
	var body BootstrapPlatformAdminRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<10)).Decode(&body); err != nil {
		return BootstrapPlatformAdminRequest{}, errors.New("invalid JSON body")
	}
	return body, nil
}
