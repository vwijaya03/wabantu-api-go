package order

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

// ---------- status machine ----------

var validTransitions = map[string][]string{
	"draft":     {"confirmed", "cancelled"},
	"confirmed": {"paid", "cancelled"},
	"paid":      {"shipped", "cancelled"},
	"shipped":   {"completed"},
}

// ---------- types ----------

type OrderItem struct {
	Name      string  `json:"name"`
	Variant   string  `json:"variant"`
	Qty       int     `json:"qty"`
	UnitPrice float64 `json:"unitPrice"`
}

type ShippingAddress struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Street     string `json:"street"`
	City       string `json:"city"`
	CityID     string `json:"cityId"`
	Province   string `json:"province"`
	ProvinceID string `json:"provinceId"`
	PostalCode string `json:"postalCode"`
}

type Order struct {
	ID                   string           `json:"id"`
	ConversationID       string           `json:"conversationId"`
	ContactID            string           `json:"contactId"`
	Items                []OrderItem      `json:"items"`
	ShippingAddress      *ShippingAddress  `json:"shippingAddress,omitempty"`
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
	ConversationID  string          `json:"conversationId"`
	ContactID       string          `json:"contactId"`
	Items           []OrderItem     `json:"items"`
	ShippingAddress *ShippingAddress `json:"shippingAddress,omitempty"`
	Notes           string          `json:"notes,omitempty"`
}

type UpdateOrderParams struct {
	Status               *string  `json:"status,omitempty"`
	TrackingNumber       *string  `json:"trackingNumber,omitempty"`
	Courier              *string  `json:"courier,omitempty"`
	PaymentTransactionID *string  `json:"paymentTransactionId,omitempty"`
	ShippingCost         *float64 `json:"shippingCost,omitempty"`
}

type ListOrdersParams struct {
	Status   string `query:"status"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListOrdersResponse struct {
	Orders []Order `json:"orders"`
	Total  int     `json:"total"`
}

// ---------- endpoints ----------

//encore:api auth method=GET path=/orders
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

	where := `WHERE deleted_at IS NULL`
	args := []any{}
	idx := 1

	if p.Status != "" {
		where += fmt.Sprintf(` AND status = $%d`, idx)
		args = append(args, p.Status)
		idx++
	}

	var total int
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM "%s"."order" %s`, u.TenantSchema, where),
		args...).Scan(&total)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(
		`SELECT id, COALESCE(conversation_id,''), COALESCE(contact_id,''), items,
		        COALESCE(shipping_address,'{}'), COALESCE(notes,''), status,
		        COALESCE(tracking_number,''), COALESCE(courier,''),
		        COALESCE(payment_transaction_id,''), subtotal, shipping_cost, total,
		        created_by, created_at, updated_at
		 FROM "%s"."order" %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		u.TenantSchema, where, idx, idx+1)
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
	return &ListOrdersResponse{Orders: orders, Total: total}, nil
}

//encore:api auth method=GET path=/orders/:id
func Get(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}

	row := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, COALESCE(conversation_id,''), COALESCE(contact_id,''), items,
		        COALESCE(shipping_address,'{}'), COALESCE(notes,''), status,
		        COALESCE(tracking_number,''), COALESCE(courier,''),
		        COALESCE(payment_transaction_id,''), subtotal, shipping_cost, total,
		        created_by, created_at, updated_at
		 FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`, u.TenantSchema), id)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	return &o, nil
}

//encore:api auth method=POST path=/orders
func Create(ctx context.Context, p *CreateOrderParams) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if len(p.Items) == 0 {
		return nil, appErrs.BadRequest("items are required")
	}

	var subtotal float64
	for _, it := range p.Items {
		subtotal += float64(it.Qty) * it.UnitPrice
	}

	itemsJSON, _ := json.Marshal(p.Items)
	addrJSON, _ := json.Marshal(p.ShippingAddress)

	row := db.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO "%s"."order"
			(conversation_id, contact_id, items, shipping_address, notes,
			 status, subtotal, shipping_cost, total, created_by)
		 VALUES ($1,$2,$3,$4,$5,'draft',$6,0,$6,$7)
		 RETURNING id, COALESCE(conversation_id,''), COALESCE(contact_id,''), items,
		           COALESCE(shipping_address,'{}'), COALESCE(notes,''), status,
		           COALESCE(tracking_number,''), COALESCE(courier,''),
		           COALESCE(payment_transaction_id,''), subtotal, shipping_cost, total,
		           created_by, created_at, updated_at`,
		u.TenantSchema),
		p.ConversationID, p.ContactID, itemsJSON, addrJSON, p.Notes,
		subtotal, u.AccountID)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	return &o, nil
}

//encore:api auth method=PATCH path=/orders/:id
func Update(ctx context.Context, id string, req *UpdateOrderParams) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if u.Role != "owner" {
		return nil, appErrs.Forbidden("owner access required")
	}

	var currentStatus string
	if err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT status FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id).Scan(&currentStatus); err != nil {
		return nil, appErrs.NotFound("order not found")
	}

	sets := []string{}
	args := []any{}
	idx := 1

	if req.Status != nil {
		if !isValidTransition(currentStatus, *req.Status) {
			return nil, appErrs.BadRequest(fmt.Sprintf("cannot transition from %s to %s", currentStatus, *req.Status))
		}
		sets = append(sets, fmt.Sprintf("status=$%d", idx))
		args = append(args, *req.Status)
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
		sets = append(sets, fmt.Sprintf("total=subtotal+$%d", idx-1))
	}
	if len(sets) == 0 {
		return nil, appErrs.BadRequest("no fields to update")
	}

	sets = append(sets, "updated_at=NOW()")
	args = append(args, id)

	q := fmt.Sprintf(
		`UPDATE "%s"."order" SET %s WHERE id=$%d AND deleted_at IS NULL
		 RETURNING id, COALESCE(conversation_id,''), COALESCE(contact_id,''), items,
		           COALESCE(shipping_address,'{}'), COALESCE(notes,''), status,
		           COALESCE(tracking_number,''), COALESCE(courier,''),
		           COALESCE(payment_transaction_id,''), subtotal, shipping_cost, total,
		           created_by, created_at, updated_at`,
		u.TenantSchema, joinStrings(sets, ", "), idx)

	o, err := scanOrder(db.QueryRow(ctx, q, args...).Scan)
	if err != nil {
		return nil, fmt.Errorf("update order: %w", err)
	}
	return &o, nil
}

//encore:api auth method=PATCH path=/orders/:id/cancel
func Cancel(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if u.Role != "owner" {
		return nil, appErrs.Forbidden("owner access required")
	}

	var currentStatus string
	if err := db.QueryRow(ctx, fmt.Sprintf(
		`SELECT status FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id).Scan(&currentStatus); err != nil {
		return nil, appErrs.NotFound("order not found")
	}

	if !isValidTransition(currentStatus, "cancelled") {
		return nil, appErrs.BadRequest(fmt.Sprintf("cannot cancel order with status %s", currentStatus))
	}

	row := db.QueryRow(ctx, fmt.Sprintf(
		`UPDATE "%s"."order" SET status='cancelled', updated_at=NOW()
		 WHERE id=$1 AND deleted_at IS NULL
		 RETURNING id, COALESCE(conversation_id,''), COALESCE(contact_id,''), items,
		           COALESCE(shipping_address,'{}'), COALESCE(notes,''), status,
		           COALESCE(tracking_number,''), COALESCE(courier,''),
		           COALESCE(payment_transaction_id,''), subtotal, shipping_cost, total,
		           created_by, created_at, updated_at`,
		u.TenantSchema), id)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}
	return &o, nil
}

// ---------- internal ----------

func getUser() (*types.AuthUser, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil {
		return nil, appErrs.Unauthenticated("missing auth data")
	}
	return u, nil
}

func isValidTransition(from, to string) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
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
