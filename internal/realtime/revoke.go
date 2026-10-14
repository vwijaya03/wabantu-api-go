package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	identityToolkitScope = "https://www.googleapis.com/auth/identitytoolkit"
	// RevokeTimeout caps one best-effort revoke so logout is never held up.
	RevokeTimeout = 2 * time.Second
)

// Revoker revokes all Firebase refresh tokens of one uid (Security M-1, backend part).
type Revoker interface {
	Revoke(ctx context.Context, uid string) error
}

// RESTRevoker calls Identity Toolkit accounts:update with validSince=now, which is what the
// Admin SDK's RevokeRefreshTokens does. Already-issued ID tokens stay valid until they expire
// (max 1h); the frontend must also signOut (M-1).
type RESTRevoker struct {
	BaseURL string // default https://identitytoolkit.googleapis.com/v1
	Project string
	Token   func(ctx context.Context) (string, error)
	HTTP    *http.Client
	Now     func() time.Time
}

func NewRESTRevoker(cfg *Config) *RESTRevoker {
	hc := &http.Client{Timeout: RevokeTimeout}
	ts := &tokenSource{cfg: cfg, tokenURL: googleTokenURL, hc: hc, scope: identityToolkitScope}
	return &RESTRevoker{BaseURL: "https://identitytoolkit.googleapis.com/v1", Project: cfg.ProjectID, Token: ts.Token, HTTP: hc, Now: time.Now}
}

func (r *RESTRevoker) Revoke(ctx context.Context, uid string) error {
	if uid == "" || len(uid) > 128 {
		return errors.New("realtime: invalid uid")
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	body, _ := json.Marshal(map[string]string{"localId": uid, "validSince": strconv.FormatInt(now().Unix(), 10)})
	u := fmt.Sprintf("%s/projects/%s/accounts:update", strings.TrimRight(r.BaseURL, "/"), url.PathEscape(r.Project))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.Token != nil {
		tok, err := r.Token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("firebase revoke: http %d: %.200s", resp.StatusCode, b)
	}
	return nil
}

// DefaultRevoker returns a REST revoker from ENV, or nil (no-op) when
// FIRESTORE_PROJECT_ID / FIREBASE_SERVICE_ACCOUNT_JSON are missing or invalid.
func DefaultRevoker() Revoker {
	cfg, err := EnvConfig()
	if err != nil || cfg == nil {
		return nil
	}
	return NewRESTRevoker(cfg)
}

// RevokeBestEffort revokes uid's refresh tokens with a RevokeTimeout cap, detached from the
// caller's cancellation. nil revoker or empty uid → no-op. Errors are only logged.
func RevokeBestEffort(ctx context.Context, r Revoker, uid string, logf func(string, ...any)) {
	if r == nil || strings.TrimSpace(uid) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RevokeTimeout)
	defer cancel()
	if err := r.Revoke(ctx, uid); err != nil && logf != nil {
		logf("firebase revoke failed", "uid", uid, "err", err)
	}
}

// RevokeAsync runs RevokeBestEffort in a goroutine so the caller (logout) never waits.
func RevokeAsync(ctx context.Context, r Revoker, uid string, logf func(string, ...any)) {
	if r == nil || strings.TrimSpace(uid) == "" {
		return
	}
	go RevokeBestEffort(ctx, r, uid, logf)
}
