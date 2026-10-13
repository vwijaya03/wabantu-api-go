package middleware

import (
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	encoremw "encore.dev/middleware"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/shared/types"
)

// SessionKind separates admin and client sessions: /api/v1/admin/* and tag:super_admin
// endpoints accept ONLY admin-portal sessions; every other authenticated endpoint rejects
// admin sessions; impersonation tokens work only on client endpoints.
//
//encore:middleware global target=all
func SessionKind(req encoremw.Request, next encoremw.Next) encoremw.Response {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil {
		return next(req) // unauthenticated: endpoint's own auth requirement applies
	}
	data := req.Data()
	path, tag := "", false
	if data != nil {
		path = data.Path
		if data.API != nil {
			tag = data.API.Tags.Has("super_admin")
		}
	}
	if err := adminauth.RequestAccess(path, tag, u.SessionKind); err != nil {
		return encoremw.Response{Err: &errs.Error{Code: errs.PermissionDenied, Message: err.Error()}}
	}
	return next(req)
}
