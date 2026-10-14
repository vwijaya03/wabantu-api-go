package adminauth

import (
	"strings"
	"time"
)

// Results.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultBlocked = "blocked"
)

// Login reasons (in addition to the guard reasons).
const (
	ReasonOK                 = "ok"
	ReasonInvalidCredentials = "invalid_credentials" // unknown account OR wrong password
	ReasonPortalMismatch     = "portal_mismatch"
	ReasonBadRequest         = "bad_request"
	ReasonTenantInactive     = "tenant_inactive"
	ReasonNoTenant           = "no_tenant"
	ReasonInternal           = "internal_error"
)

// Attempt is one login audit row. It has no field that could carry a password, token or
// secret; free-text fields are bounded.
type Attempt struct {
	At        time.Time `json:"at"`
	Portal    string    `json:"portal"`
	Email     string    `json:"email"`
	AccountID string    `json:"accountId,omitempty"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	Result    string    `json:"result"`
	Reason    string    `json:"reason"`
}

var allowedReasons = map[string]bool{
	ReasonOK: true, ReasonInvalidCredentials: true, ReasonPortalMismatch: true, ReasonBadRequest: true,
	ReasonTenantInactive: true, ReasonNoTenant: true, ReasonInternal: true,
	ReasonRateIP: true, ReasonRateAccount: true, ReasonLockedPair: true, ReasonLockedIP: true, ReasonLockedAcct: true,
	"store_error": true, "bootstrap_secret_invalid": true, "bootstrap_disabled": true, "bootstrap_already_done": true,
	"email_taken": true,
}

// NewAttempt builds a sanitized audit record. Reason must be one of the known codes
// (anything else becomes "other") so no caller can smuggle request data into it.
func NewAttempt(at time.Time, portal, email, accountID, ip, ua, result, reason string) Attempt {
	if !allowedReasons[reason] {
		reason = "other"
	}
	return Attempt{
		At: at.UTC(), Portal: clip(portal, 16), Email: clip(strings.ToLower(strings.TrimSpace(email)), 254),
		AccountID: clip(accountID, 64), IP: clip(ip, 64), UserAgent: clip(ua, 256),
		Result: clip(result, 16), Reason: reason,
	}
}

func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
