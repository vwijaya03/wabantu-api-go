// Package realtime mirrors simulator conversations and order status to Firestore
// (best-effort; Postgres stays the source of truth) and mints Firebase custom tokens so the
// judge UI can listen in real time. It uses the Firestore/OAuth REST APIs with golang-jwt,
// so no Google SDK dependency is added.
//
// Configuration (ENV only; map to Secret Manager on Cloud Run, never commit):
//
//	FIRESTORE_PROJECT_ID            GCP/Firebase project id
//	FIREBASE_SERVICE_ACCOUNT_JSON   service-account key JSON (client_email, private_key)
//
// When either is missing the mirror is a no-op and the token endpoint answers 503.
package realtime

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

const (
	EnvProjectID      = "FIRESTORE_PROJECT_ID"
	EnvServiceAccount = "FIREBASE_SERVICE_ACCOUNT_JSON"
)

var ErrNotConfigured = errors.New("realtime: firestore/firebase not configured")

type Config struct {
	ProjectID   string
	ClientEmail string
	Key         *rsa.PrivateKey
}

type saJSON struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// ConfigFrom reads the config via getenv. Returns ErrNotConfigured when unset.
func ConfigFrom(getenv func(string) string) (*Config, error) {
	project := strings.TrimSpace(getenv(EnvProjectID))
	raw := strings.TrimSpace(getenv(EnvServiceAccount))
	if project == "" || raw == "" {
		return nil, ErrNotConfigured
	}
	var sa saJSON
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return nil, errors.New("realtime: service account JSON invalid")
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("realtime: service account JSON missing client_email/private_key")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(sa.PrivateKey))
	if err != nil {
		return nil, errors.New("realtime: service account private key invalid")
	}
	return &Config{ProjectID: project, ClientEmail: sa.ClientEmail, Key: key}, nil
}

var (
	envOnce sync.Once
	envCfg  *Config
	envErr  error
)

// EnvConfig is ConfigFrom(os.Getenv), parsed once per process.
func EnvConfig() (*Config, error) {
	envOnce.Do(func() { envCfg, envErr = ConfigFrom(os.Getenv) })
	return envCfg, envErr
}
