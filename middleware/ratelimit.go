// Package middleware provides global Encore HTTP middleware.
package middleware

import (
	"time"

	"encore.dev/beta/errs"
	encoremw "encore.dev/middleware"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/shared/ratelimit"
)

// RateLimit applies a Redis sliding-window limit to all HTTP APIs (default 400 req/min per client IP).
//
//encore:middleware global target=all
func RateLimit(req encoremw.Request, next encoremw.Next) encoremw.Response {
	ip := ""
	if data := req.Data(); data != nil {
		ip = ratelimit.ClientIPFromHeaders(data.Headers)
	}
	if ip == "" {
		return next(req)
	}
	key := ratelimit.Key("api", ip)
	if !ratelimit.Allow(req.Context(), appauth.RedisClient(), key, ratelimit.DefaultPublicRPM, time.Minute) {
		return encoremw.Response{
			Err: &errs.Error{
				Code:    errs.ResourceExhausted,
				Message: "too many requests — coba lagi dalam satu menit",
			},
		}
	}
	return next(req)
}
