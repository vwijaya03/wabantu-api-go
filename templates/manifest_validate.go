package templates

import (
	"encoding/json"
	"fmt"
)

// ManifestV1 is the minimal template manifest contract for Epic 0.
type ManifestV1 struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Meta          struct {
		Name string `json:"name"`
	} `json:"meta"`
	Tokens map[string]any `json:"tokens"`
}

// ValidateManifestJSON checks manifest v1 shape before persisting a version.
func ValidateManifestJSON(raw json.RawMessage) (ManifestV1, error) {
	var m ManifestV1
	if err := json.Unmarshal(raw, &m); err != nil {
		return ManifestV1{}, fmt.Errorf("manifest JSON tidak valid: %w", err)
	}
	if m.SchemaVersion != 1 {
		return ManifestV1{}, fmt.Errorf("schemaVersion harus 1")
	}
	switch m.Kind {
	case "chatbot", "storefront", "bundle":
	default:
		return ManifestV1{}, fmt.Errorf("kind tidak dikenal: %s", m.Kind)
	}
	if m.Meta.Name == "" {
		return ManifestV1{}, fmt.Errorf("meta.name wajib diisi")
	}
	if m.Tokens == nil {
		return ManifestV1{}, fmt.Errorf("tokens wajib diisi")
	}
	return m, nil
}
