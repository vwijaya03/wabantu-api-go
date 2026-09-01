package ai

import (
	"context"
	"fmt"

	"encore.app/wabantu/shared/retrieval"
)

const maxKBEntriesByIDsFetch = 50

func loadKBEntriesByIDs(ctx context.Context, ts tenantScopedQuerier, ids []string) ([]dbKBEntry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxKBEntriesByIDsFetch {
		ids = ids[:maxKBEntriesByIDsFetch]
	}
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, question, answer, category, is_active
		FROM %s
		WHERE is_active = true AND id = ANY($1::uuid[])`, ts.T("knowledge_base_entry")), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []dbKBEntry
	for rows.Next() {
		var e dbKBEntry
		if err := rows.Scan(&e.ID, &e.Question, &e.Answer, &e.Category, &e.IsActive); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func loadCatalogItemsByIDs(ctx context.Context, ts tenantScopedQuerier, ids []string) ([]dbCatalogItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxKBEntriesByIDsFetch {
		ids = ids[:maxKBEntriesByIDsFetch]
	}
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, external_code, name,
		       COALESCE(sell_price, 0), COALESCE(sell_unit, 'pcs')
		FROM %s
		WHERE deleted_at IS NULL AND is_active = true AND id = ANY($1::uuid[])`,
		ts.T("business_catalog_item")), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbCatalogItem
	for rows.Next() {
		var it dbCatalogItem
		if err := rows.Scan(&it.ID, &it.ExternalCode, &it.Name, &it.SellPrice, &it.SellUnit); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func mergeKBEntries(base, extra []dbKBEntry) []dbKBEntry {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]dbKBEntry, 0, len(base)+len(extra))
	for _, e := range base {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		out = append(out, e)
	}
	for _, e := range extra {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		out = append(out, e)
	}
	return out
}

func mergeCatalogItems(base, extra []dbCatalogItem) []dbCatalogItem {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]dbCatalogItem, 0, len(base)+len(extra))
	for _, it := range base {
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	for _, it := range extra {
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, it)
	}
	return out
}

// collectMissingKBEntryIDs returns entry IDs from fused/vector results not present in preloaded KB.
func collectMissingKBEntryIDs(preloaded []dbKBEntry, scores []retrieval.ScoredEntry, vectorHits []retrieval.Hit) []string {
	if len(scores) == 0 && len(vectorHits) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(preloaded))
	for _, e := range preloaded {
		seen[e.ID] = struct{}{}
	}
	var missing []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		missing = append(missing, id)
	}
	for _, s := range scores {
		add(s.EntryID)
	}
	for _, h := range vectorHits {
		add(retrieval.EntryIDFromHit(h))
	}
	if len(missing) > maxKBEntriesByIDsFetch {
		missing = missing[:maxKBEntriesByIDsFetch]
	}
	return missing
}

func collectMissingCatalogIDs(preloaded []dbCatalogItem, hits []retrieval.Hit) []string {
	if len(hits) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(preloaded))
	for _, it := range preloaded {
		seen[it.ID] = struct{}{}
	}
	var missing []string
	for _, h := range hits {
		id := retrieval.EntryIDFromHit(h)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		missing = append(missing, id)
	}
	if len(missing) > maxKBEntriesByIDsFetch {
		missing = missing[:maxKBEntriesByIDsFetch]
	}
	return missing
}
