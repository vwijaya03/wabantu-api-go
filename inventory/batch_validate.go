package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"database/sql"
	"errors"
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

func validateCatalogItemsBatch(ctx context.Context, sch appdb.SchemaSQL, q querier, ids []string) error {
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return appErrs.BadRequest("item katalog wajib dipilih")
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := qquery(ctx, sch, q, fmt.Sprintf(`
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

func validateWarehousesBatch(ctx context.Context, sch appdb.SchemaSQL, q querier, ids []string) error {
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return appErrs.BadRequest("gudang wajib dipilih")
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := qquery(ctx, sch, q, fmt.Sprintf(`
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

type openingBalanceKey struct {
	catalogItemID string
	warehouseID   string
}

// validateOpeningBalanceEntryPairs rejects duplicate SKU+gudang within one submit.
func validateOpeningBalanceEntryPairs(entries []OpeningEntry) error {
	seen := map[openingBalanceKey]int{}
	for i, e := range entries {
		itemID := strings.TrimSpace(e.CatalogItemID)
		whID := strings.TrimSpace(e.WarehouseID)
		if itemID == "" || whID == "" {
			continue
		}
		k := openingBalanceKey{itemID, whID}
		if prev, ok := seen[k]; ok {
			return appErrs.BadRequest(fmt.Sprintf(
				"baris %d dan %d: kombinasi produk+gudang duplikat dalam form",
				prev+1, i+1,
			))
		}
		seen[k] = i
	}
	return nil
}

func uniqueOpeningBalanceKeys(entries []OpeningEntry) []openingBalanceKey {
	seen := map[openingBalanceKey]struct{}{}
	out := make([]openingBalanceKey, 0, len(entries))
	for _, e := range entries {
		itemID := strings.TrimSpace(e.CatalogItemID)
		whID := strings.TrimSpace(e.WarehouseID)
		if itemID == "" || whID == "" {
			continue
		}
		k := openingBalanceKey{itemID, whID}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// validateOpeningBalanceNotRegistered blocks saldo awal when SKU+gudang already has opening txn.
func validateOpeningBalanceNotRegistered(ctx context.Context, sch appdb.SchemaSQL, q querier, entries []OpeningEntry) error {
	keys := uniqueOpeningBalanceKeys(entries)
	if len(keys) == 0 {
		return nil
	}
	tupleParts := make([]string, len(keys))
	args := make([]any, 0, 1+len(keys)*2)
	args = append(args, TxnKindOpeningBalance)
	idx := 2
	for i, k := range keys {
		tupleParts[i] = fmt.Sprintf("($%d::uuid,$%d::uuid)", idx, idx+1)
		args = append(args, k.catalogItemID, k.warehouseID)
		idx += 2
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(ci.name, ''), COALESCE(w.name, '')
		FROM inv_stock_transaction_line l
		JOIN inv_stock_transaction t ON t.id = l.transaction_id
		LEFT JOIN business_catalog_item ci ON ci.id = l.catalog_item_id
		LEFT JOIN inv_warehouse w ON w.id = l.warehouse_id
		WHERE t.kind = $1
		  AND (l.catalog_item_id, l.warehouse_id) IN (%s)
		LIMIT 1`, strings.Join(tupleParts, ","))
	row := qrow(ctx, sch, q, query, args...)
	var itemName, whName string
	if err := row.Scan(&itemName, &whName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return appErrs.Internal(err.Error())
	}
	if strings.TrimSpace(itemName) == "" {
		itemName = "produk"
	}
	if strings.TrimSpace(whName) == "" {
		whName = "gudang"
	}
	return appErrs.BadRequest(fmt.Sprintf(
		"%s di %s sudah punya saldo awal — gunakan Penyesuaian Stok untuk menambah atau mengurangi stok",
		itemName, whName,
	))
}
