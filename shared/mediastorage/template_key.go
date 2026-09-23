package mediastorage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// BuildTemplateAssetKey stores developer assets content-addressed under templates/.
// Format: templates/{developerID}/{sha256_prefix}.{ext}
func BuildTemplateAssetKey(developerID string, data []byte, ext string) string {
	sum := sha256.Sum256(data)
	prefix := hex.EncodeToString(sum[:])
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if ext == "" {
		ext = "bin"
	}
	return fmt.Sprintf("templates/%s/%s.%s",
		strings.TrimSpace(developerID),
		prefix,
		ext,
	)
}
