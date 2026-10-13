package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"encore.dev/rlog"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/shared/ratelimit"
	"encore.app/wabantu/system"
)

const (
	msgTooMany    = "too many attempts — coba lagi nanti"
	msgBadRequest = "email and password are required"
)

// PlatformAdminLogin is the ONLY login for platform admins (super_admin without tenant).
// Client accounts get the same generic 401 as a wrong password. Returns a short-lived
// admin-typed token (sessionType "admin").
//
//encore:api public raw method=POST path=/api/v1/platform-admin/login
func PlatformAdminLogin(w http.ResponseWriter, req *http.Request) {
	serveLoginPortal(w, req, adminauth.PortalAdmin)
}

// redisCounters implements adminauth.CounterStore.
type redisCounters struct{ c *redis.Client }

func (r redisCounters) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	pipe := r.c.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (r redisCounters) SetLock(ctx context.Context, key string, ttl time.Duration) error {
	return r.c.Set(ctx, key, 1, ttl).Err()
}

func (r redisCounters) Locked(ctx context.Context, key string) (bool, error) {
	n, err := r.c.Exists(ctx, key).Result()
	return n > 0, err
}

func (r redisCounters) Del(ctx context.Context, keys ...string) error {
	return r.c.Del(ctx, keys...).Err()
}

func loginGuard(portal string) adminauth.Guard {
	p := adminauth.ClientPolicy()
	if portal == adminauth.PortalAdmin {
		p = adminauth.AdminPolicy()
	}
	return adminauth.Guard{S: redisCounters{getRedis()}, P: p}
}

// recordLoginAttempt writes one sanitized row to auth_login_attempt (best-effort) and a log
// line. Never receives the password or token.
func recordLoginAttempt(ctx context.Context, r *http.Request, portal, email, accountID, result, reason string) {
	a := adminauth.NewAttempt(time.Now(), portal, email, accountID, ratelimit.ClientIP(r), r.UserAgent(), result, reason)
	rlog.Info("auth attempt", "portal", a.Portal, "email", a.Email, "ip", a.IP, "result", a.Result, "reason", a.Reason)
	var acc any
	if a.AccountID != "" {
		acc = a.AccountID
	}
	if _, err := system.DB.Exec(ctx, `
		INSERT INTO auth_login_attempt (at, portal, email, account_id, ip, user_agent, result, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		a.At, a.Portal, a.Email, acc, a.IP, a.UserAgent, a.Result, a.Reason); err != nil {
		rlog.Warn("auth attempt audit insert failed", "err", err)
	}
}

// serveLoginPortal implements both portals. Unknown account, wrong password and wrong
// portal all cost one full bcrypt compare, count as a failure, and return the identical
// 401 body; lock/rate responses are identical for existing and unknown accounts.
func serveLoginPortal(w http.ResponseWriter, req *http.Request, portal string) {
	ctx := req.Context()
	body, err := parseLoginRequest(req)
	if err != nil || body.Email == "" || body.Password == "" {
		recordLoginAttempt(ctx, req, portal, body.Email, "", adminauth.ResultFailure, adminauth.ReasonBadRequest)
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	emailLower := normalizeEmail(body.Email)
	ip := ratelimit.ClientIP(req)
	acct := adminauth.AccountKey(emailLower)
	guard := loginGuard(portal)

	if portal == adminauth.PortalClient && !allowAuthRate(ctx, req) {
		recordLoginAttempt(ctx, req, portal, emailLower, "", adminauth.ResultBlocked, adminauth.ReasonRateIP)
		writeError(w, http.StatusTooManyRequests, msgTooMany)
		return
	}
	ok, reason, gerr := guard.Check(ctx, ip, acct)
	if gerr != nil {
		rlog.Warn("login guard store error", "portal", portal, "err", gerr)
	}
	if !ok {
		recordLoginAttempt(ctx, req, portal, emailLower, "", adminauth.ResultBlocked, reason)
		writeError(w, http.StatusTooManyRequests, msgTooMany)
		return
	}

	fail := func(accountID, reason string) {
		if err := guard.Fail(ctx, ip, acct); err != nil {
			rlog.Warn("login guard fail-count error", "err", err)
		}
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultFailure, reason)
		writeError(w, http.StatusUnauthorized, adminauth.ErrInvalidCredentials.Error())
	}

	var accountID, storedHash, email string
	var accountName, accountTenantID sql.NullString
	var accountRole string
	err = system.DB.QueryRow(ctx,
		`SELECT id, password_hash, email, name, role, tenant_id
		 FROM tenant_account
		 WHERE email_hash = $1 AND deleted_at IS NULL`,
		hashForLookup(emailLower),
	).Scan(&accountID, &storedHash, &email, &accountName, &accountRole, &accountTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(adminauth.DummyHash(bcryptCost), []byte(body.Password))
		fail("", adminauth.ReasonInvalidCredentials)
		return
	}
	if err != nil {
		recordLoginAttempt(ctx, req, portal, emailLower, "", adminauth.ResultFailure, adminauth.ReasonInternal)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(body.Password)); err != nil {
		fail(accountID, adminauth.ReasonInvalidCredentials)
		return
	}
	kind, perr := adminauth.CheckPortal(portal, accountRole, accountTenantID.String)
	if perr != nil {
		fail(accountID, adminauth.ReasonPortalMismatch)
		return
	}
	_ = guard.Success(ctx, ip, acct)
	system.DB.Exec(ctx, "UPDATE tenant_account SET last_login_at = $1 WHERE id = $2", time.Now(), accountID)

	if kind == adminauth.KindAdmin {
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultSuccess, adminauth.ReasonOK)
		completeLoginKind(w, req, ctx, accountID, email, nullStr(accountName), accountRole,
			"", "", "", "", http.StatusOK, adminauth.KindAdmin)
		return
	}

	// Client portal (unchanged tenant checks; only reachable with a correct password).
	if !accountTenantID.Valid || accountTenantID.String == "" {
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultFailure, adminauth.ReasonNoTenant)
		writeError(w, http.StatusForbidden, "Akun tidak terhubung ke bisnis")
		return
	}
	var tenantSlug, tenantName, tenantStatus string
	err = system.DB.QueryRow(ctx,
		`SELECT slug, name, status FROM tenant WHERE id = $1 AND deleted_at IS NULL`,
		accountTenantID.String,
	).Scan(&tenantSlug, &tenantName, &tenantStatus)
	if errors.Is(err, sql.ErrNoRows) {
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultFailure, adminauth.ReasonNoTenant)
		writeError(w, http.StatusUnauthorized, "Tenant tidak ditemukan")
		return
	}
	if err != nil {
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultFailure, adminauth.ReasonInternal)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tenantStatus != "active" {
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultFailure, adminauth.ReasonTenantInactive)
		writeError(w, http.StatusForbidden, "Akun bisnis tidak aktif")
		return
	}
	var schemaName string
	if err := system.DB.QueryRow(ctx,
		`SELECT schema_name FROM tenant_company WHERE tenant_id = $1`, accountTenantID.String,
	).Scan(&schemaName); err != nil {
		recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultFailure, adminauth.ReasonInternal)
		writeError(w, http.StatusInternalServerError, "company not found")
		return
	}
	recordLoginAttempt(ctx, req, portal, emailLower, accountID, adminauth.ResultSuccess, adminauth.ReasonOK)
	completeLoginKind(w, req, ctx, accountID, email, nullStr(accountName), accountRole,
		accountTenantID.String, tenantSlug, tenantName, schemaName, http.StatusOK, adminauth.KindClient)
}

// PlatformAdminMeResponse is the admin profile.
type PlatformAdminMeResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// PlatformAdminMe returns the admin profile. ONLY admin tokens (session kind admin, within
// the 2 h cap); client and impersonation tokens get 401.
//
//encore:api public raw method=GET path=/api/v1/platform-admin/me
func PlatformAdminMe(w http.ResponseWriter, r *http.Request) {
	u, err := AuthenticateHTTP(r.Context(), r)
	if err != nil || adminauth.MeAccess(adminauth.MeAdmin, u.SessionKind) != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, PlatformAdminMeResponse{ID: u.AccountID, Email: u.Email, Name: u.Name, Role: u.Role})
}
