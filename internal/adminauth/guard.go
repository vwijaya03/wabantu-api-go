package adminauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// CounterStore is a minimal Redis-like store (implemented over go-redis in package auth).
type CounterStore interface {
	// Incr increments key and sets its expiry to window on first increment.
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
	SetLock(ctx context.Context, key string, ttl time.Duration) error
	Locked(ctx context.Context, key string) (bool, error)
	Del(ctx context.Context, keys ...string) error
}

// Policy for one portal. Failures are counted per (account,IP) pair, per IP and per account:
//   - pair:    PairMax failures in Window → that IP is locked for that account (LockFor)
//   - IP:      IPMax failures (any accounts) in Window → IP locked for this portal
//   - account: AccountMax failures from any IPs in Window → account locked (distributed
//     guessing). AccountMax is deliberately much higher than PairMax so one attacker IP
//     cannot lock a victim out (the victim's own IP keeps its separate pair budget).
//
// Rate limits (attempts, not failures) per IP and per account per minute run before any
// password check.
type Policy struct {
	Portal               string
	PairMax, IPMax       int64
	AccountMax           int64
	Window, LockFor      time.Duration
	IPPerMin, AcctPerMin int64
	// FailClosed denies on store (Redis) errors. Always on for PortalAdmin.
	FailClosed bool
}

func AdminPolicy() Policy {
	return Policy{Portal: PortalAdmin, PairMax: 5, IPMax: 10, AccountMax: 20,
		Window: 15 * time.Minute, LockFor: 15 * time.Minute, IPPerMin: 10, AcctPerMin: 5}
}

// BootstrapPolicy guards POST /internal/platform-admin/bootstrap. Fail-closed (Security L-1):
// without a working Redis the endpoint answers 503 instead of skipping the rate limit.
func BootstrapPolicy() Policy {
	return Policy{Portal: PortalBootstrap, PairMax: 5, IPMax: 5, AccountMax: 20,
		Window: 15 * time.Minute, LockFor: 15 * time.Minute, IPPerMin: 5, AcctPerMin: 5, FailClosed: true}
}

// ErrStoreUnavailable is returned by Check when the counter store is missing (nil).
var ErrStoreUnavailable = errors.New("adminauth: counter store unavailable")

// CheckStatus maps a Check result to HTTP: 0 = proceed, 503 = store unavailable on a
// fail-closed policy, 429 = rate limited/locked.
func (g Guard) CheckStatus(ok bool, err error) int {
	switch {
	case ok:
		return 0
	case err != nil && g.failClosed():
		return http.StatusServiceUnavailable
	default:
		return http.StatusTooManyRequests
	}
}

func (g Guard) failClosed() bool { return g.P.FailClosed || g.P.Portal == PortalAdmin }

func ClientPolicy() Policy {
	return Policy{Portal: PortalClient, PairMax: 5, IPMax: 30, AccountMax: 50,
		Window: 15 * time.Minute, LockFor: 15 * time.Minute, IPPerMin: 20, AcctPerMin: 10}
}

// Reasons (audit + metrics; never shown verbatim to the client).
const (
	ReasonRateIP      = "rate_limited_ip"
	ReasonRateAccount = "rate_limited_account"
	ReasonLockedPair  = "locked_account_ip"
	ReasonLockedIP    = "locked_ip"
	ReasonLockedAcct  = "locked_account"
)

type Guard struct {
	S CounterStore
	P Policy
}

// AccountKey hashes the normalized email so Redis keys never contain the address and
// unknown/existing accounts are keyed identically.
func AccountKey(email string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(h[:12])
}

func (g Guard) k(parts ...string) string {
	return "login:" + g.P.Portal + ":" + strings.Join(parts, ":")
}

// Check runs before the password check. ok=false → respond 429 with the generic message.
// Store errors fail closed for the admin portal and open for the client portal.
func (g Guard) Check(ctx context.Context, ip, acct string) (ok bool, reason string, err error) {
	failClosed := g.failClosed()
	if g.S == nil {
		return !failClosed, "store_error", ErrStoreUnavailable
	}
	for _, l := range []struct{ key, reason string }{
		{g.k("lock", "pair", acct, ip), ReasonLockedPair},
		{g.k("lock", "ip", ip), ReasonLockedIP},
		{g.k("lock", "acct", acct), ReasonLockedAcct},
	} {
		locked, err := g.S.Locked(ctx, l.key)
		if err != nil {
			return !failClosed, "store_error", err
		}
		if locked {
			return false, l.reason, nil
		}
	}
	n, err := g.S.Incr(ctx, g.k("rate", "ip", ip), time.Minute)
	if err != nil {
		return !failClosed, "store_error", err
	}
	if n > g.P.IPPerMin {
		return false, ReasonRateIP, nil
	}
	n, err = g.S.Incr(ctx, g.k("rate", "acct", acct), time.Minute)
	if err != nil {
		return !failClosed, "store_error", err
	}
	if n > g.P.AcctPerMin {
		return false, ReasonRateAccount, nil
	}
	return true, "", nil
}

// Fail records a failed attempt (unknown account, wrong password, wrong portal alike) and
// sets locks when thresholds are crossed.
func (g Guard) Fail(ctx context.Context, ip, acct string) error {
	if g.S == nil {
		return ErrStoreUnavailable
	}
	for _, c := range []struct {
		counter, lock string
		max           int64
	}{
		{g.k("fail", "pair", acct, ip), g.k("lock", "pair", acct, ip), g.P.PairMax},
		{g.k("fail", "ip", ip), g.k("lock", "ip", ip), g.P.IPMax},
		{g.k("fail", "acct", acct), g.k("lock", "acct", acct), g.P.AccountMax},
	} {
		n, err := g.S.Incr(ctx, c.counter, g.P.Window)
		if err != nil {
			return err
		}
		if n >= c.max {
			if err := g.S.SetLock(ctx, c.lock, g.P.LockFor); err != nil {
				return err
			}
		}
	}
	return nil
}

// Success clears the pair counter (not IP/account counters: a success must not reset a
// distributed attack's budget).
func (g Guard) Success(ctx context.Context, ip, acct string) error {
	if g.S == nil {
		return ErrStoreUnavailable
	}
	return g.S.Del(ctx, g.k("fail", "pair", acct, ip))
}
