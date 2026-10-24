// Package tlsguard rejects TLS server chains that go through one of the eight
// compromised eMudhra intermediates (GO-2026-6809), independent of which Go
// toolchain or CA bundle the binary was built with.
//
// It mirrors the Go stdlib fix (CL 851047): Certificate.Verify drops every
// candidate chain that contains a denylisted SPKI and fails when none remain.
// VerifyConnection applies the same filter to tls.ConnectionState.VerifiedChains.
// It never changes RootCAs, MinVersion, ServerName or InsecureSkipVerify.
//
// Mode is read once from TLSGUARD_MODE (off|log|enforce, default log).
package tlsguard

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"sync"

	"encore.dev/rlog"
)

// Mode controls what the guard does with a denylisted chain.
type Mode string

const (
	ModeOff     Mode = "off"     // guard does nothing
	ModeLog     Mode = "log"     // warn, let the connection proceed (default)
	ModeEnforce Mode = "enforce" // reject the handshake
)

// EnvVar is the environment variable read once at process start.
const EnvVar = "TLSGUARD_MODE"

// ErrDistrustedChain is returned in enforce mode.
var ErrDistrustedChain = errors.New("tlsguard: server certificate chain uses a distrusted eMudhra intermediate (GO-2026-6809)")

var (
	modeOnce sync.Once
	mode     Mode
)

// parseMode maps an env value to a Mode. Unknown/empty values fall back to log.
func parseMode(v string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(v))) {
	case ModeOff:
		return ModeOff
	case ModeEnforce:
		return ModeEnforce
	default:
		return ModeLog
	}
}

// CurrentMode returns the process-wide mode (read once from TLSGUARD_MODE).
// It cannot be overridden per client.
func CurrentMode() Mode {
	modeOnce.Do(func() {
		raw := os.Getenv(EnvVar)
		mode = parseMode(raw)
		if raw != "" && string(mode) != strings.ToLower(strings.TrimSpace(raw)) {
			logWarn("tlsguard: unknown TLSGUARD_MODE, using log", "value", raw)
		}
	})
	return mode
}

// VerifyConnection is a tls.Config.VerifyConnection callback using the
// process-wide mode.
func VerifyConnection(cs tls.ConnectionState) error {
	return verify(CurrentMode(), cs)
}

// verify implements the guard for an explicit mode (tests call it directly).
func verify(m Mode, cs tls.ConnectionState) error {
	if m == ModeOff {
		return nil
	}

	if len(cs.VerifiedChains) == 0 {
		// No verified chains: the caller disabled verification
		// (InsecureSkipVerify) or uses its own VerifyPeerCertificate. Go's patch
		// lives inside Verify, so it would not apply here either. We do not
		// treat this as clean: always warn, and in enforce mode reject only if
		// a presented certificate itself carries a denylisted SPKI (stricter
		// than Go, never rejects an unverified peer that is not eMudhra).
		bad := distrustedCert(cs.PeerCertificates)
		if bad != nil {
			logWarn("tlsguard: unverified connection presents distrusted eMudhra certificate",
				"host", cs.ServerName, "ca", bad.Subject.CommonName, "spki_sha256", spkiHash(bad), "mode", string(m))
			if m == ModeEnforce {
				return ErrDistrustedChain
			}
			return nil
		}
		logWarn("tlsguard: connection has no verified chains (verification disabled?)",
			"host", cs.ServerName, "mode", string(m))
		return nil
	}

	// Go semantics: keep only chains with no denylisted SPKI; fail if none remain.
	var firstBad *x509.Certificate
	for _, chain := range cs.VerifiedChains {
		bad := distrustedCert(chain)
		if bad == nil {
			return nil // at least one clean chain -> accepted, like Go
		}
		if firstBad == nil {
			firstBad = bad
		}
	}

	logWarn("tlsguard: all verified chains use a distrusted eMudhra intermediate",
		"host", cs.ServerName, "ca", firstBad.Subject.CommonName, "spki_sha256", spkiHash(firstBad),
		"chains", len(cs.VerifiedChains), "mode", string(m))
	if m == ModeEnforce {
		return ErrDistrustedChain
	}
	return nil
}

// logWarn never panics (rlog panics outside an Encore runtime, e.g. plain
// `go test`); a panic inside a TLS handshake callback would crash the process.
func logWarn(msg string, kv ...any) {
	defer func() { _ = recover() }()
	rlog.Warn(msg, kv...)
}
