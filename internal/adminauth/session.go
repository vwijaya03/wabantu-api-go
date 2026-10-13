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
	// KindImpersonation is a short-lived token minted by POST /api/v1/admin/impersonate/:tenantId.
	// It acts as the tenant on client endpoints and is REJECTED at /admin/*.
	KindImpersonation = "impersonation"
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
	// Impersonation tokens/sessions live shorter than the admin session.
	ImpersonationTTL = 30 * time.Minute
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
	switch kind {
	case KindAdmin, KindImpersonation:
		return kind
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
	switch NormalizeKind(kind) {
	case KindAdmin:
		return AdminTokenTTL
	case KindImpersonation:
		return ImpersonationTTL
	}
	return ClientTokenTTL
}

func SessionTTL(kind string) time.Duration {
	switch NormalizeKind(kind) {
	case KindAdmin:
		return AdminSessionTTL
	case KindImpersonation:
		return ImpersonationTTL
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
	if claimTyp != "" && claimTyp != KindClient && claimTyp != KindAdmin && claimTyp != KindImpersonation {
		return ErrTokenKind
	}
	return nil
}

// CheckAdminExpiry enforces the absolute admin session lifetime (Redis TTL is the first
// line; this guards against a TTL being extended by any code path).
func CheckAdminExpiry(kind string, issuedAt, now time.Time) error {
	k := NormalizeKind(kind)
	if k == KindClient {
		return nil
	}
	if issuedAt.IsZero() || now.Sub(issuedAt) > SessionTTL(k) {
		return ErrSessionExpired
	}
	return nil
}

var (
	ErrAdminSessionRequired  = errors.New("admin session required")
	ErrClientSessionRequired = errors.New("client session required")
)

// EndpointAccess: admin endpoints (tag:super_admin, /api/v1/admin/*) accept ONLY admin
// sessions (client and impersonation tokens rejected). Client endpoints reject admin
// sessions; an admin acts as a tenant only through a separate impersonation token.
func EndpointAccess(kind string, adminEndpoint bool) error {
	k := NormalizeKind(kind)
	if adminEndpoint {
		if k != KindAdmin {
			return ErrAdminSessionRequired
		}
		return nil
	}
	if k == KindAdmin {
		return ErrClientSessionRequired
	}
	return nil
}

// ImpersonationAudit is the audit payload for impersonation start/stop: who (admin id +
// email) impersonated which tenant, via which grant/session. No token.
func ImpersonationAudit(adminID, adminEmail, tenantID, grantID, impSessionID string) map[string]string {
	return map[string]string{
		"adminAccountId": adminID, "adminEmail": strings.ToLower(strings.TrimSpace(adminEmail)),
		"tenantId": tenantID, "grantId": grantID, "impersonationSessionId": impSessionID,
	}
}

// CookieName for a session kind.
func CookieName(kind string) string {
	if NormalizeKind(kind) != KindClient {
		return AdminCookie
	}
	return ClientCookie
}

// AdminPathPrefix is the admin console API namespace.
const AdminPathPrefix = "/api/v1/admin/"

// IsAdminEndpoint: tagged super_admin OR under /api/v1/admin/.
func IsAdminEndpoint(path string, superAdminTag bool) bool {
	return superAdminTag || strings.HasPrefix(path, AdminPathPrefix) || path == strings.TrimSuffix(AdminPathPrefix, "/")
}

// RequestAccess is the global middleware decision. Private service-to-service endpoints
// (/api/v1/internal/*, not externally routable) are exempt so admin handlers can call them.
func RequestAccess(path string, superAdminTag bool, kind string) error {
	admin := IsAdminEndpoint(path, superAdminTag)
	if !admin && (strings.HasPrefix(path, "/api/v1/internal/") || PrivatePaths[path]) {
		return nil
	}
	return EndpointAccess(kind, admin)
}

// PrivatePaths are `//encore:api private` endpoints outside /api/v1/internal/ (not externally
// routable; reached only service-to-service, e.g. audit.Log during an admin request).
// TestPrivatePathsComplete keeps this in sync with the source.
var PrivatePaths = map[string]bool{
	"/api/v1/audit/log":                     true,
	"/api/v1/billing/activate-paid-invoice": true,
	"/api/v1/finance/recurring/process":     true,
	"/api/v1/leads/capture":                 true,
	"/api/v1/usage/record":                  true,
	"/api/v1/usage/reset":                   true,
	"/internal/apitest/ping":                true,
}
