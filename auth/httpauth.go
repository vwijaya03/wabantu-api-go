package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"

	"encore.app/wabantu/internal/adminauth"
	"encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

// AuthenticateHTTP validates JWT from Authorization Bearer, cookie, or access_token
// query on the inbox SSE stream only.
func AuthenticateHTTP(ctx context.Context, r *http.Request) (*types.AuthUser, error) {
	token := extractBearerOrCookie(r)
	if token == "" {
		return nil, errs.Unauthenticated("not authenticated")
	}

	accountID, sessionID, err := parseJWT(token)
	if err != nil {
		return nil, errs.Unauthenticated("invalid token")
	}

	sess, err := getSession(ctx, accountID, sessionID)
	if err != nil {
		return nil, errs.Internal("session lookup failed")
	}
	if sess == nil {
		return nil, errs.Unauthenticated("session expired")
	}
	if err := reconcileSessionTenant(ctx, sess); err != nil {
		return nil, errs.Unauthenticated("session invalid")
	}
	if err := reconcileImpersonationGrant(ctx, accountID, sessionID, sess); err != nil {
		return nil, errs.Unauthenticated("session invalid")
	}

	if err := checkSessionKind(token, sess); err != nil {
		return nil, errs.Unauthenticated("session invalid")
	}

	return buildAuthUser(sess, sessionID), nil
}

// RedisClient returns the shared Redis client used for sessions and realtime.
func RedisClient() *redis.Client {
	return getRedis()
}

func extractBearerOrCookie(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if parts := strings.SplitN(h, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if c, err := r.Cookie(adminauth.AdminCookie); err == nil && c.Value != "" {
		return c.Value
	}
	// Query tokens are for EventSource only. Other routes ignore ?access_token=
	// so a leaked stream URL cannot authenticate REST calls.
	if accessTokenQueryAllowed(r.URL.Path) {
		if t := strings.TrimSpace(r.URL.Query().Get("access_token")); t != "" {
			return t
		}
	}
	return ""
}

// accessTokenQueryAllowed is true only for the inbox SSE stream.
func accessTokenQueryAllowed(path string) bool {
	path = strings.TrimSuffix(path, "/")
	return path == "/api/v1/inbox/stream" || strings.HasSuffix(path, "/api/v1/inbox/stream")
}
