package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
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
	CatalogItemID string  `json:"catalogItemId,omitempty"`
	ExternalCode  string  `json:"externalCode,omitempty"`
	Name          string  `json:"name"`
	Variant       string  `json:"variant,omitempty"`
	Size          string  `json:"size,omitempty"`
	Color         string  `json:"color,omitempty"`
	Qty           int     `json:"qty"`
	UnitPrice     float64 `json:"unitPrice"`
	SellUnit      string  `json:"sellUnit,omitempty"`
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

type Order struct {
	ID                   string           `json:"id"`
	ConversationID       string           `json:"conversationId"`
	ContactID            string           `json:"contactId"`
	Items                []OrderItem      `json:"items"`
	ShippingAddress      *ShippingAddress `json:"shippingAddress,omitempty"`
	Notes                string           `json:"notes"`
	Status               string           `json:"status"`
	TrackingNumber       string           `json:"trackingNumber"`
	Courier              string           `json:"courier"`
	PaymentTransactionID string           `json:"paymentTransactionId"`
	Subtotal             float64          `json:"subtotal"`
	ShippingCost         float64          `json:"shippingCost"`
	Total                float64          `json:"total"`
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
		COALESCE(%s::text, ''), %s, %s, %s,
		COALESCE(%s::text, ''), %s, %s`,
		col("id"),
		col("conversation_id"), col("contact_id"), col("items"),
		col("shipping_address"), col("notes"), col("status"),
		col("tracking_number"), col("courier"),
		col("payment_transaction_id"), col("subtotal"), col("shipping_cost"), col("total"),
		col("created_by"), col("created_at"), col("updated_at"))
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
		args = append(args, "%"+q+"%")
		where += fmt.Sprintf(` AND (
			o.id::text ILIKE $%[1]d OR
			COALESCE(o.notes, '') ILIKE $%[1]d OR
			COALESCE(o.tracking_number, '') ILIKE $%[1]d OR
			COALESCE(o.courier, '') ILIKE $%[1]d OR
			o.items::text ILIKE $%[1]d OR
			COALESCE(c.display_name, '') ILIKE $%[1]d OR
			COALESCE(c.phone_number, '') ILIKE $%[1]d
		)`, idx)
		idx++
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
		`SELECT %s
		 FROM "%s"."order" o
		 LEFT JOIN "%s".contact c ON c.id = o.contact_id
		 %s
		 ORDER BY o.created_at DESC
		 LIMIT $%d OFFSET $%d`,
		orderSelectCols("o"), u.TenantSchema, u.TenantSchema, where, idx, idx+1)
	args = append(args, pageSize, offset)

	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := make([]Order, 0)
	for rows.Next() {
		o, err := scanOrder(rows.Scan)
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
		`SELECT %s FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		orderSelectCols(""), u.TenantSchema), id)

	o, err := scanOrder(row.Scan)
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

	items, err := normalizeOrderItems(ctx, u.TenantSchema, p.Items)
	if err != nil {
		return nil, err
	}

	var subtotal float64
	for _, it := range items {
		subtotal += float64(it.Qty) * it.UnitPrice
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

	itemsJSON, _ := json.Marshal(items)
	addrJSON, _ := json.Marshal(p.ShippingAddress)
	convID, contactID := nullUUIDArg(p.ConversationID), nullUUIDArg(p.ContactID)

	row := db.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, shipping_address, notes,
			 status, tracking_number, courier, subtotal, shipping_cost, total, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING %s`,
		u.TenantSchema, orderSelectCols("")),
		convID, contactID, itemsJSON, addrJSON, p.Notes,
		status, strings.TrimSpace(p.TrackingNumber), strings.TrimSpace(p.Courier),
		subtotal, p.ShippingCost, total, u.AccountID)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
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
	if len(req.Items) > 0 {
		items, err := normalizeOrderItems(ctx, u.TenantSchema, req.Items)
		if err != nil {
			return nil, err
		}
		subtotal := 0.0
		for _, it := range items {
			subtotal += float64(it.Qty) * it.UnitPrice
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

	sets = append(sets, "updated_at=NOW()")
	args = append(args, id)

	q := fmt.Sprintf(
		`UPDATE "%s"."order" SET %s WHERE id=$%d AND deleted_at IS NULL RETURNING %s`,
		u.TenantSchema, joinStrings(sets, ", "), idx, orderSelectCols(""))

	o, err := scanOrder(db.QueryRow(ctx, q, args...).Scan)
	if err != nil {
		return nil, fmt.Errorf("update order: %w", err)
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
	return &o, nil
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

	placeholders := make([]string, 0, len(req.IDs))
	args := []any{status}
	for _, id := range req.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)+1))
		args = append(args, id)
	}
	if len(placeholders) == 0 {
		return nil, appErrs.BadRequest("ids are required")
	}

	res, err := db.Exec(ctx, fmt.Sprintf(
		`UPDATE "%s"."order"
		 SET status=$1, updated_at=NOW()
		 WHERE id IN (%s) AND deleted_at IS NULL`,
		u.TenantSchema, strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return nil, fmt.Errorf("batch update order status: %w", err)
	}
	n := res.RowsAffected()
	return &BatchUpdateStatusResponse{Updated: int(n)}, nil
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
	placeholders, args := batchIDs(req.IDs, 2)
	if len(placeholders) == 0 {
		return nil, appErrs.BadRequest("ids are required")
	}
	uid, _ := auth.UserID()
	execArgs := []any{string(uid)}
	execArgs = append(execArgs, args...)
	res, err := db.Exec(ctx, fmt.Sprintf(`
		UPDATE "%s"."order"
		SET deleted_at=NOW(), deleted_by=$1, updated_at=NOW()
		WHERE id IN (%s) AND deleted_at IS NULL`, u.TenantSchema, strings.Join(placeholders, ", ")), execArgs...)
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

func normalizeOrderItems(ctx context.Context, schema string, raw []OrderItem) ([]OrderItem, error) {
	items := make([]OrderItem, 0, len(raw))
	for i, it := range raw {
		if it.Qty <= 0 {
			return nil, appErrs.BadRequest("item qty must be greater than zero")
		}
		item, err := resolveCatalogItem(ctx, schema, it, i)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, appErrs.BadRequest("items are required")
	}
	return items, nil
}

func resolveCatalogItem(ctx context.Context, schema string, it OrderItem, index int) (OrderItem, error) {
	if strings.TrimSpace(it.CatalogItemID) != "" {
		item, err := loadCatalogOrderItem(ctx, schema, "id = $1", strings.TrimSpace(it.CatalogItemID))
		if err != nil {
			return OrderItem{}, err
		}
		item.Qty = it.Qty
		return item, nil
	}

	code := strings.TrimSpace(it.ExternalCode)
	name := strings.TrimSpace(it.Name)
	if code != "" {
		if item, err := loadCatalogOrderItem(ctx, schema, "LOWER(external_code) = LOWER($1)", code); err == nil {
			item.Qty = it.Qty
			return item, nil
		}
	}
	if name != "" {
		if item, err := loadCatalogOrderItem(ctx, schema, "LOWER(name) = LOWER($1)", name); err == nil {
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

func loadCatalogOrderItem(ctx context.Context, schema, condition string, arg any) (OrderItem, error) {
	var item OrderItem
	var price sql.NullFloat64
	var unit sql.NullString
	row := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT id::text, external_code, name, sell_price, sell_unit
		FROM "%s".business_catalog_item
		WHERE deleted_at IS NULL AND %s
		ORDER BY updated_at DESC
		LIMIT 1`, schema, condition), arg)
	if err := row.Scan(&item.CatalogItemID, &item.ExternalCode, &item.Name, &price, &unit); err != nil {
		return OrderItem{}, appErrs.NotFound("catalog item not found")
	}
	if price.Valid {
		item.UnitPrice = price.Float64
	}
	if unit.Valid {
		item.SellUnit = unit.String
	}
	return item, nil
}

func createCatalogOrderItem(ctx context.Context, schema, code, name string, price float64, unit string, qty int) (OrderItem, error) {
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
	var itemsRaw, addrRaw []byte
	if err := scan(
		&o.ID, &o.ConversationID, &o.ContactID, &itemsRaw,
		&addrRaw, &o.Notes, &o.Status,
		&o.TrackingNumber, &o.Courier,
		&o.PaymentTransactionID, &o.Subtotal, &o.ShippingCost, &o.Total,
		&o.CreatedBy, &o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return o, err
	}
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
	return o, nil
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
