package inventory

import (
	"context"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func inClause(startIdx int, n int) (string, []any) {
	if n == 0 {
		return "", nil
	}
	parts := make([]string, n)
	args := make([]any, n)
	for i := 0; i < n; i++ {
		parts[i] = fmt.Sprintf("$%d", startIdx+i)
		args[i] = nil // filled by caller
	}
	return strings.Join(parts, ","), args
}

func validateCatalogItemsBatch(ctx context.Context, q querier, ids []string) error {
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return appErrs.BadRequest("item katalog wajib dipilih")
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text FROM business_catalog_item
		WHERE deleted_at IS NULL AND id IN (%s)`, clause), args...)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()
	found := make(map[string]struct{}, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return appErrs.Internal(err.Error())
		}
		found[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return appErrs.BadRequest("item katalog tidak ditemukan")
		}
	}
	return nil
}

func validateWarehousesBatch(ctx context.Context, q querier, ids []string) error {
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return appErrs.BadRequest("gudang wajib dipilih")
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text FROM inv_warehouse
		WHERE deleted_at IS NULL AND id IN (%s)`, clause), args...)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()
	found := make(map[string]struct{}, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return appErrs.Internal(err.Error())
		}
		found[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return appErrs.BadRequest("gudang tidak ditemukan")
		}
	}
	return nil
}
