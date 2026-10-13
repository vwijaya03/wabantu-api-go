package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	firebaseAudience = "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit"
	googleTokenURL   = "https://oauth2.googleapis.com/token"
	datastoreScope   = "https://www.googleapis.com/auth/datastore"
	CustomTokenTTL   = time.Hour // Firebase maximum
)

// MintCustomToken signs a Firebase Auth custom token (RS256) for uid with developer claims.
func MintCustomToken(cfg *Config, uid string, claims map[string]any, now time.Time) (string, error) {
	if cfg == nil {
		return "", ErrNotConfigured
	}
	if uid == "" || len(uid) > 128 {
		return "", errors.New("realtime: invalid uid")
	}
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": cfg.ClientEmail, "sub": cfg.ClientEmail, "aud": firebaseAudience,
		"iat": now.Unix(), "exp": now.Add(CustomTokenTTL).Unix(),
		"uid": uid, "claims": claims,
	})
	return t.SignedString(cfg.Key)
}

// tokenSource exchanges a service-account JWT for an OAuth access token (cached).
type tokenSource struct {
	cfg      *Config
	tokenURL string
	scope    string // default datastoreScope
	hc       *http.Client
	mu       sync.Mutex
	tok      string
	exp      time.Time
}

func (s *tokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok != "" && time.Until(s.exp) > time.Minute {
		return s.tok, nil
	}
	now := time.Now()
	scope := s.scope
	if scope == "" {
		scope = datastoreScope
	}
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": s.cfg.ClientEmail, "scope": scope, "aud": s.tokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}).SignedString(s.cfg.Key)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("realtime: oauth token http %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &tr); err != nil || tr.AccessToken == "" {
		return "", errors.New("realtime: oauth token decode failed")
	}
	s.tok, s.exp = tr.AccessToken, now.Add(time.Duration(tr.ExpiresIn)*time.Second)
	return s.tok, nil
}
