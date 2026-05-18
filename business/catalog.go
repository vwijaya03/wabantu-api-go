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
	ID           string     `json:"id"`
	ExternalCode string     `json:"externalCode"`
	Name         string     `json:"name"`
	Description  *string    `json:"description,omitempty"`
	SellPrice    *float64   `json:"sellPrice,omitempty"`
	SellUnit     *string    `json:"sellUnit,omitempty"`
	IsActive     bool       `json:"isActive"`
	Barcode      *string    `json:"barcode,omitempty"`
	Source       string     `json:"source"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type ListCatalogResponse struct {
	Items []CatalogItem `json:"items"`
	Total int           `json:"total"`
}

type CreateCatalogRequest struct {
	ExternalCode string   `json:"externalCode"`
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	SellPrice    *float64 `json:"sellPrice,omitempty"`
	SellUnit     *string  `json:"sellUnit,omitempty"`
	IsActive     *bool    `json:"isActive,omitempty"`
	Barcode      *string  `json:"barcode,omitempty"`
}

type UpdateCatalogRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	SellPrice   *float64 `json:"sellPrice,omitempty"`
	SellUnit    *string  `json:"sellUnit,omitempty"`
	IsActive    *bool    `json:"isActive,omitempty"`
	Barcode     *string  `json:"barcode,omitempty"`
}

//encore:api auth method=GET path=/api/v1/business/catalog
func ListCatalog(ctx context.Context) (*ListCatalogResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT id, external_code, name, description, sell_price, sell_unit,
		       is_active, barcode, source, created_at, updated_at
		FROM business_catalog_item
		WHERE deleted_at IS NULL
		ORDER BY name ASC`)
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
	return &ListCatalogResponse{Items: items, Total: len(items)}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/business/catalog tag:owner
func CreateCatalog(ctx context.Context, req *CreateCatalogRequest) (*CatalogItem, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if user.Role != "owner" {
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

	row := conn.QueryRowContext(ctx, `
		INSERT INTO business_catalog_item
			(external_code, name, description, sell_price, sell_unit, is_active, barcode, source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'manual')
		RETURNING id, external_code, name, description, sell_price, sell_unit,
		          is_active, barcode, source, created_at, updated_at`,
		code, name, req.Description, req.SellPrice, req.SellUnit, active, req.Barcode)
	item, err := scanCatalog(row.Scan)
	if err != nil {
		rlog.Error("create catalog item failed", "err", err)
		return nil, apperr.Internal("create catalog item failed")
	}
	return &item, nil
}

//encore:api auth method=PATCH path=/api/v1/business/catalog/:id tag:owner
func UpdateCatalog(ctx context.Context, id string, req *UpdateCatalogRequest) (*CatalogItem, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if user.Role != "owner" {
		return nil, apperr.Forbidden("only owner can manage catalog")
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

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
	if len(sets) == 0 {
		return nil, apperr.BadRequest("no fields to update")
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	q := fmt.Sprintf(`
		UPDATE business_catalog_item SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, external_code, name, description, sell_price, sell_unit,
		          is_active, barcode, source, created_at, updated_at`,
		strings.Join(sets, ", "), n)
	item, err := scanCatalog(conn.QueryRowContext(ctx, q, args...).Scan)
	if err == sql.ErrNoRows {
		return nil, apperr.NotFound("catalog item not found")
	}
	if err != nil {
		return nil, apperr.Internal("update catalog item failed")
	}
	return &item, nil
}

//encore:api auth method=DELETE path=/api/v1/business/catalog/:id tag:owner
func DeleteCatalog(ctx context.Context, id string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	if user.Role != "owner" {
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
