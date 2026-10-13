package adminauth

import (
	"sync"

	"golang.org/x/crypto/bcrypt"
)

var (
	dummyOnce sync.Once
	dummyHash []byte
)

// DummyHash is a real bcrypt hash at the given cost, so comparing a password for an
// unknown account costs the same as for an existing one (the previous dummy string was not
// a valid hash and returned immediately — a timing oracle for account enumeration).
func DummyHash(cost int) []byte {
	dummyOnce.Do(func() {
		h, err := bcrypt.GenerateFromPassword([]byte("wabantu-dummy-password-for-timing"), cost)
		if err == nil {
			dummyHash = h
		}
	})
	return dummyHash
}
