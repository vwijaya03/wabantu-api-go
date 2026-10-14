package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRESTRevokerRequest(t *testing.T) {
	var got map[string]string
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"localId":"acc-1"}`))
	}))
	defer srv.Close()
	r := &RESTRevoker{BaseURL: srv.URL, Project: "demo-proj", HTTP: srv.Client(),
		Token: func(context.Context) (string, error) { return "tok", nil },
		Now:   func() time.Time { return time.Unix(1700000000, 0) }}
	if err := r.Revoke(context.Background(), "acc-1"); err != nil {
		t.Fatal(err)
	}
	if path != "/projects/demo-proj/accounts:update" || auth != "Bearer tok" || got["localId"] != "acc-1" || got["validSince"] != "1700000000" {
		t.Fatalf("%s %s %v", path, auth, got)
	}
	if err := r.Revoke(context.Background(), ""); err == nil {
		t.Fatal("empty uid accepted")
	}
}

func TestRESTRevokerHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", 403) }))
	defer srv.Close()
	r := &RESTRevoker{BaseURL: srv.URL, Project: "p", HTTP: srv.Client()}
	if err := r.Revoke(context.Background(), "u"); err == nil {
		t.Fatal("want error")
	}
}

type fakeRevoker struct {
	mu   sync.Mutex
	uids []string
	err  error
	wait time.Duration
	dl   bool
}

func (f *fakeRevoker) Revoke(ctx context.Context, uid string) error {
	_, f.dl = ctx.Deadline()
	if f.wait > 0 {
		select {
		case <-time.After(f.wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.uids = append(f.uids, uid)
	f.mu.Unlock()
	return f.err
}

func TestRevokeBestEffort(t *testing.T) {
	// nil revoker (not configured) and empty uid are no-ops.
	RevokeBestEffort(context.Background(), nil, "u", nil)
	f := &fakeRevoker{}
	RevokeBestEffort(context.Background(), f, " ", nil)
	if len(f.uids) != 0 {
		t.Fatal("empty uid revoked")
	}
	// Cancelled request ctx still revokes (detached), with a deadline.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RevokeBestEffort(ctx, f, "acc-1", nil)
	if len(f.uids) != 1 || !f.dl {
		t.Fatalf("%v dl=%v", f.uids, f.dl)
	}
	// Errors are logged, not returned.
	var logged bool
	RevokeBestEffort(context.Background(), &fakeRevoker{err: errors.New("x")}, "u", func(string, ...any) { logged = true })
	if !logged {
		t.Fatal("error not logged")
	}
}

func TestRevokeAsyncDoesNotBlock(t *testing.T) {
	f := &fakeRevoker{wait: 300 * time.Millisecond}
	start := time.Now()
	RevokeAsync(context.Background(), f, "acc-1", nil)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("RevokeAsync blocked")
	}
	time.Sleep(500 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.uids) != 1 {
		t.Fatalf("%v", f.uids)
	}
}

func TestDefaultRevokerNoopWithoutEnv(t *testing.T) {
	cfg, err := ConfigFrom(func(string) string { return "" })
	if cfg != nil || !errors.Is(err, ErrNotConfigured) {
		t.Fatal("expected not configured")
	}
}
