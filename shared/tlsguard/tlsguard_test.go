package tlsguard

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exact list from Go CL 851047 / go1.27.3 src/crypto/x509/verify.go.
var goSourceHashes = []string{
	"ebed6475c2c2d80fcb199f3fe8dd6e6b3ce60b77eb15d4e42c3dec0c6f5b5105",
	"49dd353dc2b919a3770645117d0e60c16115e7d1bc259eefe734b00ea434fe5f",
	"e0254c7fcdc265e2f0f8a428dabb75ab4ae039b4872d62f5ee57f0877d5a8a41",
	"5c0660a2b164910d8ff589c18e14552e9e0ec237dcefe2cb736d41a4811f81a0",
	"43bffa19d6ef651451f6841e019931e96f6f2eaeff28ab4b6f1ce4576de2717b",
	"63ecefee431d49523bc1136c4e72d0206a477086caf606cd5cf025686c109cf8",
	"ec4ffa1ad0ac88081314b7aaffef6a91f97cd57291bb5ba686c5415f5eb4f6b6",
	"92c063d67e8f14db71d288e11266ffdaf69cbf115f7bab14f3bc2f68d25e0e30",
}

func TestDenylistMatchesGoSource(t *testing.T) {
	if len(distrustedSPKIHashes) != 8 {
		t.Fatalf("want 8 hashes, got %d", len(distrustedSPKIHashes))
	}
	for _, h := range goSourceHashes {
		if _, ok := distrustedSPKIHashes[h]; !ok {
			t.Errorf("missing %s", h)
		}
	}
}

func readChain(t *testing.T, path string) []*x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	certs, err := parsePEM(b)
	if err != nil {
		t.Fatal(err)
	}
	return certs
}

func pool(certs ...*x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	for _, c := range certs {
		p.AddCert(c)
	}
	return p
}

func emudhraFiles(t *testing.T) []string {
	files, err := filepath.Glob("testdata/chains/*.pem")
	if err != nil || len(files) != 14 {
		t.Fatalf("want 14 eMudhra chains, got %d (%v)", len(files), err)
	}
	return files
}

// Every intermediate in chains.zip must be on the denylist (computed SPKI).
func TestFixtureIntermediatesAreDenylisted(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range emudhraFiles(t) {
		c := readChain(t, f)
		if _, ok := distrustedSPKIHashes[spkiHash(c[1])]; !ok {
			t.Errorf("%s: intermediate %q spki %s not denylisted", f, c[1].Subject.CommonName, spkiHash(c[1]))
		}
		seen[spkiHash(c[1])] = true
	}
	if len(seen) != 6 {
		t.Errorf("want 6 distinct TLS intermediates, got %d", len(seen))
	}
}

func emudhraState(t *testing.T, f string) tls.ConnectionState {
	c := readChain(t, f)
	return tls.ConnectionState{ServerName: "x.test", PeerCertificates: c, VerifiedChains: [][]*x509.Certificate{c}}
}

func TestEnforceRejectsAllEmudhraChainsInclSectigo(t *testing.T) {
	aaa := 0
	for _, f := range emudhraFiles(t) {
		if strings.Contains(f, "-aaa-") {
			aaa++
		}
		if err := verify(ModeEnforce, emudhraState(t, f)); !errors.Is(err, ErrDistrustedChain) {
			t.Errorf("%s: want ErrDistrustedChain, got %v", f, err)
		}
	}
	if aaa != 7 {
		t.Errorf("want 7 Sectigo AAA cross-sign chains, got %d", aaa)
	}
}

func googleState(t *testing.T) tls.ConnectionState {
	certs := readChain(t, "testdata/google-www-chain.pem")
	root := readChain(t, "testdata/gts-root-r4.pem")[0]
	chains, err := certs[0].Verify(x509.VerifyOptions{
		DNSName: "www.google.com", Roots: pool(root), Intermediates: pool(certs[1:]...), CurrentTime: selfCheckTime,
	})
	if err != nil {
		t.Fatalf("google fixture should verify: %v", err)
	}
	return tls.ConnectionState{ServerName: "www.google.com", PeerCertificates: certs, VerifiedChains: chains}
}

func TestNormalChainPassesAllModes(t *testing.T) {
	cs := googleState(t)
	for _, m := range []Mode{ModeOff, ModeLog, ModeEnforce} {
		if err := verify(m, cs); err != nil {
			t.Errorf("mode %s: %v", m, err)
		}
	}
}

func TestModes(t *testing.T) {
	bad := emudhraState(t, "testdata/chains/dv-g3b-aaa-emudhra.we-have-a-problem.com.pem")
	if err := verify(ModeOff, bad); err != nil {
		t.Errorf("off: %v", err)
	}
	if err := verify(ModeLog, bad); err != nil {
		t.Errorf("log must never return error: %v", err)
	}
	if err := verify(ModeEnforce, bad); !errors.Is(err, ErrDistrustedChain) {
		t.Errorf("enforce: %v", err)
	}
}

func TestLogNeverErrors(t *testing.T) {
	for _, f := range emudhraFiles(t) {
		if err := verify(ModeLog, emudhraState(t, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	if err := verify(ModeLog, tls.ConnectionState{}); err != nil {
		t.Fatal(err)
	}
}

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{"": ModeLog, "off": ModeOff, "OFF": ModeOff, "log": ModeLog, " enforce ": ModeEnforce, "Enforce": ModeEnforce, "block": ModeLog, "true": ModeLog}
	for in, want := range cases {
		if got := parseMode(in); got != want {
			t.Errorf("parseMode(%q)=%s want %s", in, got, want)
		}
	}
}

// Multiple chains: Go accepts if ANY chain is clean, rejects if ALL are bad.
func TestEnforceMultipleChains(t *testing.T) {
	a := readChain(t, "testdata/chains/dv-g3b-emudhra.we-have-a-problem.com.pem")
	b := readChain(t, "testdata/chains/dv-g3b-aaa-emudhra.we-have-a-problem.com.pem")
	allBad := tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{a, b}}
	if err := verify(ModeEnforce, allBad); !errors.Is(err, ErrDistrustedChain) {
		t.Errorf("all bad chains must be rejected, got %v", err)
	}
	g := googleState(t)
	mixed := tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{a, g.VerifiedChains[0], b}}
	if err := verify(ModeEnforce, mixed); err != nil {
		t.Errorf("one clean chain -> accept (Go semantics), got %v", err)
	}
	// bad cert at root position / leaf position is also caught (Go checks every cert)
	rev := []*x509.Certificate{a[2], a[1], a[0]}
	if err := verify(ModeEnforce, tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{rev}}); err == nil {
		t.Error("position-independent check expected")
	}
}

func TestNoVerifiedChains(t *testing.T) {
	bad := readChain(t, "testdata/chains/dv-g3b-emudhra.we-have-a-problem.com.pem")
	g := readChain(t, "testdata/google-www-chain.pem")
	// unverified + eMudhra peer cert -> rejected in enforce, allowed (warn) in log
	if err := verify(ModeEnforce, tls.ConnectionState{PeerCertificates: bad}); !errors.Is(err, ErrDistrustedChain) {
		t.Errorf("enforce unverified emudhra: %v", err)
	}
	if err := verify(ModeLog, tls.ConnectionState{PeerCertificates: bad}); err != nil {
		t.Errorf("log: %v", err)
	}
	// unverified clean peer -> allowed (warn), never panics
	if err := verify(ModeEnforce, tls.ConnectionState{PeerCertificates: g}); err != nil {
		t.Errorf("enforce unverified clean: %v", err)
	}
	if err := verify(ModeEnforce, tls.ConnectionState{}); err != nil {
		t.Errorf("empty state: %v", err)
	}
	if err := verify(ModeEnforce, tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{nil}}}); err != nil {
		t.Errorf("nil cert: %v", err)
	}
}

func TestHardenChainsExistingVerifyConnection(t *testing.T) {
	bad := emudhraState(t, "testdata/chains/dv-g3b-emudhra.we-have-a-problem.com.pem")
	called := 0
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "keep.me", VerifyConnection: func(tls.ConnectionState) error { called++; return nil }}
	HardenTLSConfig(cfg)
	if cfg.MinVersion != tls.VersionTLS12 || cfg.ServerName != "keep.me" || cfg.RootCAs != nil || cfg.InsecureSkipVerify {
		t.Fatal("harden must not change other fields")
	}
	// Existing callback returning nil must not bypass the guard; guard uses process mode.
	err := cfg.VerifyConnection(bad)
	if called != 1 {
		t.Fatalf("existing VerifyConnection not called")
	}
	if CurrentMode() == ModeEnforce && err == nil {
		t.Fatal("guard bypassed")
	}
	// Existing error wins.
	sentinel := errors.New("prev")
	cfg2 := HardenTLSConfig(&tls.Config{VerifyConnection: func(tls.ConnectionState) error { return sentinel }}) //nolint:gosec
	if err := cfg2.VerifyConnection(googleState(t)); !errors.Is(err, sentinel) {
		t.Fatalf("want prev error, got %v", err)
	}
}

func TestHardenTransportPreservesSettings(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	tr := HardenTransport(base)
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.VerifyConnection == nil {
		t.Fatal("not hardened")
	}
	if tr.Proxy == nil || !tr.ForceAttemptHTTP2 || tr.TLSHandshakeTimeout == 0 || tr.IdleConnTimeout == 0 {
		t.Fatal("transport settings lost")
	}
	if HardenTransport(nil) != nil {
		t.Fatal("nil in, nil out")
	}
}

func TestInstallDefaultTransportIdempotent(t *testing.T) {
	if CurrentMode() == ModeOff {
		t.Skip("TLSGUARD_MODE=off")
	}
	// startup package (or this call) installs once.
	if !InstallDefaultTransport() {
		t.Fatal("DefaultTransport not hooked")
	}
	first := http.DefaultTransport
	if !InstallDefaultTransport() || http.DefaultTransport != first {
		t.Fatal("not idempotent")
	}
	tr := http.DefaultTransport.(*http.Transport)
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.VerifyConnection == nil {
		t.Fatal("no VerifyConnection on DefaultTransport")
	}
	if tr.TLSClientConfig.RootCAs != nil || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.MinVersion != 0 {
		t.Fatal("TLS trust settings changed")
	}
	if !tr.ForceAttemptHTTP2 || tr.Proxy == nil {
		t.Fatal("HTTP/2 or proxy lost")
	}
}

func TestSelfCheckNeverPanics(t *testing.T) {
	InstallDefaultTransport()
	r := RunSelfCheck()
	if r.GoVersion == "" {
		t.Fatal("no go version")
	}
	allowed := map[string]bool{"PATCHED": true, "VULNERABLE": true, "UNKNOWN": true}
	if !allowed[r.Stdlib] {
		t.Fatalf("bad label %q", r.Stdlib)
	}
	if r.Stdlib == "UNKNOWN" {
		t.Fatalf("self-check could not classify stdlib: %s", r.Detail)
	}
	if CurrentMode() != ModeOff && r.Guard != "ACTIVE" {
		t.Fatalf("guard %s: %s", r.Guard, r.Detail)
	}
	t.Logf("go=%s emudhra_check=%s sysroot=%s tlsguard=%s mode=%s", r.GoVersion, r.Stdlib, r.SysRootEmSignECCG3, r.Guard, r.Mode)
}

// The stdlib label must be honest: compare with a direct Verify of the same input.
func TestStdlibLabelHonest(t *testing.T) {
	c := readChain(t, "fixtures/emudhra-dv-g3b-chain.pem")
	root := readChain(t, "fixtures/root-emsign-ecc-root-ca-g3.pem")[0]
	chains, err := c[0].Verify(x509.VerifyOptions{Roots: pool(root), Intermediates: pool(c[1:]...), CurrentTime: selfCheckTime})
	got := classifyStdlib(chains, err)
	if err == nil && got != "VULNERABLE" {
		t.Fatal("accepted chain must be VULNERABLE")
	}
	if err != nil && got == "VULNERABLE" {
		t.Fatal("rejected chain cannot be VULNERABLE")
	}
	// Sanity: the chain is structurally valid -> with the denylisted intermediate
	// replaced by nothing, rejection is only due to the denylist.
	if got == "PATCHED" {
		var cie x509.CertificateInvalidError
		if !errors.As(err, &cie) || cie.Reason != x509.NoValidChains {
			t.Fatal("PATCHED needs NoValidChains")
		}
	}
	if classifyStdlib(nil, errors.New("boom")) != "UNKNOWN" {
		t.Fatal("other errors must be UNKNOWN")
	}
}
