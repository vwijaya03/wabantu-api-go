package apitest

import (
	"context"
	"testing"

	"encore.app/wabantu/auth"
)

func redisAvailable() bool {
	return auth.RedisAvailable(context.Background())
}

// RequireRedis skips the test when Redis is not reachable (Encore Cloud build has no local Redis).
func RequireRedis(t *testing.T) {
	t.Helper()
	RequireEncoreInfra(t)
	if !redisAvailable() {
		t.Skip("Redis not available — skip JWT/session smoke (run locally with ./scripts/run-api-smoke-tests.sh)")
	}
}
