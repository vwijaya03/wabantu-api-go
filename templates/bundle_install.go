package templates

import (
	"context"
)

// installTemplateSurfaces atomically pins a template to one or more surfaces.
// Bundle kind expands to chatbot + storefront with shared manifest version.
func installTemplateSurfaces(ctx context.Context, tenantID, listingID, versionID, kind, accountID string) error {
	surfaces := surfacesForKind(kind)
	for _, surface := range surfaces {
		_, err := sysDB.Exec(ctx, `
			INSERT INTO tenant_template_install (tenant_id, listing_id, version_id, kind, surface, installed_by)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid)
			ON CONFLICT (tenant_id, surface) DO UPDATE
			SET listing_id = EXCLUDED.listing_id, version_id = EXCLUDED.version_id,
			    kind = EXCLUDED.kind, is_active = true, installed_at = now()`,
			tenantID, listingID, versionID, kind, surface, accountID)
		if err != nil {
			return err
		}
	}
	return nil
}

func surfacesForKind(kind string) []string {
	switch kind {
	case "bundle":
		return []string{"chatbot", "storefront"}
	case "chatbot", "storefront":
		return []string{kind}
	default:
		return []string{kind}
	}
}
