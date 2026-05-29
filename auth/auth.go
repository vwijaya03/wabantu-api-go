package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	_ "encore.app/wabantu/platform"
	encoreAuth "encore.dev/beta/auth"
	"encore.dev/rlog"

	"encore.app/wabantu/audit"
	"encore.app/wabantu/branch"
	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/ratelimit"
	"encore.app/wabantu/shared/response"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenant"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ---------- Encore secrets ----------

var secrets struct {
	JWTSecret                    string
	DataEncryptionKey            string
	RedisURL                     string
	PlatformAdminBootstrapSecret string
}

// ---------- constants ----------

const (
	bcryptCost   = 12
	jwtTTL       = 60 * time.Minute
	cookieName   = "wabantu_at"
	schemaPrefix = "t_"
	maxSchemaLen = 63
)

// ---------- request / response types ----------

type RegisterRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	Name         string `json:"name"`
	BusinessName string `json:"businessName"`
	Slug         string `json:"slug,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	AccessToken      string       `json:"accessToken"`
	User             UserResponse `json:"user"`
	ExpiresInSeconds int          `json:"expiresInSeconds"`
}

type UserResponse struct {
	ID            string                 `json:"id"`
	Email         string                 `json:"email"`
	Name          string                 `json:"name"`
	Role          string                 `json:"role"`
	Tenant        *TenantResponse        `json:"tenant,omitempty"`
	Platform      bool                   `json:"platform,omitempty"`
	Impersonation *ImpersonationResponse `json:"impersonation,omitempty"`
}

type TenantResponse struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type ImpersonationResponse struct {
	Active bool            `json:"active"`
	Tenant *TenantResponse `json:"tenant,omitempty"`
}

// MeResponse mirrors UserResponse for GET /auth/me.
type MeResponse = UserResponse

type LogoutResponse struct {
	OK bool `json:"ok"`
}

// ---------- Encore auth handler ----------

// AuthHandler validates the Bearer JWT, loads the Redis session, and returns
// the authenticated user context for all `auth`-tagged endpoints.
//
//encore:authhandler
func AuthHandler(ctx context.Context, token string) (encoreAuth.UID, *types.AuthUser, error) {
	accountID, sessionID, err := parseJWT(token)
	if err != nil {
		return "", nil, errs.Unauthenticated("invalid token")
	}

	sess, err := getSession(ctx, accountID, sessionID)
	if err != nil {
		return "", nil, errs.Internal("session lookup failed")
	}
	if sess == nil {
		return "", nil, errs.Unauthenticated("session expired")
	}

	return encoreAuth.UID(accountID), buildAuthUser(sess, sessionID), nil
}

// ---------- API endpoints ----------

// Register creates a new tenant + account, bootstraps the tenant schema,
// seeds a business_profile row, and returns a JWT.
//
//encore:api public raw method=POST path=/api/v1/auth/register
func Register(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if !allowAuthRate(ctx, req) {
		writeError(w, http.StatusTooManyRequests, "too many attempts — coba lagi nanti")
		return
	}

	body, err := parseRegisterRequest(req)
	if err != nil {
		rlog.Warn("register decode failed", "err", err, "contentType", req.Header.Get("Content-Type"))
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Email == "" || body.Password == "" || body.Name == "" || body.BusinessName == "" {
		writeError(w, http.StatusBadRequest, "email, password, name, and businessName are required")
		return
	}

	emailLower := normalizeEmail(body.Email)
	emailHash := hashForLookup(emailLower)

	// Duplicate check
	var existingID string
	err = system.DB.QueryRow(ctx,
		"SELECT id FROM tenant_account WHERE email_hash = $1 AND deleted_at IS NULL",
		emailHash,
	).Scan(&existingID)
	if err == nil {
		writeError(w, http.StatusConflict, "Email sudah terdaftar")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	// Unique slug
	baseSlug := slugify(body.Slug)
	if baseSlug == "" {
		baseSlug = slugify(body.BusinessName)
	}
	if baseSlug == "" {
		baseSlug = "biz"
	}
	uniqueSlug, err := tenant.FindUniqueSlug(ctx, baseSlug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "slug generation failed")
		return
	}

	schemaName := schemaPrefix + uniqueSlug
	if len(schemaName) > maxSchemaLen {
		schemaName = schemaName[:maxSchemaLen]
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcryptCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password hash failed")
		return
	}

	// --- system-DB transaction: tenant + company + account ---
	tx, err := system.DB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin tx: "+err.Error())
		return
	}
	defer tx.Rollback()

	var tenantID, tenantSlug, tenantName string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tenant (slug, name, status) VALUES ($1, $2, 'active')
		 RETURNING id, slug, name`,
		uniqueSlug, strings.TrimSpace(body.BusinessName),
	).Scan(&tenantID, &tenantSlug, &tenantName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert tenant: "+err.Error())
		return
	}

	var companyID string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tenant_company (tenant_id, schema_name) VALUES ($1, $2) RETURNING id`,
		tenantID, schemaName,
	).Scan(&companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert company: "+err.Error())
		return
	}

	accountRole := "owner"

	var accountID string
	var accountName sql.NullString
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tenant_account (email, email_hash, password_hash, name, tenant_id, role)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, name, role`,
		emailLower, emailHash, string(passwordHash), strings.TrimSpace(body.Name), tenantID, accountRole,
	).Scan(&accountID, &accountName, &accountRole)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert account: "+err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit tx: "+err.Error())
		return
	}

	// --- bootstrap tenant schema ---
	if err := tenant.RunTenantDDL(ctx, schemaName); err != nil {
		// Clean up committed system rows so the user can retry.
		cleanupRegistration(ctx, accountID, companyID, tenantID)
		writeError(w, http.StatusInternalServerError, "schema bootstrap failed: "+err.Error())
		return
	}

	// Seed empty business_profile + default branch
	if conn, err := tenant.TenantConn(ctx, schemaName); err == nil {
		defer conn.Close()
		conn.ExecContext(ctx,
			`INSERT INTO business_profile (business_name, tone, ai_enabled)
			 VALUES ($1, 'friendly', true) ON CONFLICT DO NOTHING`,
			tenantName,
		)
	}
	_ = branch.EnsureDefaultBranch(ctx, schemaName)

	completeLogin(w, req, ctx, accountID, emailLower, nullStr(accountName), accountRole,
		tenantID, tenantSlug, tenantName, schemaName, http.StatusCreated)
}

// Login verifies credentials and returns a JWT + sets a cookie.
//
//encore:api public raw method=POST path=/api/v1/auth/login
func Login(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if !allowAuthRate(ctx, req) {
		writeError(w, http.StatusTooManyRequests, "too many attempts — coba lagi nanti")
		return
	}

	body, err := parseLoginRequest(req)
	if err != nil {
		rlog.Warn("login decode failed", "err", err, "contentType", req.Header.Get("Content-Type"))
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	emailLower := normalizeEmail(body.Email)
	emailHash := hashForLookup(emailLower)

	var accountID, storedHash, email string
	var accountName sql.NullString
	var accountRole string
	var accountTenantID sql.NullString
	err = system.DB.QueryRow(ctx,
		`SELECT id, password_hash, email, name, role, tenant_id
		 FROM tenant_account
		 WHERE email_hash = $1 AND deleted_at IS NULL`,
		emailHash,
	).Scan(&accountID, &storedHash, &email, &accountName, &accountRole, &accountTenantID)

	if errors.Is(err, sql.ErrNoRows) {
		// Constant-time compare against a dummy hash to prevent timing attacks.
		bcrypt.CompareHashAndPassword(
			[]byte("$2b$12$invalidsaltinvalidsaltinvalidsaltinvalidsaltinv"),
			[]byte(body.Password),
		)
		writeError(w, http.StatusUnauthorized, "Email atau password salah")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "Email atau password salah")
		return
	}

	system.DB.Exec(ctx,
		"UPDATE tenant_account SET last_login_at = $1 WHERE id = $2",
		time.Now(), accountID,
	)

	// Internal platform operator — no customer tenant required.
	if accountRole == roleSuperAdmin && (!accountTenantID.Valid || accountTenantID.String == "") {
		completeLogin(w, req, ctx, accountID, email, nullStr(accountName), accountRole,
			"", "", "", "", http.StatusOK)
		return
	}

	if !accountTenantID.Valid || accountTenantID.String == "" {
		writeError(w, http.StatusForbidden, "Akun tidak terhubung ke bisnis")
		return
	}

	var tenantSlug, tenantName, tenantStatus string
	err = system.DB.QueryRow(ctx,
		`SELECT slug, name, status FROM tenant WHERE id = $1 AND deleted_at IS NULL`,
		accountTenantID.String,
	).Scan(&tenantSlug, &tenantName, &tenantStatus)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "Tenant tidak ditemukan")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tenantStatus != "active" {
		writeError(w, http.StatusForbidden, "Akun bisnis tidak aktif")
		return
	}

	var schemaName string
	err = system.DB.QueryRow(ctx,
		`SELECT schema_name FROM tenant_company WHERE tenant_id = $1`,
		accountTenantID.String,
	).Scan(&schemaName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "company not found")
		return
	}

	completeLogin(w, req, ctx, accountID, email, nullStr(accountName), accountRole,
		accountTenantID.String, tenantSlug, tenantName, schemaName, http.StatusOK)
}

// Logout destroys the current session (Bearer token required).
//
//encore:api public raw method=POST path=/api/v1/auth/logout
func Logout(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	userData, err := AuthenticateHTTP(ctx, req)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	_ = destroySession(ctx, userData.AccountID, userData.SessionID)
	audit.Log(ctx, userData.TenantID, userData.AccountID, "auth.logout", "account", userData.AccountID, nil)

	writeJSON(w, http.StatusOK, LogoutResponse{OK: true})
}

// MeEnvelopeResponse matches NestJS { success, data } for GET /auth/me.
type MeEnvelopeResponse struct {
	Success bool       `json:"success"`
	Data    MeResponse `json:"data"`
}

// Me returns the current authenticated user's profile.
// Raw HTTP so Cookie and Authorization Bearer both work (Encore auth tag only passes Bearer).
//
//encore:api public raw method=GET path=/api/v1/auth/me
func Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userData, err := AuthenticateHTTP(ctx, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	writeJSON(w, http.StatusOK, profileResponse(userData))
}

// ---------- JWT helpers ----------

func signJWT(accountID, sessionID string) (string, int, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": accountID,
		"sid": sessionID,
		"iat": now.Unix(),
		"exp": now.Add(jwtTTL).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secrets.JWTSecret))
	if err != nil {
		return "", 0, err
	}
	return signed, int(jwtTTL.Seconds()), nil
}

func parseJWT(tokenString string) (accountID, sessionID string, err error) {
	return parseJWTClaims(tokenString, false)
}

// parseJWTAllowExpired reads sub/sid from a JWT that may be past exp (Redis session must still exist).
func parseJWTAllowExpired(tokenString string) (accountID, sessionID string, err error) {
	return parseJWTClaims(tokenString, true)
}

func parseJWTClaims(tokenString string, allowExpired bool) (accountID, sessionID string, err error) {
	keyFn := func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secrets.JWTSecret), nil
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	token, err := parser.ParseWithClaims(tokenString, claims, keyFn)
	if err != nil {
		if allowExpired && errors.Is(err, jwt.ErrTokenExpired) {
			// Signature already verified; read sub/sid from expired token.
		} else {
			return "", "", err
		}
	} else if !token.Valid {
		return "", "", fmt.Errorf("invalid token")
	}

	sub, _ := claims["sub"].(string)
	sid, _ := claims["sid"].(string)
	if sub == "" || sid == "" {
		return "", "", fmt.Errorf("missing sub/sid in token")
	}
	return sub, sid, nil
}

// ---------- internal helpers ----------

func completeLogin(
	w http.ResponseWriter, r *http.Request, ctx context.Context,
	accountID, email, name, role,
	tenantID, tenantSlug, tenantName, schemaName string,
	statusCode int,
) {
	sess, err := createSession(ctx, SessionData{
		AccountID:    accountID,
		TenantID:     tenantID,
		TenantSchema: schemaName,
		Role:         role,
		Email:        email,
		Name:         name,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create session failed")
		return
	}

	accessToken, expiresIn, err := signJWT(accountID, sess.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sign jwt failed")
		return
	}

	audit.Log(ctx, tenantID, accountID, "auth.login", "account", accountID, map[string]string{
		"email": email, "role": role,
	})

	authUser := buildAuthUser(&sess.Data, sess.SessionID)
	writeJSON(w, statusCode, AuthResponse{
		AccessToken:      accessToken,
		ExpiresInSeconds: expiresIn,
		User:             profileResponse(authUser),
	})
}

func profileResponse(u *types.AuthUser) UserResponse {
	resp := UserResponse{
		ID:    u.AccountID,
		Email: u.Email,
		Name:  u.Name,
		Role:  u.Role,
	}
	if u.IsPlatformSession {
		resp.Platform = true
		return resp
	}
	if u.Impersonating && u.TenantID != "" {
		resp.Impersonation = &ImpersonationResponse{
			Active: true,
			Tenant: &TenantResponse{
				ID:   u.TenantID,
				Slug: u.ImpersonationTenantSlug,
				Name: u.ImpersonationTenantName,
			},
		}
		resp.Tenant = resp.Impersonation.Tenant
		return resp
	}
	if u.TenantID != "" {
		slug, name := u.ImpersonationTenantSlug, u.ImpersonationTenantName
		if slug == "" || name == "" {
			slug, name = lookupTenantDisplay(context.Background(), u.TenantID)
		}
		resp.Tenant = &TenantResponse{ID: u.TenantID, Slug: slug, Name: name}
	}
	return resp
}

func lookupTenantDisplay(ctx context.Context, tenantID string) (slug, name string) {
	_ = system.DB.QueryRow(ctx,
		`SELECT slug, name FROM tenant WHERE id = $1 AND deleted_at IS NULL`, tenantID,
	).Scan(&slug, &name)
	return slug, name
}

func cleanupRegistration(ctx context.Context, accountID, companyID, tenantID string) {
	system.DB.Exec(ctx, "DELETE FROM tenant_account WHERE id = $1", accountID)
	system.DB.Exec(ctx, "DELETE FROM tenant_company WHERE id = $1", companyID)
	system.DB.Exec(ctx, "DELETE FROM tenant WHERE id = $1", tenantID)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashForLookup(email string) string {
	h := sha256.Sum256([]byte(email))
	return hex.EncodeToString(h[:])
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) && r < 128:
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// ---------- HTTP helpers for raw endpoints ----------

// Register/login expect JSON. Postman: Body → raw → JSON, Header Content-Type: application/json.
func parseRegisterRequest(r *http.Request) (RegisterRequest, error) {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return RegisterRequest{}, fmt.Errorf("invalid form body")
		}
		return RegisterRequest{
			Email:        r.FormValue("email"),
			Password:     r.FormValue("password"),
			Name:         r.FormValue("name"),
			BusinessName: r.FormValue("businessName"),
			Slug:         r.FormValue("slug"),
		}, nil
	}
	if ct != "" && !strings.Contains(ct, "application/json") {
		return RegisterRequest{}, fmt.Errorf(
			"Content-Type must be application/json (raw body). Got: %s. Do not use form-data.",
			r.Header.Get("Content-Type"),
		)
	}
	var body RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return RegisterRequest{}, fmt.Errorf("invalid JSON body: use raw JSON with email, password, name, businessName")
	}
	return body, nil
}

func parseLoginRequest(r *http.Request) (LoginRequest, error) {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return LoginRequest{}, fmt.Errorf("invalid form body")
		}
		return LoginRequest{
			Email:    r.FormValue("email"),
			Password: r.FormValue("password"),
		}, nil
	}
	if ct != "" && !strings.Contains(ct, "application/json") {
		return LoginRequest{}, fmt.Errorf("Content-Type must be application/json (raw body)")
	}
	var body LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return LoginRequest{}, fmt.Errorf("invalid JSON body: use raw JSON with email and password")
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response.Wrap(v))
}

func allowAuthRate(ctx context.Context, r *http.Request) bool {
	return ratelimit.Allow(ctx, getRedis(), ratelimit.Key("auth", ratelimit.ClientIP(r)), ratelimit.AuthRPM, time.Minute)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": msg,
	})
}
