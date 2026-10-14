package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"encore.app/wabantu/internal/adminauth"
)

// checkSessionKind binds the (already signature-verified) token's "typ" claim to the
// server-side session kind and enforces the absolute admin session lifetime. A client token
// can never address an admin session and vice versa.
func checkSessionKind(token string, sess *SessionData) error {
	if sess == nil {
		return errors.New("no session")
	}
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
		return err
	}
	typ, _ := claims["typ"].(string)
	if err := adminauth.CheckTokenKind(typ, sess.Kind); err != nil {
		return err
	}
	return adminauth.CheckAdminExpiry(sess.Kind, time.Unix(sess.IssuedAt, 0), time.Now())
}
