package auth

import "context"

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
