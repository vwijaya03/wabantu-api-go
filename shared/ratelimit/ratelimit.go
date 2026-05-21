// Package ratelimit provides Redis-backed HTTP rate limiting (sliding window counter).
package ratelimit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultPublicRPM is the default limit for authenticated/public API traffic per client key.
	// Dashboard SPA can burst on navigation (React Query + layout); 120/min was too low in practice.
	DefaultPublicRPM = 400
	// AuthRPM limits login/register attempts per IP.
	AuthRPM = 20
)

// Allow reports whether the client may proceed (true) or is over limit (false).
func Allow(ctx context.Context, rdb *redis.Client, key string, max int, window time.Duration) bool {
	if rdb == nil || key == "" || max <= 0 {
		return true
	}
	redisKey := fmt.Sprintf("rl:%s", key)
	pipe := rdb.Pipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return true
	}
	n, err := incr.Result()
	if err != nil {
		return true
	}
	return int(n) <= max
}

// ClientIPFromHeaders returns the best-effort client IP from request headers.
func ClientIPFromHeaders(h http.Header) string {
	if h == nil {
		return ""
	}
	if xff := h.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := h.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return ""
}

// ClientIP returns the best-effort client IP from an HTTP request.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip := ClientIPFromHeaders(r.Header); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Key builds a rate-limit bucket name.
func Key(scope, id string) string {
	return scope + ":" + id
}
