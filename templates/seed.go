package templates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"encore.dev/rlog"
)

type seedTemplate struct {
	Kind    string
	Slug    string
	Title   string
	Version string
	Manifest map[string]any
}

var platformSeeds = []seedTemplate{
	{
		Kind: "chatbot", Slug: "platform-chat-minimal", Title: "Chat Minimal",
		Version: "1.0.0",
		Manifest: map[string]any{
			"schemaVersion": 1,
			"kind":          "chatbot",
			"meta":          map[string]any{"name": "Chat Minimal", "author": "WABantu"},
			"tokens": map[string]any{
				"color": map[string]any{
					"primary": "#10b981", "primaryForeground": "#ffffff",
					"background": "#ffffff", "foreground": "#0f172a",
				},
			},
		},
	},
	{
		Kind: "chatbot", Slug: "platform-chat-playful", Title: "Chat Playful",
		Version: "1.0.0",
		Manifest: map[string]any{
			"schemaVersion": 1,
			"kind":          "chatbot",
			"meta":          map[string]any{"name": "Chat Playful", "author": "WABantu"},
			"tokens": map[string]any{
				"color": map[string]any{
					"primary": "#6366f1", "primaryForeground": "#ffffff",
					"background": "#f8fafc", "foreground": "#0f172a", "accent": "#f59e0b",
				},
				"motion": map[string]any{"launcherBounce": true},
			},
		},
	},
}

func manifestSHA(m map[string]any) string {
	b, _ := json.Marshal(m)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ensurePlatformTemplates seeds built-in listings once (idempotent).
func ensurePlatformTemplates(ctx context.Context) error {
	for _, s := range platformSeeds {
		raw, err := json.Marshal(s.Manifest)
		if err != nil {
			return err
		}
		if _, err := ValidateManifestJSON(raw); err != nil {
			return err
		}
		sha := manifestSHA(s.Manifest)
		var listingID string
		err = sysDB.QueryRow(ctx, `
			INSERT INTO template_listing (kind, slug, title, status, price_idr)
			VALUES ($1, $2, $3, 'published', 0)
			ON CONFLICT (kind, slug) DO UPDATE SET title = EXCLUDED.title, status = 'published', updated_at = now()
			RETURNING id::text`, s.Kind, s.Slug, s.Title).Scan(&listingID)
		if err != nil {
			return err
		}
		_, err = sysDB.Exec(ctx, `
			INSERT INTO template_version (listing_id, version, manifest_json, manifest_sha256, status)
			VALUES ($1::uuid, $2, $3::jsonb, $4, 'published')
			ON CONFLICT (listing_id, version) DO UPDATE
			SET manifest_json = EXCLUDED.manifest_json,
			    manifest_sha256 = EXCLUDED.manifest_sha256,
			    status = 'published'`,
			listingID, s.Version, raw, sha)
		if err != nil {
			return err
		}
	}
	rlog.Info("platform templates seeded")
	return nil
}
