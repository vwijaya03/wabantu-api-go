package types

// AuthUser is extracted from the JWT/session on every authenticated request.
// Returned by the Encore auth handler and accessible via auth.Data().
type AuthUser struct {
	AccountID    string `json:"accountId"`
	TenantID     string `json:"tenantId"`
	TenantSchema string `json:"tenantSchema"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	Role         string `json:"role"`      // "owner" | "staff" | "super_admin"
	SessionID    string `json:"sessionId"` // needed for logout
}
