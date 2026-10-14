package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestCheckPortalRoleSeparation(t *testing.T) {
	cases := []struct {
		portal, role, tenant string
		kind                 string
		ok                   bool
	}{
		{PortalAdmin, "super_admin", "", KindAdmin, true},
		{PortalAdmin, "owner", "t1", "", false},
		{PortalAdmin, "staff", "t1", "", false},
		{PortalAdmin, "super_admin", "t1", "", false}, // tenant-bound super_admin is not a platform admin
		{PortalClient, "super_admin", "", "", false},
		{PortalClient, "owner", "t1", KindClient, true},
		{PortalClient, "staff", "t1", KindClient, true},
		{"other", "owner", "t1", "", false},
	}
	for _, c := range cases {
		k, err := CheckPortal(c.portal, c.role, c.tenant)
		if (err == nil) != c.ok || k != c.kind {
			t.Errorf("%+v: kind=%q err=%v", c, k, err)
		}
		if err != nil && err != ErrInvalidCredentials {
			t.Errorf("portal errors must be the generic credential error, got %v", err)
		}
	}
	if ErrInvalidCredentials.Error() != "Email atau password salah" {
		t.Fatal("generic message changed")
	}
}

func TestLifetimesAndExpiry(t *testing.T) {
	if TokenTTL(KindAdmin) > time.Hour || SessionTTL(KindAdmin) > 2*time.Hour || SessionTTL(KindAdmin) >= SessionTTL(KindClient) {
		t.Fatal("admin lifetimes must be short")
	}
	now := time.Now()
	if CheckAdminExpiry(KindAdmin, now.Add(-119*time.Minute), now) != nil {
		t.Fatal("within cap")
	}
	if CheckAdminExpiry(KindAdmin, now.Add(-121*time.Minute), now) != ErrSessionExpired {
		t.Fatal("past cap must expire")
	}
	if CheckAdminExpiry(KindAdmin, time.Time{}, now) != ErrSessionExpired {
		t.Fatal("missing issuedAt must expire")
	}
	if CheckAdminExpiry(KindClient, now.Add(-72*time.Hour), now) != nil {
		t.Fatal("client not capped here")
	}
}

func TestTokenKindAndCookies(t *testing.T) {
	if CheckTokenKind("", "") != nil || CheckTokenKind("client", "") != nil || CheckTokenKind("admin", "admin") != nil {
		t.Fatal("valid combos")
	}
	for _, c := range [][2]string{{"", "admin"}, {"client", "admin"}, {"admin", ""}, {"admin", "client"}, {"root", "client"}} {
		if CheckTokenKind(c[0], c[1]) != ErrTokenKind {
			t.Fatalf("%v must fail", c)
		}
	}
	if CookieName(KindAdmin) == CookieName(KindClient) || CookieName(KindAdmin) != AdminCookie {
		t.Fatal("cookie names must differ")
	}
}

func TestEndpointAccess(t *testing.T) {
	if EndpointAccess(KindClient, true) != ErrAdminSessionRequired || EndpointAccess("", true) != ErrAdminSessionRequired {
		t.Fatal("client session must be rejected on /admin/*")
	}
	if EndpointAccess(KindImpersonation, true) != ErrAdminSessionRequired {
		t.Fatal("impersonation token must be rejected on /admin/*")
	}
	if EndpointAccess(KindAdmin, true) != nil {
		t.Fatal("admin on admin endpoint")
	}
	if EndpointAccess(KindAdmin, false) != ErrClientSessionRequired {
		t.Fatal("admin session must be rejected on client endpoints")
	}
	if EndpointAccess(KindImpersonation, false) != nil || EndpointAccess(KindClient, false) != nil {
		t.Fatal("client/impersonation on client endpoint")
	}
}

func TestImpersonationTokenShorterAndMarked(t *testing.T) {
	if !(TokenTTL(KindImpersonation) < SessionTTL(KindAdmin)) || !(SessionTTL(KindImpersonation) < SessionTTL(KindAdmin)) {
		t.Fatal("impersonation must expire before the admin session")
	}
	if NormalizeKind(KindImpersonation) != KindImpersonation {
		t.Fatal("marker lost")
	}
	if CheckTokenKind(KindImpersonation, KindAdmin) != ErrTokenKind || CheckTokenKind(KindAdmin, KindImpersonation) != ErrTokenKind ||
		CheckTokenKind(KindImpersonation, KindImpersonation) != nil {
		t.Fatal("impersonation token bound to impersonation session only")
	}
	now := time.Now()
	if CheckAdminExpiry(KindImpersonation, now.Add(-31*time.Minute), now) != ErrSessionExpired ||
		CheckAdminExpiry(KindImpersonation, now.Add(-29*time.Minute), now) != nil {
		t.Fatal("absolute impersonation cap")
	}
	for _, p := range []string{"/api/v1/admin/impersonate/t1", "/api/v1/admin/ai/function-config", "/api/v1/admin/ai/catalog", "/api/v1/admin/stop-impersonation"} {
		if RequestAccess(p, true, KindImpersonation) == nil {
			t.Fatalf("impersonation token accepted at %s", p)
		}
	}
}

func TestImpersonationAuditHasAdminAndTenant(t *testing.T) {
	m := ImpersonationAudit("acc-admin", " Ops@WaBantu.id ", "tenant-9", "grant-1", "sess-2")
	if m["adminAccountId"] != "acc-admin" || m["adminEmail"] != "ops@wabantu.id" || m["tenantId"] != "tenant-9" {
		t.Fatal(m)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(strings.ToLower(string(b)), "token") && !strings.Contains(string(b), "impersonationSessionId") {
		t.Fatal(string(b))
	}
}

// memStore is a CounterStore with an injectable clock.
type memStore struct {
	mu   sync.Mutex
	now  func() time.Time
	vals map[string]int64
	exp  map[string]time.Time
	fail bool
}

func newMem(now func() time.Time) *memStore {
	return &memStore{now: now, vals: map[string]int64{}, exp: map[string]time.Time{}}
}

func (m *memStore) gc(k string) {
	if e, ok := m.exp[k]; ok && !m.now().Before(e) {
		delete(m.vals, k)
		delete(m.exp, k)
	}
}
func (m *memStore) Incr(_ context.Context, k string, w time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return 0, errors.New("redis down")
	}
	m.gc(k)
	if _, ok := m.vals[k]; !ok {
		m.exp[k] = m.now().Add(w)
	}
	m.vals[k]++
	return m.vals[k], nil
}
func (m *memStore) SetLock(_ context.Context, k string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vals[k], m.exp[k] = 1, m.now().Add(ttl)
	return nil
}
func (m *memStore) Locked(_ context.Context, k string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return false, errors.New("redis down")
	}
	m.gc(k)
	return m.vals[k] > 0, nil
}
func (m *memStore) Del(_ context.Context, ks ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range ks {
		delete(m.vals, k)
	}
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestLockoutPairAfterFiveWithinWindow(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	g := Guard{S: newMem(c.now), P: AdminPolicy()}
	ctx := context.Background()
	acct := AccountKey("Admin@WaBantu.id")
	for i := 0; i < 5; i++ {
		ok, _, _ := g.Check(ctx, "1.1.1.1", acct)
		if !ok {
			t.Fatalf("attempt %d blocked early", i)
		}
		c.t = c.t.Add(61 * time.Second) // stay under per-minute rate limits
		_ = g.Fail(ctx, "1.1.1.1", acct)
	}
	if ok, reason, _ := g.Check(ctx, "1.1.1.1", acct); ok || reason != ReasonLockedPair {
		t.Fatalf("want pair lock, got ok=%v %s", ok, reason)
	}
	// Victim on another IP is NOT locked out by the attacker's IP (anti lock-out-of-others).
	if ok, _, _ := g.Check(ctx, "9.9.9.9", acct); !ok {
		t.Fatal("other IP must not be locked")
	}
	c.t = c.t.Add(15*time.Minute + time.Second)
	if ok, _, _ := g.Check(ctx, "1.1.1.1", acct); !ok {
		t.Fatal("lock must expire after 15 minutes")
	}
}

func TestLockoutPerIPAcrossAccounts(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	g := Guard{S: newMem(c.now), P: AdminPolicy()}
	ctx := context.Background()
	for i := 0; i < int(AdminPolicy().IPMax); i++ {
		c.t = c.t.Add(61 * time.Second)
		_ = g.Fail(ctx, "6.6.6.6", AccountKey(strings.Repeat("x", i+1)+"@a.b"))
	}
	if ok, reason, _ := g.Check(ctx, "6.6.6.6", AccountKey("fresh@a.b")); ok || reason != ReasonLockedIP {
		t.Fatalf("IP spraying must lock the IP: ok=%v %s", ok, reason)
	}
}

func TestLockoutPerAccountDistributed(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	g := Guard{S: newMem(c.now), P: AdminPolicy()}
	ctx := context.Background()
	acct := AccountKey("admin@wabantu.id")
	for i := 0; i < int(AdminPolicy().AccountMax); i++ {
		_ = g.Fail(ctx, "10.0.0."+string(rune('a'+i)), acct)
	}
	if ok, reason, _ := g.Check(ctx, "203.0.113.7", acct); ok || reason != ReasonLockedAcct {
		t.Fatalf("distributed guessing must lock account: %v %s", ok, reason)
	}
}

func TestUnknownAndExistingAccountsKeyedAlike(t *testing.T) {
	if AccountKey(" A@B.c ") != AccountKey("a@b.c") || strings.Contains(AccountKey("a@b.c"), "@") {
		t.Fatal("account key must normalize and not contain the email")
	}
}

func TestRateLimits(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	g := Guard{S: newMem(c.now), P: AdminPolicy()}
	ctx := context.Background()
	acct := AccountKey("a@b.c")
	n := 0
	for {
		ok, reason, _ := g.Check(ctx, "1.2.3.4", acct)
		if !ok {
			if reason != ReasonRateAccount || n != int(AdminPolicy().AcctPerMin) {
				t.Fatalf("per-account limit at %d: %s", n, reason)
			}
			break
		}
		n++
	}
	ip := 0
	for i := 0; ; i++ {
		ok, reason, _ := g.Check(ctx, "5.5.5.5", AccountKey(string(rune('a'+i))+"@x.y"))
		if !ok {
			if reason != ReasonRateIP || ip != int(AdminPolicy().IPPerMin) {
				t.Fatalf("per-IP limit at %d: %s", ip, reason)
			}
			break
		}
		ip++
	}
	c.t = c.t.Add(time.Minute + time.Second)
	if ok, _, _ := g.Check(ctx, "1.2.3.4", acct); !ok {
		t.Fatal("rate window must reset")
	}
}

func TestGuardStoreFailure(t *testing.T) {
	m := newMem(time.Now)
	m.fail = true
	if ok, _, err := (Guard{S: m, P: AdminPolicy()}).Check(context.Background(), "ip", "a"); ok || err == nil {
		t.Fatal("admin portal must fail closed")
	}
	if ok, _, err := (Guard{S: m, P: ClientPolicy()}).Check(context.Background(), "ip", "a"); !ok || err == nil {
		t.Fatal("client portal fails open (availability) but reports the error")
	}
}

func TestSuccessClearsPairOnly(t *testing.T) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	m := newMem(c.now)
	g := Guard{S: m, P: AdminPolicy()}
	ctx := context.Background()
	acct := AccountKey("a@b.c")
	for i := 0; i < 4; i++ {
		_ = g.Fail(ctx, "1.1.1.1", acct)
	}
	_ = g.Success(ctx, "1.1.1.1", acct)
	_ = g.Fail(ctx, "1.1.1.1", acct)
	if ok, _, _ := g.Check(ctx, "1.1.1.1", acct); !ok {
		t.Fatal("pair counter must reset on success")
	}
	if m.vals["login:admin:fail:acct:"+acct] != 5 {
		t.Fatal("account counter must not reset on success")
	}
}

func TestAuditAttemptHasNoSecrets(t *testing.T) {
	pw, tok := "S3cr3t-P@ssw0rd!", "eyJhbGciOiJIUzI1NiJ9.payload.sig"
	a := NewAttempt(time.Now(), PortalAdmin, " Admin@WaBantu.ID ", "acc1", "1.2.3.4",
		"Mozilla/5.0\x00\n"+strings.Repeat("x", 500), ResultFailure, "password="+pw+" token="+tok)
	b, _ := json.Marshal(a)
	s := string(b)
	if strings.Contains(s, pw) || strings.Contains(s, tok) || strings.Contains(strings.ToLower(s), "password") {
		t.Fatalf("audit leaked: %s", s)
	}
	if a.Reason != "other" || a.Email != "admin@wabantu.id" || len(a.UserAgent) != 256 || strings.ContainsAny(a.UserAgent, "\x00\n") {
		t.Fatalf("%+v", a)
	}
	for _, f := range []string{"at", "portal", "email", "ip", "userAgent", "result", "reason"} {
		if !strings.Contains(s, `"`+f+`"`) {
			t.Fatalf("missing %s", f)
		}
	}
}

func TestBootstrapSecret(t *testing.T) {
	good := strings.Repeat("k", 40)
	if CheckBootstrapSecret("", "anything") != ErrBootstrapDisabled || CheckBootstrapSecret("   ", "") != ErrBootstrapDisabled ||
		CheckBootstrapSecret("short-secret", "short-secret") != ErrBootstrapDisabled {
		t.Fatal("unset/empty/short must disable")
	}
	if CheckBootstrapSecret(good, "") != ErrBootstrapSecret || CheckBootstrapSecret(good, good+"x") != ErrBootstrapSecret ||
		CheckBootstrapSecret(good, strings.Repeat("k", 39)) != ErrBootstrapSecret {
		t.Fatal("wrong secret must fail")
	}
	if CheckBootstrapSecret(good, " "+good+" ") != nil {
		t.Fatal("right secret")
	}
}

type fakeBoot struct {
	mu      *sync.Mutex // shared "database"
	admins  *int
	emails  map[string]bool
	locked  bool
	pending int
	steps   []string
}

func (f *fakeBoot) Lock(context.Context) error { f.mu.Lock(); f.locked = true; return nil }
func (f *fakeBoot) PlatformAdminExists(context.Context) (bool, error) {
	return *f.admins > 0, nil
}
func (f *fakeBoot) EmailTaken(_ context.Context, e string) (bool, error) { return f.emails[e], nil }
func (f *fakeBoot) CreateAdmin(_ context.Context, e, _, pw string) (string, error) {
	if strings.Contains(e, pw) {
		return "", errors.New("never")
	}
	f.pending++
	return "acc-1", nil
}
func (f *fakeBoot) unlock() {
	if f.locked {
		f.locked = false
		f.mu.Unlock()
	}
}
func (f *fakeBoot) Commit() error   { *f.admins += f.pending; f.pending = 0; f.unlock(); return nil }
func (f *fakeBoot) Rollback() error { f.pending = 0; f.unlock(); return nil }

func TestBootstrapOneTime(t *testing.T) {
	var mu sync.Mutex
	admins := 0
	in := BootstrapInput{Email: "ops@wabantu.id", Name: "Ops", Password: "a-very-long-password"}
	newTx := func() *fakeBoot { return &fakeBoot{mu: &mu, admins: &admins, emails: map[string]bool{}} }
	if id, err := RunBootstrap(context.Background(), newTx(), in); err != nil || id == "" {
		t.Fatal(id, err)
	}
	if _, err := RunBootstrap(context.Background(), newTx(), in); err != ErrAlreadyBootstrapped {
		t.Fatal("second bootstrap must 409", err)
	}
	if admins != 1 {
		t.Fatal(admins)
	}
}

func TestBootstrapConcurrentCreatesOne(t *testing.T) {
	var mu sync.Mutex
	admins := 0
	in := BootstrapInput{Email: "ops@wabantu.id", Name: "Ops", Password: "a-very-long-password"}
	var wg sync.WaitGroup
	var okN, conflictN int
	var cmu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := RunBootstrap(context.Background(), &fakeBoot{mu: &mu, admins: &admins, emails: map[string]bool{}}, in)
			cmu.Lock()
			defer cmu.Unlock()
			if err == nil {
				okN++
			} else if err == ErrAlreadyBootstrapped {
				conflictN++
			}
		}()
	}
	wg.Wait()
	if okN != 1 || conflictN != 15 || admins != 1 {
		t.Fatalf("ok=%d conflict=%d admins=%d", okN, conflictN, admins)
	}
}

func TestBootstrapValidationAndEmailTaken(t *testing.T) {
	var mu sync.Mutex
	admins := 0
	if _, err := RunBootstrap(context.Background(), &fakeBoot{mu: &mu, admins: &admins}, BootstrapInput{Email: "a@b", Name: "x", Password: "short"}); err != ErrBootstrapInput {
		t.Fatal(err)
	}
	tx := &fakeBoot{mu: &mu, admins: &admins, emails: map[string]bool{"a@b.c": true}}
	if _, err := RunBootstrap(context.Background(), tx, BootstrapInput{Email: "a@b.c", Name: "x", Password: "long-enough-pw"}); err != ErrEmailTaken || admins != 0 {
		t.Fatal(err)
	}
}

func TestDummyHashIsRealBcrypt(t *testing.T) {
	h := DummyHash(bcrypt.MinCost)
	cost, err := bcrypt.Cost(h)
	if err != nil || cost != bcrypt.MinCost {
		t.Fatal("dummy must be a valid bcrypt hash so unknown accounts cost a full compare", err)
	}
	if bcrypt.CompareHashAndPassword(h, []byte("x")) == nil {
		t.Fatal()
	}
}

func TestRequestAccess(t *testing.T) {
	cases := []struct {
		path string
		tag  bool
		kind string
		want error
	}{
		{"/api/v1/admin/ai/catalog", true, KindClient, ErrAdminSessionRequired},
		{"/api/v1/admin/ai/catalog", false, KindClient, ErrAdminSessionRequired},
		{"/api/v1/admin/tenants", true, KindAdmin, nil},
		{"/api/v1/tenant-access/requests", true, KindClient, ErrAdminSessionRequired},
		{"/api/v1/orders", false, KindAdmin, ErrClientSessionRequired},
		{"/api/v1/orders", false, KindImpersonation, nil},
		{"/api/v1/orders", false, KindClient, nil},
		{"/api/v1/internal/kb/triage-patch", false, KindAdmin, nil},
	}
	for _, c := range cases {
		if got := RequestAccess(c.path, c.tag, c.kind); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

func TestMeAccessAllKinds(t *testing.T) {
	cases := []struct {
		ep, kind string
		ok       bool
	}{
		{MeAdmin, KindAdmin, true},
		{MeAdmin, KindClient, false},
		{MeAdmin, "", false},
		{MeAdmin, KindImpersonation, false},
		{MeClient, KindAdmin, false},
		{MeClient, KindClient, true},
		{MeClient, "", true},
		{MeClient, KindImpersonation, true},
		{"/other", KindAdmin, false},
	}
	for _, c := range cases {
		if err := MeAccess(c.ep, c.kind); (err == nil) != c.ok {
			t.Errorf("%s %q: %v", c.ep, c.kind, err)
		}
	}
	// An expired admin session never reaches MeAccess: CheckAdminExpiry rejects it first.
	now := time.Now()
	if CheckAdminExpiry(KindAdmin, now.Add(-3*time.Hour), now) == nil {
		t.Fatal("expired admin must be rejected")
	}
}
