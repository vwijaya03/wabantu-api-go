package templates

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const assetTokenTTL = 15 * time.Minute

func mintAssetToken(secret, assetID string) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", fmt.Errorf("asset token secret tidak dikonfigurasi")
	}
	exp := time.Now().Add(assetTokenTTL).Unix()
	payload := fmt.Sprintf("%s|%d", assetID, exp)
	sig := signAsset(secret, payload)
	return payload + "|" + sig, nil
}

func verifyAssetToken(secret, assetID, raw string) bool {
	secret = strings.TrimSpace(secret)
	raw = strings.TrimSpace(raw)
	if secret == "" || raw == "" {
		return false
	}
	parts := strings.Split(raw, "|")
	if len(parts) != 3 {
		return false
	}
	if parts[0] != assetID {
		return false
	}
	var exp int64
	for _, c := range parts[1] {
		if c < '0' || c > '9' {
			return false
		}
		exp = exp*10 + int64(c-'0')
	}
	if time.Now().Unix() > exp {
		return false
	}
	payload := parts[0] + "|" + parts[1]
	return signAsset(secret, payload) == parts[2]
}

func signAsset(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
