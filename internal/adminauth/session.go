// Package adminauth holds the runtime-free security logic for split client/admin login:
// portal (role) separation, session kinds and lifetimes, endpoint access, login lockout and
// rate limits, the login audit record and the one-time platform-admin bootstrap. Package
// auth wires it to Redis/Postgres; everything here is unit-testable without Encore.
package adminauth

import (
	"errors"
	"strings"
	"time"
)

// Session kinds. The kind is stored server-side in the Redis session (authoritative) and
// mirrored in the JWT "typ" claim; both must agree.
const (
	KindClient = "client"
	KindAdmin  = "admin"
)

// Cookie names differ so a browser never sends one portal's token to the other.
const (
	ClientCookie = "wabantu_at"
	AdminCookie  = "wabantu_admin_at"
)

// Lifetimes. Admin sessions have an absolute cap (never extended by reauth/touch).
const (
	ClientTokenTTL   = 60 * time.Minute
	ClientSessionTTL = 7 * 24 * time.Hour
	AdminTokenTTL    = 60 * time.Minute
	AdminSessionTTL  = 2 * time.Hour
)

// Portals (login endpoints).
const (
	PortalClient = "client" // POST /api/v1/auth/login
	PortalAdmin  = "admin"  // POST /api/v1/platform-admin/login
)

const RoleSuperAdmin = "super_admin"

// ErrInvalidCredentials is the ONLY error a login reveals for unknown account, wrong
// password or wrong portal (no account enumeration).
var ErrInvalidCredentials = errors.New("Email atau password salah")

// NormalizeKind maps legacy sessions (no kind) to client.
func NormalizeKind(kind string) string {
	if kind == KindAdmin {
		return KindAdmin
	}
	return KindClient
}

// CheckPortal enforces role separation after the password was verified: platform admins
// (super_admin without tenant) only via the admin portal, everyone else only via the client
// portal. Returns the session kind to create.
func CheckPortal(portal, role, tenantID string) (string, error) {
	isAdmin := role == RoleSuperAdmin
	switch portal {
	case PortalAdmin:
		if isAdmin && strings.TrimSpace(tenantID) == "" {
			return KindAdmin, nil
		}
	case PortalClient:
		if !isAdmin {
			return KindClient, nil
		}
	}
	return "", ErrInvalidCredentials
}

// TokenTTL / SessionTTL per kind.
func TokenTTL(kind string) time.Duration {
	if NormalizeKind(kind) == KindAdmin {
		return AdminTokenTTL
	}
	return ClientTokenTTL
}

func SessionTTL(kind string) time.Duration {
	if NormalizeKind(kind) == KindAdmin {
		return AdminSessionTTL
	}
	return ClientSessionTTL
}

var (
	ErrTokenKind      = errors.New("token type does not match session")
	ErrSessionExpired = errors.New("session expired")
)

// CheckTokenKind requires the JWT typ claim to match the server-side session kind.
// Legacy client tokens have no typ claim.
func CheckTokenKind(claimTyp, sessionKind string) error {
	if NormalizeKind(claimTyp) != NormalizeKind(sessionKind) {
		return ErrTokenKind
	}
	if claimTyp != "" && claimTyp != KindClient && claimTyp != KindAdmin {
		return ErrTokenKind
	}
	return nil
}

// CheckAdminExpiry enforces the absolute admin session lifetime (Redis TTL is the first
// line; this guards against a TTL being extended by any code path).
func CheckAdminExpiry(kind string, issuedAt, now time.Time) error {
	if NormalizeKind(kind) != KindAdmin {
		return nil
	}
	if issuedAt.IsZero() || now.Sub(issuedAt) > AdminSessionTTL {
		return ErrSessionExpired
	}
	return nil
}

var (
	ErrAdminSessionRequired  = errors.New("admin session required")
	ErrClientSessionRequired = errors.New("client session required")
)

// EndpointAccess: admin endpoints (tag:super_admin) accept ONLY admin sessions; client
// endpoints reject admin sessions unless the admin is impersonating a tenant through an
// approved tenant-access grant (explicit, consented, audited act-as).
func EndpointAccess(kind string, impersonating, adminEndpoint bool) error {
	k := NormalizeKind(kind)
	if adminEndpoint {
		if k != KindAdmin {
			return ErrAdminSessionRequired
		}
		return nil
	}
	if k == KindAdmin && !impersonating {
		return ErrClientSessionRequired
	}
	return nil
}

// CookieName for a session kind.
func CookieName(kind string) string {
	if NormalizeKind(kind) == KindAdmin {
		return AdminCookie
	}
	return ClientCookie
}
