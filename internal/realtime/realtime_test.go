package realtime

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	return k, string(p)
}

func testCfg(t *testing.T) *Config {
	k, _ := testKey(t)
	return &Config{ProjectID: "demo-proj", ClientEmail: "sa@demo-proj.iam.gserviceaccount.com", Key: k}
}

func TestConfigFrom(t *testing.T) {
	if _, err := ConfigFrom(func(string) string { return "" }); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	_, pemKey := testKey(t)
	sa, _ := json.Marshal(map[string]string{"client_email": "sa@x", "private_key": pemKey})
	env := map[string]string{EnvProjectID: "p", EnvServiceAccount: string(sa)}
	c, err := ConfigFrom(func(k string) string { return env[k] })
	if err != nil || c.ProjectID != "p" || c.ClientEmail != "sa@x" {
		t.Fatal(c, err)
	}
	env[EnvServiceAccount] = `{"client_email":"a","private_key":"nope"}`
	if _, err := ConfigFrom(func(k string) string { return env[k] }); err == nil || strings.Contains(err.Error(), "nope") {
		t.Fatal("must fail without echoing key", err)
	}
}

// fakeWriter records writes; optional error.
type fakeWriter struct {
	mu   sync.Mutex
	docs map[string]map[string]any
	err  error
}

func (f *fakeWriter) Set(_ context.Context, p string, d map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.docs == nil {
		f.docs = map[string]map[string]any{}
	}
	f.docs[p] = d
	return f.err
}

func TestMirrorWritesPaths(t *testing.T) {
	fw := &fakeWriter{}
	m := &Mirror{W: fw}
	m.Message(context.Background(), "t1", "c1", Message{ID: "m1", Direction: "inbound", Type: "image", ImageURL: "/api/v1/inbox/messages/m1/media", CreatedAt: time.Now()})
	m.State(context.Background(), "t1", "c1", map[string]any{"tier": "reseller"}, map[string]any{"status": "processing", "proofCheck": "cocok"}, "o1")
	for _, p := range []string{"tenants/t1/simConversations/c1/messages/m1", "tenants/t1/simConversations/c1", "tenants/t1/orders/o1"} {
		if fw.docs[p] == nil {
			t.Fatalf("missing %s: %v", p, fw.docs)
		}
	}
	if fw.docs["tenants/t1/simConversations/c1/messages/m1"]["imageUrl"] != "/api/v1/inbox/messages/m1/media" {
		t.Fatal("imageUrl")
	}
}

func TestMirrorBestEffortAndNoop(t *testing.T) {
	var logs []string
	m := &Mirror{W: &fakeWriter{err: errors.New("down")}, Logf: func(f string, a ...any) { logs = append(logs, f) }}
	m.Message(context.Background(), "t1", "c1", Message{ID: "m1"}) // must not panic/return error
	if len(logs) == 0 {
		t.Fatal("failure must be logged")
	}
	var nilM *Mirror
	nilM.Message(context.Background(), "t", "c", Message{ID: "x"})
	(&Mirror{}).State(context.Background(), "t", "c", nil, nil, "o")
}

func TestRESTWriter(t *testing.T) {
	var gotPath, gotMask, gotAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMask, gotAuth = r.URL.EscapedPath(), r.URL.Query().Get("updateMask.fieldPaths"), r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if r.Method != http.MethodPatch {
			w.WriteHeader(405)
		}
	}))
	defer srv.Close()
	w := &RESTWriter{BaseURL: srv.URL, Project: "p", HTTP: srv.Client(), Token: func(context.Context) (string, error) { return "tok", nil }}
	err := w.Set(context.Background(), "tenants/t1/orders/o1", map[string]any{"total": 144000.0, "tags": []any{"a"}, "order": map[string]any{"ok": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/projects/p/databases/(default)/documents/tenants/t1/orders/o1") || gotAuth != "Bearer tok" || gotMask == "" {
		t.Fatal(gotPath, gotAuth, gotMask)
	}
	f := body["fields"].(map[string]any)
	if f["total"].(map[string]any)["integerValue"] != "144000" || f["order"].(map[string]any)["mapValue"] == nil {
		t.Fatalf("%v", f)
	}
	for _, bad := range []string{"tenants", "tenants/../orders/x", "tenants/t 1/orders/o", "a/b/c"} {
		if w.Set(context.Background(), bad, nil) == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestRESTWriterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "PERMISSION_DENIED", 403) }))
	defer srv.Close()
	w := &RESTWriter{BaseURL: srv.URL, Project: "p", HTTP: srv.Client()}
	if err := w.Set(context.Background(), "tenants/t/orders/o", map[string]any{"a": 1}); err == nil {
		t.Fatal("want error")
	}
}

func TestTokenSource(t *testing.T) {
	cfg := testCfg(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if _, err := jwt.Parse(r.Form.Get("assertion"), func(*jwt.Token) (any, error) { return &cfg.Key.PublicKey, nil }); err != nil {
			http.Error(w, "bad", 400)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"at","expires_in":3600}`)
	}))
	defer srv.Close()
	ts := &tokenSource{cfg: cfg, tokenURL: srv.URL, hc: srv.Client()}
	for i := 0; i < 2; i++ {
		if tok, err := ts.Token(context.Background()); err != nil || tok != "at" {
			t.Fatal(tok, err)
		}
	}
	if calls != 1 {
		t.Fatal("token not cached", calls)
	}
}

func callToken(sess *Session, cfgFn func() (*Config, error), body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/firebase-token", strings.NewReader(body))
	ServeFirebaseToken(rec, req, sess, cfgFn, func() time.Time { return time.Unix(1_800_000_000, 0) })
	return rec
}

func TestFirebaseTokenIgnoresBodyTenant(t *testing.T) {
	cfg := testCfg(t)
	rec := callToken(&Session{AccountID: "acc1", TenantID: "tenant-A", Role: "owner"},
		func() (*Config, error) { return cfg, nil }, `{"tenantId":"tenant-EVIL","role":"super_admin","uid":"x"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var resp FirebaseTokenResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	tok, err := jwt.Parse(resp.Token, func(*jwt.Token) (any, error) { return &cfg.Key.PublicKey, nil },
		jwt.WithTimeFunc(func() time.Time { return time.Unix(1_800_000_100, 0) }))
	if err != nil {
		t.Fatal(err)
	}
	c := tok.Claims.(jwt.MapClaims)
	cl := c["claims"].(map[string]any)
	if c["uid"] != "acc1" || cl["tenantId"] != "tenant-A" || cl["role"] != "owner" || resp.TenantID != "tenant-A" ||
		c["aud"] != firebaseAudience || c["iss"] != cfg.ClientEmail {
		t.Fatalf("%v %+v", c, resp)
	}
	if strings.Contains(rec.Body.String(), "EVIL") {
		t.Fatal("body tenant leaked")
	}
}

func TestFirebaseTokenRejects(t *testing.T) {
	cfgOK := func() (*Config, error) { return testCfg(t), nil }
	if r := callToken(nil, cfgOK, ""); r.Code != 401 {
		t.Fatal("unauth", r.Code)
	}
	if r := callToken(&Session{AccountID: "a", TenantID: "t", Role: "buyer"}, cfgOK, ""); r.Code != 403 {
		t.Fatal("role", r.Code)
	}
	if r := callToken(&Session{AccountID: "a", Role: "super_admin"}, cfgOK, ""); r.Code != 403 {
		t.Fatal("no tenant", r.Code)
	}
	if r := callToken(&Session{AccountID: "a", TenantID: "t", Role: "staff"}, func() (*Config, error) { return nil, ErrNotConfigured }, ""); r.Code != 503 {
		t.Fatal("unconfigured", r.Code)
	}
}

func TestDefaultMirrorNoopWithoutEnv(t *testing.T) {
	t.Setenv(EnvProjectID, "")
	m := Default(nil)
	if m == nil || m.enabled() {
		t.Fatal("must be no-op without env")
	}
}
