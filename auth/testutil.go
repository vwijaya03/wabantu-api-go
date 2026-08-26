package auth

import (
	"context"

	"encore.app/wabantu/shared/redisurl"
)

// IssueTestAccessToken creates a Redis session and signed JWT for integration tests.
// Call from smoke tests instead of raw register/login HTTP handlers (Encore forbids calling raw APIs in-process).
func IssueTestAccessToken(ctx context.Context, data SessionData) (string, error) {
	sess, err := createSession(ctx, data)
	if err != nil {
		return "", err
	}
	token, _, err := signJWT(data.AccountID, sess.SessionID)
	return token, err
}

// RedisAvailable reports whether RedisURL is configured and reachable (for smoke test gating).
func RedisAvailable(ctx context.Context) bool {
	if secrets.RedisURL == "" {
		return false
	}
	client, err := redisurl.NewClient(secrets.RedisURL)
	if err != nil {
		return false
	}
	defer client.Close()
	return client.Ping(ctx).Err() == nil
}
