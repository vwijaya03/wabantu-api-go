package codesim

import appErrs "encore.app/wabantu/shared/errs"

// SessionAccessQuery carries optional client token via query or header.
type SessionAccessQuery struct {
	QueryClientToken  string `query:"clientToken"`
	HeaderClientToken string `header:"X-Codesim-Client-Token"`
}

func (p *SessionAccessQuery) resolvedClientToken() string {
	if p == nil {
		return ""
	}
	return resolveClientToken(p.HeaderClientToken, p.QueryClientToken)
}

func resolveClientToken(values ...string) string {
	for _, v := range values {
		if t := parseClientToken(v); t != "" {
			return t
		}
	}
	return ""
}

func authorizeSessionAccess(row *examSessionRow, callerAccountID, clientToken string) error {
	if callerAccountID != "" && row.AccountID != "" && callerAccountID == row.AccountID {
		return nil
	}
	if clientToken != "" && row.ClientToken != "" && clientToken == row.ClientToken {
		return nil
	}
	// Legacy sessions without owner metadata remain reachable by UUID.
	if row.AccountID == "" && row.ClientToken == "" {
		return nil
	}
	return appErrs.NotFound("session tidak ditemukan")
}
