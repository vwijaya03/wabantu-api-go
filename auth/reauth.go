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
	"encore.app/wabantu/system"
)

type ReauthRequest struct {
	Password    string `json:"password"`
	AccessToken string `json:"accessToken,omitempty"`
}

// Reauth issues a new access token for an existing Redis session when the JWT expired.
// Client must send accessToken in JSON body (not Authorization header) so Encore does not
// run AuthHandler on an expired JWT before this handler.
//
//encore:api public raw method=POST path=/api/v1/auth/reauth
func Reauth(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if !allowAuthRate(ctx, req) {
		writeError(w, http.StatusTooManyRequests, "too many attempts — coba lagi nanti")
		return
	}

	body, err := parseReauthRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Password) == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	token := strings.TrimSpace(body.AccessToken)
	if token == "" {
		token = extractBearerOrCookie(req)
	}
	if token == "" {
		writeError(w, http.StatusUnauthorized, "session tidak ditemukan — silakan masuk ulang")
		return
	}

	accountID, sessionID, err := parseJWTAllowExpired(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session tidak valid — silakan masuk ulang")
		return
	}

	sess, err := getSession(ctx, accountID, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "session berakhir — silakan masuk ulang")
		return
	}

	if err := verifyAccountPassword(ctx, accountID, body.Password); err != nil {
		if errors.Is(err, errInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "Password salah")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if err := touchSession(ctx, accountID, sessionID); err != nil {
		rlog.Warn("touch session failed", "err", err)
	}

	accessToken, expiresIn, err := signJWT(accountID, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sign jwt failed")
		return
	}

	audit.Log(ctx, sess.TenantID, accountID, "auth.reauth", "account", accountID, nil)

	authUser := buildAuthUser(sess, sessionID)
	writeJSON(w, http.StatusOK, AuthResponse{
		AccessToken:      accessToken,
		ExpiresInSeconds: expiresIn,
		User:             profileResponse(authUser),
	})
}

var errInvalidCredentials = errors.New("invalid credentials")

func verifyAccountPassword(ctx context.Context, accountID, password string) error {
	var storedHash string
	err := system.DB.QueryRow(ctx,
		`SELECT password_hash FROM tenant_account WHERE id = $1 AND deleted_at IS NULL`,
		accountID,
	).Scan(&storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(
			[]byte("$2b$12$invalidsaltinvalidsaltinvalidsaltinvalidsaltinv"),
			[]byte(password),
		)
		return errInvalidCredentials
	}
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		return errInvalidCredentials
	}
	return nil
}

func parseReauthRequest(r *http.Request) (ReauthRequest, error) {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "application/json") {
		return ReauthRequest{}, errors.New("Content-Type must be application/json")
	}
	var body ReauthRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return ReauthRequest{}, errors.New("invalid JSON body")
	}
	return body, nil
}
