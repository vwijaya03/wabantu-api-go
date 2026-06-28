package order

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/finance"
	"encore.app/wabantu/inventory"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pricing"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
)

var db = sqldb.Named("tenant")

// ---------- status ----------

var validOrderStatuses = map[string]bool{
	"draft":      true,
	"processing": true,
	"shipped":    true,
	"completed":  true,
	"cancelled":  true,
	// Legacy statuses kept readable for orders created before the current ops flow.
	"confirmed": true,
	"paid":      true,
}

// ---------- types ----------

type OrderItem struct {
	LineID        string  `json:"lineId,omitempty"`
	CatalogItemID string  `json:"catalogItemId,omitempty"`
	ExternalCode  string  `json:"externalCode,omitempty"`
	Name          string  `json:"name"`
	Variant       string  `json:"variant,omitempty"`
	Size          string  `json:"size,omitempty"`
	Color         string  `json:"color,omitempty"`
	Qty           float64 `json:"qty"`
	UnitPrice     float64 `json:"unitPrice"`
	SellUnit      string  `json:"sellUnit,omitempty"`
	WarehouseID   string  `json:"warehouseId,omitempty"`
}

type ShippingAddress struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Street     string `json:"street"`
	RT         string `json:"rt,omitempty"`
	RW         string `json:"rw,omitempty"`
	Kelurahan  string `json:"kelurahan,omitempty"`
	Kecamatan  string `json:"kecamatan,omitempty"`
	City       string `json:"city"`
	CityID     string `json:"cityId,omitempty"`
	Province   string `json:"province"`
	ProvinceID string `json:"provinceId,omitempty"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country,omitempty"`
}

type PaymentProofMeta struct {
	Amount        *float64 `json:"amount,omitempty"`
	Bank          string   `json:"bank,omitempty"`
	AccountNumber string   `json:"accountNumber,omitempty"`
	AccountName   string   `json:"accountName,omitempty"`
	Date          string   `json:"date,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	Flags         []string `json:"flags,omitempty"`
	RejectReason  string   `json:"rejectReason,omitempty"`
	FileHash      string   `json:"fileHash,omitempty"`
}

type Order struct {
	ID                   string           `json:"id"`
	OrderNumber          string           `json:"orderNumber"`
	ConversationID       string           `json:"conversationId"`
	ContactID            string           `json:"contactId"`
	ContactDisplayName   string           `json:"contactDisplayName,omitempty"`
	ContactPhone         string           `json:"contactPhone,omitempty"`
	Items                []OrderItem      `json:"items"`
	ShippingAddress      *ShippingAddress `json:"shippingAddress,omitempty"`
	Notes                string           `json:"notes"`
	Status               string           `json:"status"`
	TrackingNumber       string           `json:"trackingNumber"`
	Courier              string           `json:"courier"`
	PaymentTransactionID       string            `json:"paymentTransactionId"`
	PaymentStatus              string            `json:"paymentStatus"`
	PaymentProofMessageID      string            `json:"paymentProofMessageId,omitempty"`
	PaymentProofSubmittedAt    *time.Time        `json:"paymentProofSubmittedAt,omitempty"`
	PaymentProofVerifiedAt     *time.Time        `json:"paymentProofVerifiedAt,omitempty"`
	PaymentProofVerifiedBy     string            `json:"paymentProofVerifiedBy,omitempty"`
	PaymentProofMeta           *PaymentProofMeta `json:"paymentProofMeta,omitempty"`
	Subtotal                   float64           `json:"subtotal"`
	ShippingCost         float64          `json:"shippingCost"`
	Total                float64          `json:"total"`
	IncomeWalletID       string           `json:"incomeWalletId,omitempty"`
	CreatedBy            string           `json:"createdBy"`
	CreatedAt            time.Time        `json:"createdAt"`
	UpdatedAt            time.Time        `json:"updatedAt"`
	DeletedAt            *time.Time       `json:"deletedAt,omitempty"`
}

type CreateOrderParams struct {
	ConversationID  string           `json:"conversationId"`
	ContactID       string           `json:"contactId"`
	Items           []OrderItem      `json:"items"`
	ShippingAddress *ShippingAddress `json:"shippingAddress,omitempty"`
	Notes           string           `json:"notes,omitempty"`
	Status          string           `json:"status,omitempty"`
	TrackingNumber  string           `json:"trackingNumber,omitempty"`
	Courier         string           `json:"courier,omitempty"`
	ShippingCost    float64          `json:"shippingCost,omitempty"`
	IncomeWalletID  string           `json:"incomeWalletId,omitempty"`
}

type UpdateOrderParams struct {
	ContactID            *string     `json:"contactId,omitempty"`
	Items                []OrderItem `json:"items,omitempty"`
	Notes                *string     `json:"notes,omitempty"`
	Status               *string     `json:"status,omitempty"`
	TrackingNumber       *string     `json:"trackingNumber,omitempty"`
	Courier              *string     `json:"courier,omitempty"`
	PaymentTransactionID *string     `json:"paymentTransactionId,omitempty"`
	ShippingCost         *float64    `json:"shippingCost,omitempty"`
	IncomeWalletID       *string     `json:"incomeWalletId,omitempty"`
}

type ListOrdersParams struct {
	Status   string `query:"status"`
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListOrdersResponse struct {
	Orders   []Order `json:"orders"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
}

type BatchUpdateStatusParams struct {
	IDs    []string `json:"ids"`
	Status string   `json:"status"`
}

type BatchUpdateStatusResponse struct {
	Updated int `json:"updated"`
}

type BatchDeleteParams struct {
	IDs []string `json:"ids"`
}

type BatchDeleteResponse struct {
	Deleted int `json:"deleted"`
}

var validPaymentStatuses = map[string]bool{
	"unpaid":          true,
	"proof_submitted": true,
	"verified":        true,
	"rejected":        true,
}

func orderSelectCols(prefix string) string {
	col := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	// UUID columns are cast to text; COALESCE(uuid,'') fails with SQLSTATE 22P02.
	return fmt.Sprintf(`%s,
		COALESCE(%s::text, ''), COALESCE(%s::text, ''), %s,
		COALESCE(%s, '{}'), COALESCE(%s, ''), %s,
		COALESCE(%s, ''), COALESCE(%s, ''),
		COALESCE(%s::text, ''), %s,
		COALESCE(%s::text, ''), %s, %s,
		COALESCE(%s::text, ''), COALESCE(%s, '{}'),
		%s, %s, %s,
		COALESCE(%s::text, ''), COALESCE(%s::text, ''), %s, %s`,
		col("id"),
		col("conversation_id"), col("contact_id"), col("items"),
		col("shipping_address"), col("notes"), col("status"),
		col("tracking_number"), col("courier"),
		col("payment_transaction_id"),
		col("payment_status"),
		col("payment_proof_message_id"),
		col("payment_proof_submitted_at"),
		col("payment_proof_verified_at"),
		col("payment_proof_verified_by"),
		col("payment_proof_meta"),
		col("subtotal"), col("shipping_cost"), col("total"),
		col("income_wallet_id"), col("created_by"), col("created_at"), col("updated_at"))
}

// ---------- endpoints ----------

//encore:api auth method=GET path=/api/v1/orders
func List(ctx context.Context, p *ListOrdersParams) (*ListOrdersResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}

	page := p.Page
	if page < 1 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	where := `WHERE o.deleted_at IS NULL`
	args := []any{}
	idx := 1

	if p.Status != "" {
		status := strings.ToLower(strings.TrimSpace(p.Status))
		if !validOrderStatuses[status] {
			return nil, appErrs.BadRequest("invalid order status")
		}
		where += fmt.Sprintf(` AND o.status = $%d`, idx)
		args = append(args, status)
		idx++
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		piiActive, _ := tenantschema.TableColumnExists(ctx, db.Stdlib(), u.TenantSchema, "contact", "phone_number_idx")
		frag, extra := orderContactSearchSQL(idx, q, piiActive)
		where += " AND " + frag
		args = append(args, extra...)
		idx += len(extra)
	}

	var total int
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*)
		 FROM "%s"."order" o
		 LEFT JOIN "%s".contact c ON c.id = o.contact_id
		 %s`, u.TenantSchema, u.TenantSchema, where),
		args...).Scan(&total)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(
		`SELECT %s, %s
		 FROM "%s"."order" o
		 LEFT JOIN "%s".contact c ON c.id = o.contact_id
		 %s
		 ORDER BY o.created_at DESC
		 LIMIT $%d OFFSET $%d`,
		orderSelectCols("o"), contactJoinCols, u.TenantSchema, u.TenantSchema, where, idx, idx+1)
	args = append(args, pageSize, offset)

	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := make([]Order, 0)
	for rows.Next() {
		o, err := scanOrderWithContact(rows.Scan)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return &ListOrdersResponse{Orders: orders, Total: total, Page: page, PageSize: pageSize}, nil
}

//encore:api auth method=GET path=/api/v1/orders/:id
func Get(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}

	row := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT %s, %s
		 FROM "%s"."order" o
		 LEFT JOIN "%s".contact c ON c.id = o.contact_id
		 WHERE o.id=$1 AND o.deleted_at IS NULL`,
		orderSelectCols("o"), contactJoinCols, u.TenantSchema, u.TenantSchema), id)

	o, err := scanOrderWithContact(row.Scan)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	return &o, nil
}

//encore:api auth method=POST path=/api/v1/orders
func Create(ctx context.Context, p *CreateOrderParams) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if len(p.Items) == 0 {
		return nil, appErrs.BadRequest("items are required")
	}

	items, err := normalizeOrderItems(ctx, u.TenantSchema, strings.TrimSpace(p.ContactID), p.Items)
	if err != nil {
		return nil, err
	}

	var subtotal float64
	for _, it := range items {
		subtotal += it.Qty * it.UnitPrice
	}
	status := strings.ToLower(strings.TrimSpace(p.Status))
	if status == "" {
		status = "draft"
	}
	if !validOrderStatuses[status] {
		return nil, appErrs.BadRequest("invalid order status")
	}
	if p.ShippingCost < 0 {
		return nil, appErrs.BadRequest("shipping cost cannot be negative")
	}
	total := subtotal + p.ShippingCost

	// Fail fast before writing the order row — if the finance period is locked
	// we cannot record income afterwards, which would leave the order in an
	// inconsistent state (status=completed but no finance entry).
	if status == "completed" {
		if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
			return nil, err
		}
	}
	if err := finance.ValidateIncomeWallet(ctx, u.TenantSchema, p.IncomeWalletID); err != nil {
		return nil, err
	}
	// Fail fast if creating directly into a stock-committed status would oversell.
	if inventory.IsCommittedOrderStatus(status) {
		if err := inventory.PrecheckOrderStock(ctx, u.TenantSchema, "", orderStockItems(items)); err != nil {
			return nil, err
		}
	}

	itemsJSON, _ := json.Marshal(items)
	addrJSON, _ := json.Marshal(p.ShippingAddress)
	convID, contactID := nullUUIDArg(p.ConversationID), nullUUIDArg(p.ContactID)

	row := db.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, shipping_address, notes,
			 status, tracking_number, courier, subtotal, shipping_cost, total,
			 income_wallet_id, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING %s`,
		u.TenantSchema, orderSelectCols("")),
		convID, contactID, itemsJSON, addrJSON, p.Notes,
		status, strings.TrimSpace(p.TrackingNumber), strings.TrimSpace(p.Courier),
		subtotal, p.ShippingCost, total, nullUUIDArg(p.IncomeWalletID), u.AccountID)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	if status == "completed" {
		if err := finance.RecordOrderCompletedIncome(ctx, u.TenantSchema, u.AccountID, o.ID, o.Total, o.IncomeWalletID); err != nil {
			return nil, err
		}
	}
	if err := inventory.SyncOrderStock(ctx, u.TenantSchema, o.ID, o.Status, orderStockItems(o.Items), u.AccountID); err != nil {
		return nil, err
	}
	return &o, nil
}

//encore:api auth method=PATCH path=/api/v1/orders/:id
func Update(ctx context.Context, id string, req *UpdateOrderParams) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}

	sets := []string{}
	args := []any{}
	idx := 1
	var updatedShippingCost *float64
	newStatus := ""
	walletUpdated := false

	if req.ContactID != nil {
		sets = append(sets, fmt.Sprintf("contact_id=$%d", idx))
		args = append(args, nullUUIDArg(*req.ContactID))
		idx++
	}
	if req.Notes != nil {
		sets = append(sets, fmt.Sprintf("notes=$%d", idx))
		args = append(args, strings.TrimSpace(*req.Notes))
		idx++
	}
	var updatedSubtotal *float64
	var normalizedItems []OrderItem
	if len(req.Items) > 0 {
		contactID := ""
		if req.ContactID != nil {
			contactID = strings.TrimSpace(*req.ContactID)
		} else {
			_ = db.QueryRow(ctx, fmt.Sprintf(
				`SELECT COALESCE(contact_id::text, '') FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
				u.TenantSchema), id).Scan(&contactID)
		}
		items, ierr := normalizeOrderItems(ctx, u.TenantSchema, contactID, req.Items)
		if ierr != nil {
			return nil, ierr
		}
		normalizedItems = items
		subtotal := 0.0
		for _, it := range items {
			subtotal += it.Qty * it.UnitPrice
		}
		itemsJSON, _ := json.Marshal(items)
		sets = append(sets, fmt.Sprintf("items=$%d", idx))
		args = append(args, itemsJSON)
		idx++
		sets = append(sets, fmt.Sprintf("subtotal=$%d", idx))
		args = append(args, subtotal)
		idx++
		updatedSubtotal = &subtotal
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if !validOrderStatuses[status] {
			return nil, appErrs.BadRequest("invalid order status")
		}
		newStatus = status
		sets = append(sets, fmt.Sprintf("status=$%d", idx))
		args = append(args, status)
		idx++
	}
	if req.TrackingNumber != nil {
		sets = append(sets, fmt.Sprintf("tracking_number=$%d", idx))
		args = append(args, *req.TrackingNumber)
		idx++
	}
	if req.Courier != nil {
		sets = append(sets, fmt.Sprintf("courier=$%d", idx))
		args = append(args, *req.Courier)
		idx++
	}
	if req.PaymentTransactionID != nil {
		sets = append(sets, fmt.Sprintf("payment_transaction_id=$%d", idx))
		args = append(args, *req.PaymentTransactionID)
		idx++
	}
	if req.ShippingCost != nil {
		sets = append(sets, fmt.Sprintf("shipping_cost=$%d", idx))
		args = append(args, *req.ShippingCost)
		idx++
		updatedShippingCost = req.ShippingCost
	}
	if req.IncomeWalletID != nil {
		if err := finance.ValidateIncomeWallet(ctx, u.TenantSchema, *req.IncomeWalletID); err != nil {
			return nil, err
		}
		sets = append(sets, fmt.Sprintf("income_wallet_id=$%d", idx))
		args = append(args, nullUUIDArg(*req.IncomeWalletID))
		idx++
		walletUpdated = true
	}
	if updatedSubtotal != nil && updatedShippingCost != nil {
		sets = append(sets, fmt.Sprintf("total=$%d", idx))
		args = append(args, *updatedSubtotal+*updatedShippingCost)
		idx++
	} else if updatedSubtotal != nil {
		sets = append(sets, fmt.Sprintf("total=$%d+shipping_cost", idx))
		args = append(args, *updatedSubtotal)
		idx++
	} else if updatedShippingCost != nil {
		sets = append(sets, fmt.Sprintf("total=subtotal+$%d", idx))
		args = append(args, *updatedShippingCost)
		idx++
	}
	if len(sets) == 0 {
		return nil, appErrs.BadRequest("no fields to update")
	}

	// Same fail-fast as Create: check period before touching the order row.
	if newStatus == "completed" {
		if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
			return nil, err
		}
	}
	// Fail fast on overselling before committing a transition into a committed status.
	if inventory.IsCommittedOrderStatus(newStatus) {
		checkItems := normalizedItems
		if checkItems == nil {
			checkItems, err = loadOrderItems(ctx, u.TenantSchema, id)
			if err != nil {
				return nil, err
			}
		}
		if err := inventory.PrecheckOrderStock(ctx, u.TenantSchema, id, orderStockItems(checkItems)); err != nil {
			return nil, err
		}
	}

	sets = append(sets, "updated_at=NOW()")
	args = append(args, id)

	q := fmt.Sprintf(
		`UPDATE "%s"."order" SET %s WHERE id=$%d AND deleted_at IS NULL RETURNING %s`,
		u.TenantSchema, joinStrings(sets, ", "), idx, orderSelectCols(""))

	o, err := scanOrder(db.QueryRow(ctx, q, args...).Scan)
	if err != nil {
		return nil, fmt.Errorf("update order: %w", err)
	}

	if newStatus == "completed" {
		if err := finance.ResyncOrderCompletedIncome(ctx, u.TenantSchema, u.AccountID, o.ID, o.Total, o.IncomeWalletID); err != nil {
			return nil, err
		}
	} else if newStatus == "draft" || newStatus == "cancelled" {
		if err := finance.RemoveOrderIncomeTransaction(ctx, u.TenantSchema, o.ID); err != nil {
			return nil, err
		}
	} else if shouldResyncCompletedOrderIncome(newStatus, o.Status, updatedSubtotal, updatedShippingCost, walletUpdated) {
		if err := finance.ResyncOrderCompletedIncome(ctx, u.TenantSchema, u.AccountID, o.ID, o.Total, o.IncomeWalletID); err != nil {
			return nil, err
		}
	}
	if newStatus != "" || len(req.Items) > 0 {
		if err := inventory.SyncOrderStock(ctx, u.TenantSchema, o.ID, o.Status, orderStockItems(o.Items), u.AccountID); err != nil {
			return nil, err
		}
	}
	return &o, nil
}

//encore:api auth method=PATCH path=/api/v1/orders/:id/cancel
func Cancel(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}

	if err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT status FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id).Scan(new(string)); err != nil {
		return nil, appErrs.NotFound("order not found")
	}

	row := db.QueryRow(ctx, fmt.Sprintf(
		`UPDATE "%s"."order" SET status='cancelled', updated_at=NOW()
		 WHERE id=$1 AND deleted_at IS NULL RETURNING %s`,
		u.TenantSchema, orderSelectCols("")), id)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}
	if err := finance.RemoveOrderIncomeTransaction(ctx, u.TenantSchema, o.ID); err != nil {
		return nil, err
	}
	if err := inventory.SyncOrderStock(ctx, u.TenantSchema, o.ID, "cancelled", orderStockItems(o.Items), u.AccountID); err != nil {
		return nil, err
	}
	return &o, nil
}

// loadOrderItems reads an order's line items (for stock precheck / restore on delete).
func loadOrderItems(ctx context.Context, schema, id string) ([]OrderItem, error) {
	var raw []byte
	err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COALESCE(items, '[]') FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`, schema), id).Scan(&raw)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	var items []OrderItem
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &items)
	}
	return items, nil
}

func shouldPrecheckBatchStockTransition(targetStatus, currentStatus string) bool {
	return inventory.IsCommittedOrderStatus(targetStatus) && !inventory.IsCommittedOrderStatus(currentStatus)
}

//encore:api auth method=PATCH path=/api/v1/order-status/batch
func BatchUpdateStatus(ctx context.Context, req *BatchUpdateStatusParams) (*BatchUpdateStatusResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}
	if req == nil || len(req.IDs) == 0 {
		return nil, appErrs.BadRequest("ids are required")
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if !validOrderStatuses[status] {
		return nil, appErrs.BadRequest("invalid order status")
	}

	idPH, idArgs := batchIDs(req.IDs, 1)
	if len(idPH) == 0 {
		return nil, appErrs.BadRequest("ids are required")
	}
	updatePH := make([]string, len(idPH))
	for i := range idPH {
		updatePH[i] = fmt.Sprintf("$%d", i+2)
	}
	updateArgs := append([]any{status}, idArgs...)

	if status == "completed" {
		// Pre-flight: reject before touching any order row if the finance period is locked.
		if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
			return nil, err
		}
	}
	skipCond := ""
	if status == "completed" {
		// Skip orders already completed so income is not re-recorded.
		skipCond = " AND status <> 'completed'"
	}

	// Load targets first — fail stock precheck before mutating any row (same as PATCH /orders/:id).
	pendingQ := fmt.Sprintf(
		`SELECT %s FROM "%s"."order"
		 WHERE id IN (%s) AND deleted_at IS NULL%s`,
		orderSelectCols(""), u.TenantSchema, strings.Join(idPH, ", "), skipCond)
	pendingRows, err := db.Query(ctx, pendingQ, idArgs...)
	if err != nil {
		return nil, fmt.Errorf("batch load orders: %w", err)
	}
	pending := make([]Order, 0, len(idPH))
	for pendingRows.Next() {
		o, serr := scanOrder(pendingRows.Scan)
		if serr != nil {
			pendingRows.Close()
			return nil, serr
		}
		pending = append(pending, o)
	}
	pendingRows.Close()
	if err := pendingRows.Err(); err != nil {
		return nil, fmt.Errorf("batch load orders: %w", err)
	}
	if inventory.IsCommittedOrderStatus(status) {
		for _, o := range pending {
			if shouldPrecheckBatchStockTransition(status, o.Status) {
				if err := inventory.PrecheckOrderStock(ctx, u.TenantSchema, o.ID, orderStockItems(o.Items)); err != nil {
					return nil, err
				}
			}
		}
	}

	q := fmt.Sprintf(
		`UPDATE "%s"."order"
		 SET status=$1, updated_at=NOW()
		 WHERE id IN (%s) AND deleted_at IS NULL%s
		 RETURNING %s`,
		u.TenantSchema, strings.Join(updatePH, ", "), skipCond, orderSelectCols(""))

	rows, err := db.Query(ctx, q, updateArgs...)
	if err != nil {
		return nil, fmt.Errorf("batch update order status: %w", err)
	}
	orders := make([]Order, 0, len(idPH))
	for rows.Next() {
		o, serr := scanOrder(rows.Scan)
		if serr != nil {
			rows.Close()
			return nil, serr
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch update order status: %w", err)
	}

	updated := 0
	for _, o := range orders {
		updated++
		switch {
		case status == "completed":
			if err := finance.RecordOrderCompletedIncome(ctx, u.TenantSchema, u.AccountID, o.ID, o.Total, o.IncomeWalletID); err != nil {
				return nil, err
			}
		case status == "draft" || status == "cancelled":
			if err := finance.RemoveOrderIncomeTransaction(ctx, u.TenantSchema, o.ID); err != nil {
				return nil, err
			}
		}
		if err := inventory.SyncOrderStock(ctx, u.TenantSchema, o.ID, o.Status, orderStockItems(o.Items), u.AccountID); err != nil {
			return nil, err
		}
	}
	return &BatchUpdateStatusResponse{Updated: updated}, nil
}

//encore:api auth method=DELETE path=/api/v1/orders/:id
func Delete(ctx context.Context, id string) error {
	u, err := getUser()
	if err != nil {
		return err
	}
	if !u.CanPerformOwnerActions() {
		return appErrs.Forbidden("owner access required")
	}

	if err := finance.RemoveOrderIncomeTransaction(ctx, u.TenantSchema, id); err != nil {
		return err
	}
	// Restore any issued stock (treat delete like cancel) before removing the row.
	if items, ierr := loadOrderItems(ctx, u.TenantSchema, id); ierr == nil {
		if err := inventory.SyncOrderStock(ctx, u.TenantSchema, id, "cancelled", orderStockItems(items), u.AccountID); err != nil {
			return err
		}
	}

	uid, _ := auth.UserID()
	res, err := db.Exec(ctx, fmt.Sprintf(`
		UPDATE "%s"."order"
		SET deleted_at=NOW(), deleted_by=$1, updated_at=NOW()
		WHERE id=$2 AND deleted_at IS NULL`, u.TenantSchema), string(uid), id)
	if err != nil {
		return fmt.Errorf("delete order: %w", err)
	}
	n := res.RowsAffected()
	if n == 0 {
		return appErrs.NotFound("order not found")
	}
	return nil
}

//encore:api auth method=PATCH path=/api/v1/order-delete/batch
func BatchDelete(ctx context.Context, req *BatchDeleteParams) (*BatchDeleteResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}
	placeholders, args := batchIDs(req.IDs, 1)
	if len(placeholders) == 0 {
		return nil, appErrs.BadRequest("ids are required")
	}

	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT id::text FROM "%s"."order" WHERE id IN (%s) AND deleted_at IS NULL`,
		u.TenantSchema, strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return nil, fmt.Errorf("batch delete orders: %w", err)
	}
	defer rows.Close()

	orderIDs := make([]string, 0, len(req.IDs))
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch delete orders: %w", err)
	}
	for _, orderID := range orderIDs {
		if items, ierr := loadOrderItems(ctx, u.TenantSchema, orderID); ierr == nil {
			if err := inventory.SyncOrderStock(ctx, u.TenantSchema, orderID, "cancelled", orderStockItems(items), u.AccountID); err != nil {
				return nil, err
			}
		}
		if err := finance.RemoveOrderIncomeTransaction(ctx, u.TenantSchema, orderID); err != nil {
			return nil, err
		}
	}

	uid, _ := auth.UserID()
	deletePlaceholders, deleteArgs := batchIDs(orderIDs, 2)
	if len(deletePlaceholders) == 0 {
		return &BatchDeleteResponse{Deleted: 0}, nil
	}
	execArgs := []any{string(uid)}
	execArgs = append(execArgs, deleteArgs...)
	res, err := db.Exec(ctx, fmt.Sprintf(`
		UPDATE "%s"."order"
		SET deleted_at=NOW(), deleted_by=$1, updated_at=NOW()
		WHERE id IN (%s) AND deleted_at IS NULL`, u.TenantSchema, strings.Join(deletePlaceholders, ", ")), execArgs...)
	if err != nil {
		return nil, fmt.Errorf("batch delete orders: %w", err)
	}
	return &BatchDeleteResponse{Deleted: int(res.RowsAffected())}, nil
}

// ---------- internal ----------

func getUser() (*types.AuthUser, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil {
		return nil, appErrs.Unauthenticated("missing auth data")
	}
	return u, nil
}

// genLineID returns a random UUIDv4-style id for an order line (stock traceability).
func genLineID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("line-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

// orderStockItems maps order lines to the inventory sync view (catalog items only).
func orderStockItems(items []OrderItem) []inventory.OrderStockItem {
	out := make([]inventory.OrderStockItem, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.CatalogItemID) == "" {
			continue
		}
		out = append(out, inventory.OrderStockItem{
			LineID:        it.LineID,
			CatalogItemID: it.CatalogItemID,
			WarehouseID:   it.WarehouseID,
			Qty:           it.Qty,
		})
	}
	return out
}

func normalizeOrderItems(ctx context.Context, schema, contactID string, raw []OrderItem) ([]OrderItem, error) {
	conn, err := tenant.TenantConn(ctx, schema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := pricing.EnsureSchema(ctx, conn); err != nil {
		return nil, appErrs.Internal("prepare pricing failed")
	}
	priceTypeID, err := pricing.ResolvePriceTypeIDForContact(ctx, conn, contactID)
	if err != nil {
		return nil, err
	}

	items := make([]OrderItem, 0, len(raw))
	for i, it := range raw {
		if it.Qty <= 0 {
			return nil, appErrs.BadRequest("item qty must be greater than zero")
		}
		item, err := resolveCatalogItem(ctx, schema, priceTypeID, it, i)
		if err != nil {
			return nil, err
		}
		item.LineID = strings.TrimSpace(it.LineID)
		if item.LineID == "" {
			item.LineID = genLineID()
		}
		item.WarehouseID = strings.TrimSpace(it.WarehouseID)
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, appErrs.BadRequest("items are required")
	}
	return items, nil
}

func resolveCatalogItem(ctx context.Context, schema, priceTypeID string, it OrderItem, index int) (OrderItem, error) {
	if strings.TrimSpace(it.CatalogItemID) != "" {
		item, err := loadCatalogOrderItem(ctx, schema, priceTypeID, "id = $1", strings.TrimSpace(it.CatalogItemID))
		if err != nil {
			return OrderItem{}, err
		}
		item.Qty = it.Qty
		return item, nil
	}

	code := strings.TrimSpace(it.ExternalCode)
	name := strings.TrimSpace(it.Name)
	if code != "" {
		if item, err := loadCatalogOrderItem(ctx, schema, priceTypeID, "LOWER(external_code) = LOWER($1)", code); err == nil {
			item.Qty = it.Qty
			return item, nil
		}
	}
	if name != "" {
		if item, err := loadCatalogOrderItem(ctx, schema, priceTypeID, "LOWER(name) = LOWER($1)", name); err == nil {
			item.Qty = it.Qty
			return item, nil
		}
	}
	if name == "" {
		return OrderItem{}, appErrs.BadRequest("catalog item name is required")
	}
	if it.UnitPrice < 0 {
		return OrderItem{}, appErrs.BadRequest("catalog item price cannot be negative")
	}
	if code == "" {
		code = fmt.Sprintf("MANUAL-%d-%d", time.Now().UnixNano(), index+1)
	}
	return createCatalogOrderItem(ctx, schema, code, name, it.UnitPrice, strings.TrimSpace(it.SellUnit), it.Qty)
}

func loadCatalogOrderItem(ctx context.Context, schema, priceTypeID, condition string, arg any) (OrderItem, error) {
	var item OrderItem
	var unit sql.NullString
	row := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT id::text, external_code, name, sell_unit
		FROM "%s".business_catalog_item
		WHERE deleted_at IS NULL AND %s
		ORDER BY updated_at DESC
		LIMIT 1`, schema, condition), arg)
	if err := row.Scan(&item.CatalogItemID, &item.ExternalCode, &item.Name, &unit); err != nil {
		return OrderItem{}, appErrs.NotFound("catalog item not found")
	}
	conn, err := tenant.TenantConn(ctx, schema)
	if err != nil {
		return OrderItem{}, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	unitPrice, err := pricing.ResolveCatalogUnitPrice(ctx, conn, item.CatalogItemID, priceTypeID)
	if err != nil {
		return OrderItem{}, err
	}
	item.UnitPrice = unitPrice
	if unit.Valid {
		item.SellUnit = unit.String
	}
	return item, nil
}

func createCatalogOrderItem(ctx context.Context, schema, code, name string, price float64, unit string, qty float64) (OrderItem, error) {
	var item OrderItem
	var sqlUnit any
	if unit != "" {
		sqlUnit = unit
	}
	err := db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO "%s".business_catalog_item (external_code, name, sell_price, sell_unit, is_active, source)
		VALUES ($1, $2, $3, $4, true, 'manual')
		RETURNING id::text, external_code, name, COALESCE(sell_price, 0), COALESCE(sell_unit, '')`, schema),
		code, name, price, sqlUnit).Scan(&item.CatalogItemID, &item.ExternalCode, &item.Name, &item.UnitPrice, &item.SellUnit)
	if err != nil {
		return OrderItem{}, fmt.Errorf("create catalog item: %w", err)
	}
	item.Qty = qty
	return item, nil
}

func scanOrder(scan func(dest ...any) error) (Order, error) {
	var o Order
	var itemsRaw, addrRaw, paymentMetaRaw []byte
	if err := scan(
		&o.ID, &o.ConversationID, &o.ContactID, &itemsRaw,
		&addrRaw, &o.Notes, &o.Status,
		&o.TrackingNumber, &o.Courier,
		&o.PaymentTransactionID, &o.PaymentStatus,
		&o.PaymentProofMessageID,
		&o.PaymentProofSubmittedAt, &o.PaymentProofVerifiedAt, &o.PaymentProofVerifiedBy,
		&paymentMetaRaw,
		&o.Subtotal, &o.ShippingCost, &o.Total,
		&o.IncomeWalletID, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return o, err
	}
	o.OrderNumber = FormatOrderNumber(o.ID)
	if len(itemsRaw) > 0 {
		_ = json.Unmarshal(itemsRaw, &o.Items)
	}
	if o.Items == nil {
		o.Items = []OrderItem{}
	}
	if len(addrRaw) > 2 {
		var addr ShippingAddress
		if json.Unmarshal(addrRaw, &addr) == nil && addr.Street != "" {
			o.ShippingAddress = &addr
		}
	}
	applyPaymentProofMeta(&o, paymentMetaRaw)
	return o, nil
}

func applyPaymentProofMeta(o *Order, raw []byte) {
	if o == nil || len(raw) <= 2 {
		return
	}
	var meta PaymentProofMeta
	if json.Unmarshal(raw, &meta) == nil {
		o.PaymentProofMeta = &meta
	}
}

// nullUUIDArg maps "" to SQL NULL for optional UUID columns.
func nullUUIDArg(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func batchIDs(ids []string, start int) ([]string, []any) {
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+len(args)))
		args = append(args, id)
	}
	return placeholders, args
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
