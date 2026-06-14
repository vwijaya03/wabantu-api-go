package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// committedStatusesForSQL returns the committed order statuses (sorted, for stable SQL).
func committedStatusesForSQL() []string {
	out := make([]string, 0, len(committedOrderStatuses))
	for s := range committedOrderStatuses {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

type BackfillOrdersParams struct {
	Execute bool `json:"execute"` // false = preview only
}

type BackfillOrdersResponse struct {
	Preview        bool     `json:"preview"`
	PendingOrders  int      `json:"pendingOrders"`  // committed orders lacking stock movements
	Processed      int      `json:"processed"`      // successfully synced (execute)
	Failed         int      `json:"failed"`         // failed to sync (execute)
	Insufficient   []string `json:"insufficient"`   // order numbers blocked by stock (preview/execute)
	Failures       []string `json:"failures,omitempty"`
}

// BackfillOrders retroactively issues stock for committed orders created before the
// inventory module was active. Preview (Execute=false) reports what would happen;
// Execute=true runs SyncOrderStock per order. Owner-only.
//
//encore:api auth method=POST path=/api/v1/inventory/backfill/orders
func BackfillOrders(ctx context.Context, p *BackfillOrdersParams) (*BackfillOrdersResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	setup, _, _, err := loadSyncSetting(ctx, conn)
	if err != nil {
		return nil, err
	}
	if !setup {
		return nil, appErrs.BadRequest("selesaikan setup persediaan dulu sebelum backfill")
	}

	statuses := committedStatusesForSQL()
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, s := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT o.id::text, o.status, COALESCE(o.items, '[]')
		FROM "order" o
		WHERE o.deleted_at IS NULL AND o.status IN (%s)
		  AND NOT EXISTS (
		    SELECT 1 FROM inv_stock_movement m
		    WHERE m.ref_type='order' AND m.ref_id = o.id AND m.movement_type='sale_issue'
		  )
		ORDER BY o.created_at`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	type pendingOrder struct {
		id, status string
		items      []OrderStockItem
	}
	var pending []pendingOrder
	for rows.Next() {
		var id, status string
		var itemsRaw []byte
		if err := rows.Scan(&id, &status, &itemsRaw); err != nil {
			rows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		var lines []orderLineView
		if len(itemsRaw) > 0 {
			_ = json.Unmarshal(itemsRaw, &lines)
		}
		items := make([]OrderStockItem, 0, len(lines))
		for _, l := range lines {
			if strings.TrimSpace(l.CatalogItemID) == "" {
				continue
			}
			items = append(items, OrderStockItem{LineID: l.LineID, CatalogItemID: l.CatalogItemID, WarehouseID: l.WarehouseID, Qty: l.Qty})
		}
		pending = append(pending, pendingOrder{id: id, status: status, items: items})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	rows.Close()

	resp := &BackfillOrdersResponse{Preview: !p.Execute, PendingOrders: len(pending), Insufficient: []string{}}

	if !p.Execute {
		// Preview: flag orders that would be blocked by insufficient stock.
		for _, po := range pending {
			if err := PrecheckOrderStock(ctx, u.TenantSchema, po.id, po.items); err != nil {
				resp.Insufficient = append(resp.Insufficient, po.id)
			}
		}
		return resp, nil
	}

	for _, po := range pending {
		if err := SyncOrderStock(ctx, u.TenantSchema, po.id, po.status, po.items, u.AccountID); err != nil {
			resp.Failed++
			resp.Failures = append(resp.Failures, fmt.Sprintf("%s: %v", po.id, err))
			continue
		}
		resp.Processed++
	}
	return resp, nil
}
