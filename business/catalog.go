package business

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"

	apperr "encore.app/wabantu/shared/errs"
)

// CatalogItem is a product/service row in business_catalog_item.
type CatalogItem struct {
	ID                 string             `json:"id"`
	ExternalCode       string             `json:"externalCode"`
	Name               string             `json:"name"`
	Description        *string            `json:"description,omitempty"`
	SellPrice          *float64           `json:"sellPrice,omitempty"`
	EffectiveSellPrice *float64           `json:"effectiveSellPrice,omitempty"`
	Prices             []CatalogItemPrice `json:"prices,omitempty"`
	SellUnit           *string            `json:"sellUnit,omitempty"`
	IsActive           bool               `json:"isActive"`
	Barcode            *string            `json:"barcode,omitempty"`
	Source             string             `json:"source"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

type ListCatalogResponse struct {
	Items    []CatalogItem `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

type ListCatalogParams struct {
	Q          string `query:"q"`
	Page       int    `query:"page"`
	PageSize   int    `query:"pageSize"`
	ActiveOnly string `query:"activeOnly"`
	ContactID  string `query:"contactId"`
}

type CreateCatalogRequest struct {
	ExternalCode string             `json:"externalCode"`
	Name         string             `json:"name"`
	Description  *string            `json:"description,omitempty"`
	SellPrice    *float64           `json:"sellPrice,omitempty"`
	Prices       []CatalogItemPrice `json:"prices,omitempty"`
	SellUnit     *string            `json:"sellUnit,omitempty"`
	IsActive     *bool              `json:"isActive,omitempty"`
	Barcode      *string            `json:"barcode,omitempty"`
}

type UpdateCatalogRequest struct {
	Name        *string            `json:"name,omitempty"`
	Description *string            `json:"description,omitempty"`
	SellPrice   *float64           `json:"sellPrice,omitempty"`
	Prices      []CatalogItemPrice `json:"prices,omitempty"`
	SellUnit    *string            `json:"sellUnit,omitempty"`
	IsActive    *bool              `json:"isActive,omitempty"`
	Barcode     *string            `json:"barcode,omitempty"`
}

//encore:api auth method=GET path=/api/v1/business/catalog
func ListCatalog(ctx context.Context, p *ListCatalogParams) (*ListCatalogResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	normalizeCatalogListParams(p)
	conn, err := tenantConn(ctx, user)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := ensurePricingSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare catalog pricing failed")
	}
	if err := seedDefaultPriceTypes(ctx, conn); err != nil {
		return nil, apperr.Internal("seed price types failed")
	}

	conditions := []string{"deleted_at IS NULL"}
	args := []any{}
	if strings.EqualFold(strings.TrimSpace(p.ActiveOnly), "true") {
		conditions = append(conditions, "is_active = true")
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		args = append(args, "%"+q+"%")
		conditions = append(conditions, fmt.Sprintf(`(
			external_code ILIKE $%[1]d OR
			name ILIKE $%[1]d OR
			COALESCE(description, '') ILIKE $%[1]d OR
			COALESCE(barcode, '') ILIKE $%[1]d
		)`, len(args)))
	}
	where := strings.Join(conditions, " AND ")

	var total int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM business_catalog_item
		WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, apperr.Internal("count catalog failed")
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, p.PageSize, (p.Page-1)*p.PageSize)
	limitParam := len(queryArgs) - 1
	offsetParam := len(queryArgs)

	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, external_code, name, description, sell_price, sell_unit,
		       is_active, barcode, source, created_at, updated_at
		FROM business_catalog_item
		WHERE %s
		ORDER BY name ASC, external_code ASC
		LIMIT $%d OFFSET $%d`, where, limitParam, offsetParam), queryArgs...)
	if err != nil {
		return nil, apperr.Internal("list catalog failed")
	}
	defer rows.Close()

	items := make([]CatalogItem, 0)
	for rows.Next() {
		item, err := scanCatalog(rows.Scan)
		if err != nil {
			return nil, apperr.Internal("scan catalog row failed")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Internal("read catalog rows failed")
	}
	if err := attachCatalogItemPricesBatch(ctx, conn, items); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ContactID) != "" {
		if err := attachEffectiveSellPrices(ctx, conn, items, strings.TrimSpace(p.ContactID)); err != nil {
			return nil, err
		}
	}
	return &ListCatalogResponse{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

//encore:api auth method=POST path=/api/v1/business/catalog tag:owner
func CreateCatalog(ctx context.Context, req *CreateCatalogRequest) (*CatalogItem, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can manage catalog")
	}
	code := strings.TrimSpace(req.ExternalCode)
	name := strings.TrimSpace(req.Name)
	if code == "" || name == "" {
		return nil, apperr.BadRequest("externalCode and name are required")
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	if err := ensurePricingSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare catalog pricing failed")
	}

	item, err := insertOrRestoreCatalogItem(ctx, conn, code, name, req.Description, req.SellPrice, req.SellUnit, active, req.Barcode)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperr.BadRequest(fmt.Sprintf("SKU/kode %q sudah dipakai produk lain", code))
		}
		rlog.Error("create catalog item failed", "err", err)
		return nil, apperr.Internal("create catalog item failed")
	}
	prices := req.Prices
	if len(prices) == 0 && req.SellPrice != nil {
		if ptID, err := resolveDefaultPriceTypeID(ctx, conn); err == nil {
			prices = []CatalogItemPrice{{PriceTypeID: ptID, Price: *req.SellPrice}}
		}
	}
	if len(prices) > 0 {
		if err := upsertCatalogItemPrices(ctx, conn, item.ID, prices); err != nil {
			return nil, err
		}
		item.Prices, _ = loadCatalogItemPrices(ctx, conn, item.ID)
	}
	return &item, nil
}

//encore:api auth method=PATCH path=/api/v1/business/catalog/:id tag:owner
func UpdateCatalog(ctx context.Context, id string, req *UpdateCatalogRequest) (*CatalogItem, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can manage catalog")
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	if err := ensurePricingSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare catalog pricing failed")
	}

	sets := []string{}
	args := []any{}
	n := 1
	add := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, val)
		n++
	}
	if req.Name != nil {
		add("name", strings.TrimSpace(*req.Name))
	}
	if req.Description != nil {
		add("description", req.Description)
	}
	if req.SellPrice != nil {
		add("sell_price", req.SellPrice)
	}
	if req.SellUnit != nil {
		add("sell_unit", req.SellUnit)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.Barcode != nil {
		add("barcode", req.Barcode)
	}
	hasPriceUpdate := len(req.Prices) > 0 || req.SellPrice != nil
	if len(sets) == 0 && !hasPriceUpdate {
		return nil, apperr.BadRequest("no fields to update")
	}

	var item CatalogItem
	if len(sets) > 0 {
		sets = append(sets, "updated_at = NOW()")
		args = append(args, id)
		q := fmt.Sprintf(`
			UPDATE business_catalog_item SET %s
			WHERE id = $%d AND deleted_at IS NULL
			RETURNING id, external_code, name, description, sell_price, sell_unit,
			          is_active, barcode, source, created_at, updated_at`,
			strings.Join(sets, ", "), n)
		item, err = scanCatalog(conn.QueryRowContext(ctx, q, args...).Scan)
		if err == sql.ErrNoRows {
			return nil, apperr.NotFound("catalog item not found")
		}
		if err != nil {
			return nil, apperr.Internal("update catalog item failed")
		}
	} else {
		row := conn.QueryRowContext(ctx, `
			SELECT id, external_code, name, description, sell_price, sell_unit,
			       is_active, barcode, source, created_at, updated_at
			FROM business_catalog_item
			WHERE id = $1 AND deleted_at IS NULL`, id)
		item, err = scanCatalog(row.Scan)
		if err == sql.ErrNoRows {
			return nil, apperr.NotFound("catalog item not found")
		}
		if err != nil {
			return nil, apperr.Internal("load catalog item failed")
		}
	}
	if len(req.Prices) > 0 {
		if err := upsertCatalogItemPrices(ctx, conn, item.ID, req.Prices); err != nil {
			return nil, err
		}
	} else if req.SellPrice != nil {
		if ptID, err := resolveDefaultPriceTypeID(ctx, conn); err == nil {
			_ = upsertCatalogItemPrices(ctx, conn, item.ID, []CatalogItemPrice{{PriceTypeID: ptID, Price: *req.SellPrice}})
		}
	}
	item.Prices, _ = loadCatalogItemPrices(ctx, conn, item.ID)
	return &item, nil
}

//encore:api auth method=DELETE path=/api/v1/business/catalog/:id tag:owner
func DeleteCatalog(ctx context.Context, id string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	if !user.CanPerformOwnerActions() {
		return apperr.Forbidden("only owner can manage catalog")
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}
	defer conn.Close()

	uid, _ := auth.UserID()
	res, err := conn.ExecContext(ctx, `
		UPDATE business_catalog_item
		SET deleted_at = NOW(), deleted_by = $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL`, string(uid), id)
	if err != nil {
		return apperr.Internal("delete catalog item failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperr.NotFound("catalog item not found")
	}
	return nil
}

func scanCatalog(scan func(dest ...any) error) (CatalogItem, error) {
	var item CatalogItem
	var desc, unit, barcode sql.NullString
	var price sql.NullFloat64
	err := scan(
		&item.ID, &item.ExternalCode, &item.Name, &desc, &price, &unit,
		&item.IsActive, &barcode, &item.Source, &item.CreatedAt, &item.UpdatedAt,
	)
	if desc.Valid {
		item.Description = &desc.String
	}
	if unit.Valid {
		item.SellUnit = &unit.String
	}
	if barcode.Valid {
		item.Barcode = &barcode.String
	}
	if price.Valid {
		p := price.Float64
		item.SellPrice = &p
	}
	return item, err
}

func insertOrRestoreCatalogItem(
	ctx context.Context,
	conn *sql.Conn,
	code, name string,
	description *string,
	sellPrice *float64,
	sellUnit *string,
	active bool,
	barcode *string,
) (CatalogItem, error) {
	restore := conn.QueryRowContext(ctx, `
		UPDATE business_catalog_item
		SET deleted_at = NULL, deleted_by = NULL,
		    name = $2, description = $3, sell_price = $4, sell_unit = $5,
		    is_active = $6, barcode = $7, updated_at = NOW()
		WHERE source = 'manual' AND external_code = $1 AND deleted_at IS NOT NULL
		RETURNING id, external_code, name, description, sell_price, sell_unit,
		          is_active, barcode, source, created_at, updated_at`,
		code, name, description, sellPrice, sellUnit, active, barcode)
	item, err := scanCatalog(restore.Scan)
	if err == nil {
		return item, nil
	}
	if err != sql.ErrNoRows {
		return CatalogItem{}, err
	}

	insert := conn.QueryRowContext(ctx, `
		INSERT INTO business_catalog_item
			(external_code, name, description, sell_price, sell_unit, is_active, barcode, source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'manual')
		RETURNING id, external_code, name, description, sell_price, sell_unit,
		          is_active, barcode, source, created_at, updated_at`,
		code, name, description, sellPrice, sellUnit, active, barcode)
	return scanCatalog(insert.Scan)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}

func normalizeCatalogListParams(p *ListCatalogParams) {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 20
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}
}

// ParsePrice converts common Indonesian price strings to float64.
func ParsePrice(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "Rp", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
