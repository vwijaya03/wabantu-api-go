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

	encoreAuth "encore.dev/beta/auth"
	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ---------- Encore secrets ----------

var secrets struct {
	JWTSecret         string
	DataEncryptionKey string
	RedisURL          string
}

// ---------- constants ----------

const (
	bcryptCost      = 12
	jwtTTL          = 15 * time.Minute
	cookieName      = "wabantu_at"
	schemaPrefix    = "t_"
	maxSchemaLen    = 63
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
	ID     string         `json:"id"`
	Email  string         `json:"email"`
	Name   string         `json:"name"`
	Role   string         `json:"role"`
	Tenant TenantResponse `json:"tenant"`
}

type TenantResponse struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type MeResponse struct {
	ID     string         `json:"id"`
	Email  string         `json:"email"`
	Name   string         `json:"name"`
	Role   string         `json:"role"`
	Tenant TenantResponse `json:"tenant"`
}

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

	user := &types.AuthUser{
		AccountID:    sess.AccountID,
		TenantID:     sess.TenantID,
		TenantSchema: sess.TenantSchema,
		Email:        sess.Email,
		Name:         sess.Name,
		Role:         sess.Role,
		SessionID:    sessionID,
	}
	return encoreAuth.UID(accountID), user, nil
}

// ---------- API endpoints ----------

// Register creates a new tenant + account, bootstraps the tenant schema,
// seeds a business_profile row, and returns a JWT.
//
//encore:api public raw method=POST path=/auth/register
func Register(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	var body RegisterRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
	err := tenant.DB.QueryRow(ctx,
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
	tx, err := tenant.DB.Stdlib().BeginTx(ctx, nil)
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

	var accountID string
	var accountName sql.NullString
	var accountRole string
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tenant_account (email, email_hash, password_hash, name, tenant_id, role)
		 VALUES ($1, $2, $3, $4, $5, 'owner')
		 RETURNING id, name, role`,
		emailLower, emailHash, string(passwordHash), strings.TrimSpace(body.Name), tenantID,
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

	// Seed empty business_profile
	if conn, err := tenant.TenantConn(ctx, schemaName); err == nil {
		defer conn.Close()
		conn.ExecContext(ctx,
			`INSERT INTO business_profile (business_name, tone, ai_enabled)
			 VALUES ($1, 'friendly', true) ON CONFLICT DO NOTHING`,
			tenantName,
		)
	}

	completeLogin(w, ctx, accountID, emailLower, nullStr(accountName), accountRole,
		tenantID, tenantSlug, tenantName, schemaName, http.StatusCreated)
}

// Login verifies credentials and returns a JWT + sets a cookie.
//
//encore:api public raw method=POST path=/auth/login
func Login(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	var body LoginRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
	var accountRole, accountTenantID string
	err := tenant.DB.QueryRow(ctx,
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

	// Fetch tenant + company
	var tenantSlug, tenantName, tenantStatus string
	err = tenant.DB.QueryRow(ctx,
		`SELECT slug, name, status FROM tenant WHERE id = $1 AND deleted_at IS NULL`,
		accountTenantID,
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
	err = tenant.DB.QueryRow(ctx,
		`SELECT schema_name FROM tenant_company WHERE tenant_id = $1`,
		accountTenantID,
	).Scan(&schemaName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "company not found")
		return
	}

	// Update last_login_at
	tenant.DB.Exec(ctx,
		"UPDATE tenant_account SET last_login_at = $1 WHERE id = $2",
		time.Now(), accountID,
	)

	completeLogin(w, ctx, accountID, email, nullStr(accountName), accountRole,
		accountTenantID, tenantSlug, tenantName, schemaName, http.StatusOK)
}

// Logout destroys the current session and clears the auth cookie.
//
//encore:api auth raw method=POST path=/auth/logout
func Logout(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	userData, ok := encoreAuth.Data().(*types.AuthUser)
	if !ok || userData == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	_ = destroySession(ctx, userData.AccountID, userData.SessionID)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, LogoutResponse{OK: true})
}

// Me returns the current authenticated user's profile.
//
//encore:api auth method=GET path=/auth/me
func Me(ctx context.Context) (*MeResponse, error) {
	userData, ok := encoreAuth.Data().(*types.AuthUser)
	if !ok || userData == nil {
		return nil, errs.Unauthenticated("not authenticated")
	}

	var tenantSlug, tenantName string
	err := tenant.DB.QueryRow(ctx,
		`SELECT slug, name FROM tenant WHERE id = $1 AND deleted_at IS NULL`,
		userData.TenantID,
	).Scan(&tenantSlug, &tenantName)
	if err != nil {
		return nil, errs.NotFound("Tenant tidak ditemukan")
	}

	return &MeResponse{
		ID:    userData.AccountID,
		Email: userData.Email,
		Name:  userData.Name,
		Role:  userData.Role,
		Tenant: TenantResponse{
			ID:   userData.TenantID,
			Slug: tenantSlug,
			Name: tenantName,
		},
	}, nil
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
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secrets.JWTSecret), nil
	})
	if err != nil {
		return "", "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", fmt.Errorf("invalid token claims")
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
	w http.ResponseWriter, ctx context.Context,
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

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   expiresIn,
	})

	writeJSON(w, statusCode, AuthResponse{
		AccessToken:      accessToken,
		ExpiresInSeconds: expiresIn,
		User: UserResponse{
			ID:    accountID,
			Email: email,
			Name:  name,
			Role:  role,
			Tenant: TenantResponse{
				ID:   tenantID,
				Slug: tenantSlug,
				Name: tenantName,
			},
		},
	})
}

func cleanupRegistration(ctx context.Context, accountID, companyID, tenantID string) {
	tenant.DB.Exec(ctx, "DELETE FROM tenant_account WHERE id = $1", accountID)
	tenant.DB.Exec(ctx, "DELETE FROM tenant_company WHERE id = $1", companyID)
	tenant.DB.Exec(ctx, "DELETE FROM tenant WHERE id = $1", tenantID)
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
