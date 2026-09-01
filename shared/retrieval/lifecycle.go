package retrieval

import (
	"context"
	"fmt"
	"strings"
)

// MinIndexedPercentVector is the minimum KB+catalog indexed % before vector mode is allowed.
const MinIndexedPercentVector = 90

// EmbeddingModelSlug returns a short stable slug for vector IDs and metadata.
func EmbeddingModelSlug(model string) string {
	m := strings.TrimSpace(model)
	switch m {
	case "", EmbeddingModel:
		return "te3s"
	default:
		s := strings.NewReplacer("-", "", ".", "", "_", "").Replace(m)
		if len(s) > 12 {
			s = s[:12]
		}
		return s
	}
}

// KBContentHash hashes KB text including the active embedding model.
func KBContentHash(question, answer string) string {
	return ContentHash(EmbeddingModel, question, answer)
}

// CatalogContentHash hashes catalog text including the active embedding model.
func CatalogContentHash(name, description, externalCode string) string {
	return ContentHash(EmbeddingModel, name, description, externalCode)
}

// KBVectorIDForModel builds a model-scoped vector id (current format).
func KBVectorIDForModel(entryID, model string, version int64, chunkIndex int) string {
	return fmt.Sprintf("kb:%s:m%s:v%d:c%d", entryID, EmbeddingModelSlug(model), version, chunkIndex)
}

// LegacyKBVectorID is the pre-PR-9 id format (no model segment).
func LegacyKBVectorID(entryID string, version int64, chunkIndex int) string {
	return fmt.Sprintf("kb:%s:v%d:c%d", entryID, version, chunkIndex)
}

// CatalogVectorIDForModel builds a model-scoped catalog vector id.
func CatalogVectorIDForModel(itemID, model string, version int64, chunkIndex int) string {
	return fmt.Sprintf("catalog:%s:m%s:v%d:c%d", itemID, EmbeddingModelSlug(model), version, chunkIndex)
}

// LegacyCatalogVectorID is the pre-PR-9 catalog id format.
func LegacyCatalogVectorID(itemID string, version int64, chunkIndex int) string {
	return fmt.Sprintf("catalog:%s:v%d:c%d", itemID, version, chunkIndex)
}

// KBVectorIDsForVersions returns current + legacy ids for versions [from..to].
func KBVectorIDsForVersions(entryID, model string, fromVer, toVer int64) []string {
	if toVer < fromVer {
		return nil
	}
	ids := make([]string, 0, int(toVer-fromVer+1)*2)
	for v := fromVer; v <= toVer; v++ {
		ids = append(ids, LegacyKBVectorID(entryID, v, 0))
		ids = append(ids, KBVectorIDForModel(entryID, model, v, 0))
	}
	return ids
}

// CatalogVectorIDsForVersions returns current + legacy ids for catalog versions [from..to].
func CatalogVectorIDsForVersions(itemID, model string, fromVer, toVer int64) []string {
	if toVer < fromVer {
		return nil
	}
	ids := make([]string, 0, int(toVer-fromVer+1)*2)
	for v := fromVer; v <= toVer; v++ {
		ids = append(ids, LegacyCatalogVectorID(itemID, v, 0))
		ids = append(ids, CatalogVectorIDForModel(itemID, model, v, 0))
	}
	return ids
}

// StoredEmbeddingModelOK returns false when PG row was indexed with a different model.
func StoredEmbeddingModelOK(stored string) bool {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return true // legacy rows before embedding_model column
	}
	return stored == EmbeddingModel
}

// PurgeStaleKBVersions deletes superseded KB vectors (versions < keepVersion).
func PurgeStaleKBVersions(ctx context.Context, svc *Service, tenant TenantIdentity, entryID string, keepVersion int64) error {
	if svc == nil || svc.Store == nil || keepVersion <= 1 {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	ids := KBVectorIDsForVersions(entryID, EmbeddingModel, 1, keepVersion-1)
	return svc.Store.DeleteIDs(ctx, ns, ids)
}

// DeleteAllKBEntryVectors removes every KB vector for an entry (both id formats).
func DeleteAllKBEntryVectors(ctx context.Context, svc *Service, tenant TenantIdentity, entryID string, maxVersion int64) error {
	if svc == nil || svc.Store == nil {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	if maxVersion < 1 {
		maxVersion = 1
	}
	ids := KBVectorIDsForVersions(entryID, EmbeddingModel, 1, maxVersion)
	if err := svc.Store.DeleteIDs(ctx, ns, ids); err != nil {
		return err
	}
	// Filter delete catches any orphan metadata layout.
	return svc.Store.DeleteByFilter(ctx, ns, map[string]any{
		"source":   map[string]any{"$eq": string(SourceKB)},
		"entry_id": map[string]any{"$eq": entryID},
	})
}

// PurgeStaleCatalogVersions deletes superseded catalog vectors.
func PurgeStaleCatalogVersions(ctx context.Context, svc *Service, tenant TenantIdentity, itemID string, keepVersion int64) error {
	if svc == nil || svc.Store == nil || keepVersion <= 1 {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	ids := CatalogVectorIDsForVersions(itemID, EmbeddingModel, 1, keepVersion-1)
	return svc.Store.DeleteIDs(ctx, ns, ids)
}

// DeleteAllCatalogItemVectors removes every catalog vector for an item.
func DeleteAllCatalogItemVectors(ctx context.Context, svc *Service, tenant TenantIdentity, itemID string, maxVersion int64) error {
	if svc == nil || svc.Store == nil {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	if maxVersion < 1 {
		maxVersion = 1
	}
	ids := CatalogVectorIDsForVersions(itemID, EmbeddingModel, 1, maxVersion)
	if err := svc.Store.DeleteIDs(ctx, ns, ids); err != nil {
		return err
	}
	return svc.Store.DeleteByFilter(ctx, ns, map[string]any{
		"source":   map[string]any{"$eq": string(SourceCatalog)},
		"entry_id": map[string]any{"$eq": itemID},
	})
}

// DeleteTenantNamespace removes all vectors for a tenant schema (offboarding).
func DeleteTenantNamespace(ctx context.Context, svc *Service, tenant TenantIdentity) error {
	if svc == nil || svc.Store == nil {
		return nil
	}
	ns, err := Namespace(tenant)
	if err != nil {
		return err
	}
	return svc.Store.DeleteNamespace(ctx, ns)
}
