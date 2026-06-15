package inventory

import (
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

func validateListMovementsParams(p *ListMovementsParams) error {
	if p == nil || strings.TrimSpace(p.CatalogItemID) == "" {
		return appErrs.BadRequest("pilih produk terlebih dahulu")
	}
	return nil
}

// buildMovementListWhere builds WHERE clause + args for movement list/count queries.
// Search by product name uses EXISTS so COUNT queries do not need a ci JOIN.
func buildMovementListWhere(p *ListMovementsParams) (where string, args []any, nextIdx int) {
	where = "WHERE 1=1"
	args = make([]any, 0, 4)
	idx := 1
	if strings.TrimSpace(p.CatalogItemID) == "" {
		return where, args, idx
	}
	where += fmt.Sprintf(" AND m.catalog_item_id = $%d", idx)
	args = append(args, p.CatalogItemID)
	idx++
	if strings.TrimSpace(p.WarehouseID) != "" {
		where += fmt.Sprintf(" AND m.warehouse_id = $%d", idx)
		args = append(args, p.WarehouseID)
		idx++
	}
	if strings.TrimSpace(p.Type) != "" {
		where += fmt.Sprintf(" AND m.movement_type = $%d", idx)
		args = append(args, strings.TrimSpace(p.Type))
		idx++
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		where += fmt.Sprintf(` AND (
			EXISTS (SELECT 1 FROM business_catalog_item ci WHERE ci.id = m.catalog_item_id AND ci.name ILIKE $%d) OR
			EXISTS (SELECT 1 FROM pur_bill b WHERE b.id = m.ref_id AND m.ref_type = 'bill' AND b.bill_no ILIKE $%d) OR
			EXISTS (SELECT 1 FROM inv_stock_transaction t WHERE t.id = m.ref_id AND t.doc_no ILIKE $%d) OR
			EXISTS (SELECT 1 FROM inv_sales_return r WHERE r.id = m.ref_id AND m.ref_type = 'sales_return' AND r.return_no ILIKE $%d)
		)`, idx, idx, idx, idx)
		args = append(args, "%"+q+"%")
		idx++
	}
	return where, args, idx
}
