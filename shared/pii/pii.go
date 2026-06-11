// Package pii provides field-level encryption and blind indexes for PII columns.
package pii

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"

	appcrypto "encore.app/wabantu/shared/crypto"
)

const MinKeyLen = 32

// ErrKeyNotConfigured is returned when DataEncryptionKey is missing or too short.
var ErrKeyNotConfigured = errors.New("encryption key not configured")

// ValidateKey ensures the encryption key meets minimum length.
func ValidateKey(key string) error {
	if len(strings.TrimSpace(key)) < MinKeyLen {
		return ErrKeyNotConfigured
	}
	return nil
}

// BlindKey derives a dedicated HMAC key from the encryption key.
func BlindKey(encKey string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(encKey) + ":blind"))
	return string(h[:])
}

// BlindIndex returns a deterministic HMAC-SHA256 hex digest for equality lookup / dedup.
func BlindIndex(normalized, encKey string) string {
	mac := hmac.New(sha256.New, []byte(BlindKey(encKey)))
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

// Encrypt stores plaintext as AES-256-GCM base64 ciphertext.
func Encrypt(plain, encKey string) (string, error) {
	if err := ValidateKey(encKey); err != nil {
		return "", err
	}
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	return appcrypto.Encrypt(plain, encKey)
}

// Decrypt reverses Encrypt. Empty ciphertext returns empty string.
func Decrypt(enc, encKey string) (string, error) {
	if strings.TrimSpace(enc) == "" {
		return "", nil
	}
	if err := ValidateKey(encKey); err != nil {
		return "", err
	}
	return appcrypto.Decrypt(enc, encKey)
}

// DecryptOrLegacy returns decrypted ciphertext or legacy plaintext when enc is empty.
func DecryptOrLegacy(enc, legacy, encKey string) (string, error) {
	if strings.TrimSpace(enc) != "" {
		return Decrypt(enc, encKey)
	}
	return strings.TrimSpace(legacy), nil
}

// NormalizePhone keeps digits only for phone blind indexes.
func NormalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeName lowercases and collapses whitespace for name blind indexes.
func NormalizeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// Placeholder is stored in legacy plaintext columns after encryption migration.
const Placeholder = "\u2022"
