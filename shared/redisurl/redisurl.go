// Package redisurl parses RedisURL secrets for github.com/redis/go-redis.
//
// Upstash provides two connection types:
//   - TCP Redis URL (rediss://default:token@host:6379) — required by api-go
//   - REST (UPSTASH_REDIS_REST_URL + TOKEN) — NOT supported; HTTP SDK only
package redisurl

import (
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// ParseClientOptions returns go-redis options from a Redis TCP URL.
func ParseClientOptions(raw string) (*redis.Options, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("redis URL is empty")
	}
	if err := rejectRESTCredentials(raw); err != nil {
		return nil, err
	}
	opt, err := redis.ParseURL(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL: %w", err)
	}
	return opt, nil
}

// NewClient creates a go-redis client from a Redis TCP URL.
func NewClient(raw string) (*redis.Client, error) {
	opt, err := ParseClientOptions(raw)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opt), nil
}

func rejectRESTCredentials(raw string) error {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return fmt.Errorf(
			"RedisURL is an HTTP REST endpoint (Upstash REST API). " +
				"api-go needs the Redis TCP URL from Upstash Console, e.g. " +
				"rediss://default:TOKEN@xxxx.upstash.io:6379 (not UPSTASH_REDIS_REST_URL)",
		)
	}
	// Common mistake: pasting only the hostname without scheme/password.
	if strings.Contains(lower, ".upstash.io") && !strings.HasPrefix(lower, "redis://") && !strings.HasPrefix(lower, "rediss://") {
		return fmt.Errorf(
			"RedisURL looks incomplete for Upstash; use full TCP URL rediss://default:TOKEN@host.upstash.io:6379",
		)
	}
	return nil
}
