package chatwidget

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const visitorTokenTTL = 24 * time.Hour

func mintVisitorToken(secret, tenantSlug, sessionID string) (raw string, hash string, err error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", "", fmt.Errorf("visitor token secret tidak dikonfigurasi")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	exp := time.Now().Add(visitorTokenTTL).Unix()
	payload := fmt.Sprintf("%s|%s|%s|%d", tenantSlug, sessionID, hex.EncodeToString(nonce), exp)
	sig := signVisitor(secret, payload)
	raw = base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
	hash = hashVisitorToken(raw)
	return raw, hash, nil
}

func verifyVisitorToken(secret, tenantSlug, sessionID, raw string) bool {
	secret = strings.TrimSpace(secret)
	raw = strings.TrimSpace(raw)
	if secret == "" || raw == "" {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return false
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 5 {
		return false
	}
	if parts[0] != tenantSlug || parts[1] != sessionID {
		return false
	}
	exp, err := parseInt64(parts[3])
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	payload := strings.Join(parts[:4], "|")
	return signVisitor(secret, payload) == parts[4]
}

func signVisitor(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashVisitorToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
