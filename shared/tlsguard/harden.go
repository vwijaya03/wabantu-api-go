package tlsguard

import (
	"crypto/tls"
	"net/http"
	"sync"
)

// HardenTLSConfig chains the guard into cfg.VerifyConnection in place and
// returns cfg. An existing VerifyConnection runs first; if it returns nil the
// guard still runs, so it cannot be bypassed. Nothing else in cfg is touched
// (RootCAs, MinVersion, ServerName, InsecureSkipVerify stay as they are).
// A nil cfg yields a new empty config with only the guard set.
func HardenTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		cfg = &tls.Config{} //nolint:gosec // zero config == Go defaults; only VerifyConnection is set
	}
	prev := cfg.VerifyConnection
	if prev == nil {
		cfg.VerifyConnection = VerifyConnection
		return cfg
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if err := prev(cs); err != nil {
			return err
		}
		return VerifyConnection(cs)
	}
	return cfg
}

// HardenTransport hardens t.TLSClientConfig in place (creating it if nil)
// and returns t. Proxy, HTTP/2, timeouts and pools are untouched.
func HardenTransport(t *http.Transport) *http.Transport {
	if t == nil {
		return nil
	}
	t.TLSClientConfig = HardenTLSConfig(t.TLSClientConfig)
	return t
}

var (
	installOnce sync.Once
	installed   *http.Transport
)

// InstallDefaultTransport replaces http.DefaultTransport once with a hardened
// Clone() of it (so http.DefaultClient and every client with a nil Transport
// get the guard). Idempotent. If DefaultTransport is not an *http.Transport
// (replaced by someone else) it is left alone and false is returned.
func InstallDefaultTransport() bool {
	installOnce.Do(func() {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return
		}
		clone := base.Clone()
		HardenTransport(clone)
		installed = clone
		http.DefaultTransport = clone
	})
	return DefaultTransportHooked()
}

// DefaultTransportHooked reports whether http.DefaultTransport is the
// transport installed by InstallDefaultTransport.
func DefaultTransportHooked() bool {
	t, ok := http.DefaultTransport.(*http.Transport)
	return ok && installed != nil && t == installed
}
