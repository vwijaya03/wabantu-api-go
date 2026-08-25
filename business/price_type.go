package business

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	apperr "encore.app/wabantu/shared/errs"
)

type PriceType struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Label        string `json:"label"`
	DisplayOrder int    `json:"displayOrder"`
	IsDefault    bool   `json:"isDefault"`
	IsSystem     bool   `json:"isSystem"`
	IsActive     bool   `json:"isActive"`
}

type ListPriceTypesParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListPriceTypesResponse struct {
	Items    []PriceType `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

type CreatePriceTypeRequest struct {
	Code         string `json:"code"`
	Label        string `json:"label"`
	DisplayOrder *int   `json:"displayOrder,omitempty"`
	IsDefault    *bool  `json:"isDefault,omitempty"`
}

type UpdatePriceTypeRequest struct {
	Label        *string `json:"label,omitempty"`
	DisplayOrder *int    `json:"displayOrder,omitempty"`
	IsDefault    *bool   `json:"isDefault,omitempty"`
	IsActive     *bool   `json:"isActive,omitempty"`
}

//encore:api auth method=GET path=/api/v1/business/price-types
func ListPriceTypes(ctx context.Context, p *ListPriceTypesParams) (*ListPriceTypesResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 || p.PageSize > 100 {
		p.PageSize = 25
	}

	conn, err := tenantConn(ctx, user)
	if err != nil {
		return nil, err
	}
	defer closeTenantConn(conn)

	if err := ensurePricingSchema(ctx, conn, user.TenantSchema); err != nil {
		return nil, apperr.Internal("prepare price types failed")
	}
	if err := seedDefaultPriceTypes(ctx, conn); err != nil {
		return nil, apperr.Internal("seed price types failed")
	}

	conds := []string{"deleted_at IS NULL"}
	args := []any{}
	if q := strings.TrimSpace(p.Q); q != "" {
		args = append(args, "%"+q+"%")
		conds = append(conds, fmt.Sprintf("(code ILIKE $%d OR label ILIKE $%d)", len(args), len(args)))
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM business_price_type WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, apperr.Internal("count price types failed")
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, p.PageSize, (p.Page-1)*p.PageSize)
	limit := len(queryArgs) - 1
	offset := len(queryArgs)

	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, code, label, display_order, is_default, is_system, is_active
		FROM business_price_type
		WHERE %s
		ORDER BY display_order, label
		LIMIT $%d OFFSET $%d`, where, limit, offset), queryArgs...)
	if err != nil {
		return nil, apperr.Internal("list price types failed")
	}
	defer rows.Close()

	items := make([]PriceType, 0)
	for rows.Next() {
		var row PriceType
		if err := rows.Scan(&row.ID, &row.Code, &row.Label, &row.DisplayOrder, &row.IsDefault, &row.IsSystem, &row.IsActive); err != nil {
			return nil, apperr.Internal("scan price type failed")
		}
		items = append(items, row)
	}
	return &ListPriceTypesResponse{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/business/price-types tag:owner
func CreatePriceType(ctx context.Context, req *CreatePriceTypeRequest) (*PriceType, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can manage price types")
	}
	code := strings.ToLower(strings.TrimSpace(req.Code))
	label := strings.TrimSpace(req.Label)
	if code == "" || label == "" {
		return nil, apperr.BadRequest("code dan label wajib diisi")
	}
	if code == "umum" || code == "reseller" {
		return nil, apperr.BadRequest("code umum/reseller sudah dipakai sistem")
	}

	conn, err := tenantConn(ctx, user)
	if err != nil {
		return nil, err
	}
	defer closeTenantConn(conn)
	if err := ensurePricingSchema(ctx, conn, user.TenantSchema); err != nil {
		return nil, apperr.Internal("prepare price types failed")
	}

	order := 0
	if req.DisplayOrder != nil {
		order = *req.DisplayOrder
	}
	isDefault := false
	if req.IsDefault != nil && *req.IsDefault {
		isDefault = true
		_, _ = conn.ExecContext(ctx, `UPDATE business_price_type SET is_default = false WHERE deleted_at IS NULL`)
	}

	var row PriceType
	err = conn.QueryRowContext(ctx, `
		INSERT INTO business_price_type (code, label, display_order, is_default, is_system, is_active)
		VALUES ($1,$2,$3,$4,false,true)
		RETURNING id::text, code, label, display_order, is_default, is_system, is_active`,
		code, label, order, isDefault,
	).Scan(&row.ID, &row.Code, &row.Label, &row.DisplayOrder, &row.IsDefault, &row.IsSystem, &row.IsActive)
	if err != nil {
		return nil, apperr.Internal("create price type failed")
	}
	return &row, nil
}

//encore:api auth method=PATCH path=/api/v1/business/price-types/:id tag:owner
func UpdatePriceType(ctx context.Context, id string, req *UpdatePriceTypeRequest) (*PriceType, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can manage price types")
	}

	conn, err := tenantConn(ctx, user)
	if err != nil {
		return nil, err
	}
	defer closeTenantConn(conn)

	var isSystem bool
	if err := conn.QueryRowContext(ctx,
		`SELECT is_system FROM business_price_type WHERE id = $1::uuid AND deleted_at IS NULL`, id,
	).Scan(&isSystem); err == sql.ErrNoRows {
		return nil, apperr.NotFound("tipe harga tidak ditemukan")
	} else if err != nil {
		return nil, apperr.Internal("load price type failed")
	}

	sets := []string{"updated_at = now()"}
	args := []any{}
	n := 1
	add := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, val)
		n++
	}
	if req.Label != nil {
		label := strings.TrimSpace(*req.Label)
		if label == "" {
			return nil, apperr.BadRequest("label tidak boleh kosong")
		}
		add("label", label)
	}
	if req.DisplayOrder != nil {
		add("display_order", *req.DisplayOrder)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.IsDefault != nil && *req.IsDefault {
		_, _ = conn.ExecContext(ctx, `UPDATE business_price_type SET is_default = false WHERE deleted_at IS NULL AND id <> $1::uuid`, id)
		add("is_default", true)
	} else if req.IsDefault != nil && !*req.IsDefault && !isSystem {
		add("is_default", false)
	}
	if len(sets) == 1 {
		return nil, apperr.BadRequest("tidak ada field untuk diperbarui")
	}
	args = append(args, id)

	var row PriceType
	q := fmt.Sprintf(`
		UPDATE business_price_type SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id::text, code, label, display_order, is_default, is_system, is_active`,
		strings.Join(sets, ", "), n)
	err = conn.QueryRowContext(ctx, q, args...).Scan(
		&row.ID, &row.Code, &row.Label, &row.DisplayOrder, &row.IsDefault, &row.IsSystem, &row.IsActive,
	)
	if err == sql.ErrNoRows {
		return nil, apperr.NotFound("tipe harga tidak ditemukan")
	}
	if err != nil {
		return nil, apperr.Internal("update price type failed")
	}
	return &row, nil
}

//encore:api auth method=DELETE path=/api/v1/business/price-types/:id tag:owner
func DeletePriceType(ctx context.Context, id string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	if !user.CanPerformOwnerActions() {
		return apperr.Forbidden("only owner can manage price types")
	}

	conn, err := tenantConn(ctx, user)
	if err != nil {
		return apperr.Internal("database connection failed")
	}
	defer closeTenantConn(conn)

	var isSystem, isDefault bool
	err = conn.QueryRowContext(ctx, `
		SELECT is_system, is_default FROM business_price_type
		WHERE id = $1::uuid AND deleted_at IS NULL`, id,
	).Scan(&isSystem, &isDefault)
	if err == sql.ErrNoRows {
		return apperr.NotFound("tipe harga tidak ditemukan")
	}
	if err != nil {
		return apperr.Internal("load price type failed")
	}
	if isSystem {
		return apperr.BadRequest("tipe harga bawaan sistem tidak bisa dihapus")
	}
	if isDefault {
		return apperr.BadRequest("tipe harga default tidak bisa dihapus; set default ke tipe lain dulu")
	}

	_, err = conn.ExecContext(ctx, `UPDATE business_price_type SET deleted_at = now() WHERE id = $1::uuid`, id)
	if err != nil {
		return apperr.Internal("delete price type failed")
	}
	_, _ = conn.ExecContext(ctx, `UPDATE contact SET price_type_id = NULL WHERE price_type_id = $1::uuid`, id)
	return nil
}
