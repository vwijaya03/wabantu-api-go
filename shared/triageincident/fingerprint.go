package triageincident

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// FingerprintInput is the tenant-scoped root-cause key (never global).
type FingerprintInput struct {
	TenantID    string   `json:"tenantId"`
	Channel     string   `json:"channel"`
	Lane        string   `json:"lane"`
	FailureKind string   `json:"failureKind"`
	Path        string   `json:"path,omitempty"`
	CatalogIDs  []string `json:"catalogIds,omitempty"`
	FactKeys    []string `json:"factKeys,omitempty"`
}

// Compute is a tenant-scoped hash of the failure contract, not raw customer text.
func Compute(in FingerprintInput) string {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Channel = strings.TrimSpace(in.Channel)
	in.Lane = strings.TrimSpace(in.Lane)
	in.FailureKind = strings.TrimSpace(in.FailureKind)
	in.Path = strings.TrimSpace(in.Path)
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SameTenantRootCause is true when two sources share tenant+lane+failure (channel may differ).
func CrossChannelKey(in FingerprintInput) string {
	in.Channel = "*"
	return Compute(in)
}
