package adminauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
)

// MinBootstrapSecretLen is the minimum configured secret length; shorter (or empty)
// disables bootstrap entirely.
const MinBootstrapSecretLen = 32

var (
	ErrBootstrapDisabled   = errors.New("bootstrap disabled")            // 403: secret unset/empty/short
	ErrBootstrapSecret     = errors.New("invalid bootstrap secret")      // 403
	ErrAlreadyBootstrapped = errors.New("platform admin already exists") // 409
	ErrEmailTaken          = errors.New("email already registered")      // 409
	ErrBootstrapInput      = errors.New("email, name and password (min 12 chars) are required")
)

// CheckBootstrapSecret compares in constant time (over SHA-256 digests, so length is not
// leaked either). The configured secret must be ≥ 32 chars, else bootstrap is disabled.
func CheckBootstrapSecret(configured, provided string) error {
	configured = strings.TrimSpace(configured)
	if len(configured) < MinBootstrapSecretLen {
		return ErrBootstrapDisabled
	}
	a := sha256.Sum256([]byte(configured))
	b := sha256.Sum256([]byte(strings.TrimSpace(provided)))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 || strings.TrimSpace(provided) == "" {
		return ErrBootstrapSecret
	}
	return nil
}

// BootstrapTx is one DB transaction.
type BootstrapTx interface {
	// Lock serializes bootstraps (pg_advisory_xact_lock) so the existence check is atomic.
	Lock(ctx context.Context) error
	PlatformAdminExists(ctx context.Context) (bool, error)
	EmailTaken(ctx context.Context, email string) (bool, error)
	CreateAdmin(ctx context.Context, email, name, password string) (accountID string, err error)
	Commit() error
	Rollback() error
}

type BootstrapInput struct {
	Email, Name, Password string
}

func (in BootstrapInput) Valid() bool {
	return strings.Contains(in.Email, "@") && strings.TrimSpace(in.Name) != "" && len(in.Password) >= 12
}

// RunBootstrap creates the first platform admin exactly once: lock → exists? (409) →
// email taken? (409) → create → commit, all in one transaction.
func RunBootstrap(ctx context.Context, tx BootstrapTx, in BootstrapInput) (id string, err error) {
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if !in.Valid() {
		return "", ErrBootstrapInput
	}
	if err := tx.Lock(ctx); err != nil {
		return "", err
	}
	exists, err := tx.PlatformAdminExists(ctx)
	if err != nil {
		return "", err
	}
	if exists {
		return "", ErrAlreadyBootstrapped
	}
	taken, err := tx.EmailTaken(ctx, in.Email)
	if err != nil {
		return "", err
	}
	if taken {
		return "", ErrEmailTaken
	}
	id, err = tx.CreateAdmin(ctx, in.Email, in.Name, in.Password)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	committed = true
	return id, nil
}
