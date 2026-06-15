package inventory

import (
	"context"
	"database/sql"
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
	Execute      bool `json:"execute"` // false = preview only
	IssuesLimit  int  `json:"issuesLimit,omitempty"` // 0 = default; max 200 issues in response
}

const (
	backfillDefaultIssuesLimit = 50
	backfillMaxIssuesLimit     = 200
)

type BackfillOrderIssue struct {
	OrderID   string              `json:"orderId"`
	OrderRef  string              `json:"orderRef"`
	Status    string              `json:"status"`
	Message   string              `json:"message,omitempty"`
	Shortages []StockShortageLine `json:"shortages,omitempty"`
}

type BackfillSuggestedOpening struct {
	CatalogItemID string  `json:"catalogItemId"`
	ItemName      string  `json:"itemName"`
	WarehouseID   string  `json:"warehouseId"`
	WarehouseName string  `json:"warehouseName"`
	MinQty        float64 `json:"minQty"`
}

type BackfillOrdersResponse struct {
	Preview            bool                       `json:"preview"`
	PendingOrders      int                        `json:"pendingOrders"`      // committed orders lacking stock movements
	SufficientOrders   int                        `json:"sufficientOrders"`   // pending minus insufficient (preview)
	Processed          int                        `json:"processed"`          // successfully synced (execute)
	Failed             int                        `json:"failed"`             // failed to sync (execute)
	InsufficientCount  int                        `json:"insufficientCount"`  // orders blocked by stock shortage
	IssueCount         int                        `json:"issueCount"`         // total issue rows (may exceed len(Issues))
	IssuesTruncated    bool                       `json:"issuesTruncated"`    // true when IssueCount > len(Issues)
	FailureCount       int                        `json:"failureCount"`       // total failure messages (execute)
	FailuresTruncated  bool                       `json:"failuresTruncated"`  // true when failures list capped
	Insufficient       []string                   `json:"insufficient,omitempty"` // legacy; capped sample of order IDs
	Failures           []string                   `json:"failures,omitempty"`
	Issues             []BackfillOrderIssue       `json:"issues,omitempty"`
	SuggestedOpening   []BackfillSuggestedOpening `json:"suggestedOpening,omitempty"`
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
		WHERE o.deleted_at IS NULL
		  AND LOWER(TRIM(o.status)) IN (%s)
		ORDER BY o.created_at`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rawOrders, err := scanBackfillOrderRows(rows)
	if err != nil {
		return nil, err
	}

	batch, err := newBackfillBatch(ctx, conn, rawOrders)
	if err != nil {
		return nil, err
	}

	type pendingOrder struct {
		id, status string
		items      []OrderStockItem
	}
	var pending []pendingOrder
	for _, r := range rawOrders {
		if !batch.needsStockSync(r.id, r.status, r.items) {
			continue
		}
		pending = append(pending, pendingOrder{id: r.id, status: r.status, items: r.items})
	}

	resp := &BackfillOrdersResponse{
		Preview:      !p.Execute,
		PendingOrders: len(pending),
		Insufficient: []string{},
		Issues:       []BackfillOrderIssue{},
	}

	if !p.Execute {
		for _, po := range pending {
			shortages, serr := batch.analyzeShortages(ctx, conn, po.id, po.items)
			if serr != nil {
				return nil, serr
			}
			if len(shortages) == 0 {
				continue
			}
			resp.Insufficient = append(resp.Insufficient, po.id)
			resp.Issues = append(resp.Issues, buildBackfillIssue(po.id, po.status, shortages, ""))
		}
		resp.SuggestedOpening = aggregateSuggestedOpening(resp.Issues)
		finalizeBackfillResponse(resp, p.IssuesLimit)
		return resp, nil
	}

	for _, po := range pending {
		if err := SyncOrderStock(ctx, u.TenantSchema, po.id, po.status, po.items, u.AccountID); err != nil {
			resp.Failed++
			msg := err.Error()
			resp.Failures = append(resp.Failures, fmt.Sprintf("%s: %s", formatOrderRef(po.id), msg))
			shortages, _ := analyzeOrderStockShortageConn(ctx, conn, po.id, po.items)
			resp.Issues = append(resp.Issues, buildBackfillIssue(po.id, po.status, shortages, msg))
			if len(shortages) > 0 {
				resp.Insufficient = append(resp.Insufficient, po.id)
			}
			continue
		}
		resp.Processed++
	}
	if len(resp.Issues) > 0 {
		resp.SuggestedOpening = aggregateSuggestedOpening(resp.Issues)
	}
	finalizeBackfillResponse(resp, p.IssuesLimit)
	return resp, nil
}

type backfillOrderRow struct {
	id, status string
	items      []OrderStockItem
}

// scanBackfillOrderRows buffers committed orders before any follow-up queries on the
// same connection (pgx rejects queries while a Rows cursor is open).
func scanBackfillOrderRows(rows *sql.Rows) ([]backfillOrderRow, error) {
	defer rows.Close()
	out := make([]backfillOrderRow, 0)
	for rows.Next() {
		var id, status string
		var itemsRaw []byte
		if err := rows.Scan(&id, &status, &itemsRaw); err != nil {
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
			items = append(items, OrderStockItem{
				LineID: l.LineID, CatalogItemID: l.CatalogItemID,
				WarehouseID: l.WarehouseID, Qty: l.Qty,
			})
		}
		out = append(out, backfillOrderRow{id: id, status: status, items: items})
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return out, nil
}

func buildBackfillIssue(orderID, status string, shortages []StockShortageLine, message string) BackfillOrderIssue {
	issue := BackfillOrderIssue{
		OrderID:  orderID,
		OrderRef: formatOrderRef(orderID),
		Status:   status,
		Message:  message,
	}
	if len(shortages) > 0 {
		issue.Shortages = shortages
		if message == "" {
			issue.Message = shortageSummary(shortages)
		}
	}
	return issue
}

func shortageSummary(shortages []StockShortageLine) string {
	if len(shortages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(shortages))
	for _, s := range shortages {
		parts = append(parts, fmt.Sprintf("%s di %s: butuh %g, tersedia %g (kurang %g)",
			s.ItemName, s.WarehouseName, s.QtyRequired, s.QtyAvailable, s.QtyShort))
	}
	return strings.Join(parts, "; ")
}

// orderStockSyncDelta reports whether stock sync still needs to run (pure).
func orderStockSyncDelta(required map[reqKey]float64, netIssued map[reqKey]netEntry) bool {
	keys := map[reqKey]struct{}{}
	for k := range required {
		keys[k] = struct{}{}
	}
	for k := range netIssued {
		keys[k] = struct{}{}
	}
	for k := range keys {
		delta := round4(required[k] - netIssued[k].qty)
		if delta > epsilon || delta < -epsilon {
			return true
		}
	}
	return false
}

func orderNeedsStockSync(ctx context.Context, conn *sql.Conn, orderID, status string, items []OrderStockItem, defaultWarehouse string) (bool, error) {
	required := map[reqKey]float64{}
	if IsCommittedOrderStatus(status) {
		var err error
		required, err = resolveOrderRequirements(ctx, conn, items, defaultWarehouse)
		if err != nil {
			return false, err
		}
	}
	netIssued, err := orderNetIssued(ctx, conn, orderID)
	if err != nil {
		return false, err
	}
	return orderStockSyncDelta(required, netIssued), nil
}

func formatOrderRef(orderID string) string {
	id := strings.ReplaceAll(strings.TrimSpace(orderID), "-", "")
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return "WB-" + strings.ToUpper(id)
}

type openingKey struct{ item, warehouse string }

func aggregateSuggestedOpening(issues []BackfillOrderIssue) []BackfillSuggestedOpening {
	byKey := map[openingKey]BackfillSuggestedOpening{}
	for _, issue := range issues {
		for _, s := range issue.Shortages {
			if s.QtyShort <= epsilon {
				continue
			}
			k := openingKey{s.CatalogItemID, s.WarehouseID}
			cur, ok := byKey[k]
			if !ok {
				cur = BackfillSuggestedOpening{
					CatalogItemID: s.CatalogItemID,
					ItemName:      s.ItemName,
					WarehouseID:   s.WarehouseID,
					WarehouseName: s.WarehouseName,
					MinQty:        s.QtyShort,
				}
			} else {
				cur.MinQty = round4(cur.MinQty + s.QtyShort)
			}
			byKey[k] = cur
		}
	}
	out := make([]BackfillSuggestedOpening, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ItemName != out[j].ItemName {
			return out[i].ItemName < out[j].ItemName
		}
		return out[i].WarehouseName < out[j].WarehouseName
	})
	return out
}

func resolveIssuesLimit(n int) int {
	if n <= 0 {
		return backfillDefaultIssuesLimit
	}
	if n > backfillMaxIssuesLimit {
		return backfillMaxIssuesLimit
	}
	return n
}

const backfillMaxFailureMessages = 50

// finalizeBackfillResponse sets summary counts and caps large payloads for UI/API scale.
// suggestedOpening must already be computed from the full issue list.
func finalizeBackfillResponse(resp *BackfillOrdersResponse, issuesLimit int) {
	resp.InsufficientCount = len(resp.Insufficient)
	resp.IssueCount = len(resp.Issues)
	resp.SufficientOrders = resp.PendingOrders - resp.InsufficientCount
	if resp.SufficientOrders < 0 {
		resp.SufficientOrders = 0
	}

	limit := resolveIssuesLimit(issuesLimit)
	if resp.IssueCount > limit {
		resp.IssuesTruncated = true
		resp.Issues = resp.Issues[:limit]
	}
	if resp.InsufficientCount > limit {
		resp.Insufficient = resp.Insufficient[:limit]
	}

	resp.FailureCount = len(resp.Failures)
	if resp.FailureCount > backfillMaxFailureMessages {
		resp.FailuresTruncated = true
		resp.Failures = resp.Failures[:backfillMaxFailureMessages]
	}
}
