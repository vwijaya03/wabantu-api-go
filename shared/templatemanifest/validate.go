package templatemanifest

import (
	"encoding/json"
	"fmt"
	"strings"
)

var validLayouts = map[string]bool{
	LayoutBubbleBottomRight: true,
	LayoutBubbleBottomLeft:  true,
	LayoutFullPage:          true,
	LayoutStoreClassic:      true,
	LayoutStoreGrid:         true,
}

var validAnimations = map[string]bool{
	AnimationPlayful: true,
	AnimationSlide:   true,
	AnimationGlass:   true,
	AnimationBounce:  true,
	AnimationMinimal: true,
}

// Parse unmarshals and validates manifest JSON.
func Parse(raw json.RawMessage) (Manifest, error) {
	if len(raw) == 0 {
		return Manifest{}, fmt.Errorf("manifest kosong")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest tidak valid: %w", err)
	}
	if err := Validate(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate checks a decoded manifest.
func Validate(m Manifest) error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion harus %d", SchemaVersion)
	}
	if !ValidKinds[m.Kind] {
		return fmt.Errorf("kind tidak dikenal: %q", m.Kind)
	}
	if strings.TrimSpace(m.Tokens.Primary) == "" {
		return fmt.Errorf("tokens.primary wajib diisi")
	}
	if !validLayouts[m.Layout] {
		return fmt.Errorf("layout tidak dikenal: %q", m.Layout)
	}
	if m.AnimationPreset != "" && !validAnimations[m.AnimationPreset] {
		return fmt.Errorf("animationPreset tidak dikenal: %q", m.AnimationPreset)
	}
	switch m.Kind {
	case KindChatbot:
		if !strings.HasPrefix(m.Layout, "bubble-") && m.Layout != LayoutFullPage {
			return fmt.Errorf("layout chatbot harus bubble-* atau full-page")
		}
	case KindStorefront:
		if m.Layout != LayoutStoreClassic && m.Layout != LayoutStoreGrid {
			return fmt.Errorf("layout storefront harus store-classic atau store-grid")
		}
	}
	if len(m.Assets.CustomCSS) > 32_000 {
		return fmt.Errorf("customCss terlalu panjang")
	}
	return nil
}
