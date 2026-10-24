package importcsv

import (
	"crypto/rand"
	"encoding/hex"
)

// importJobIDPrefix + 32 lowercase hex chars (128 bits from crypto/rand).
// The FE puts the jobId straight into /import/status/${jobId}, so it must be
// URL-safe; hex needs no escaping.
const importJobIDPrefix = "imp_"

// newImportJobID returns an unguessable, URL-safe import job id such as
// "imp_3f9c0a…" (36 chars). It replaces the old time.UnixNano-based id,
// which was predictable.
func newImportJobID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return importJobIDPrefix + hex.EncodeToString(b[:]), nil
}
