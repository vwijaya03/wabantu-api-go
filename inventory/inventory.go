// Package inventory implements the stock / HPP (inventory costing) module.
//
// PR-A1 scope: per-tenant setting (setup gate + default costing method),
// warehouses, and the schema/seed foundation. Stock movements, costing layers,
// purchasing, and order integration are added in later PRs (A2+).
package inventory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
)

func getUser() (*types.AuthUser, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || !u.HasEffectiveTenantContext() {
		return nil, appErrs.Unauthenticated("missing auth data")
	}
	return u, nil
}

func requireOwner(u *types.AuthUser) error {
	if !u.CanPerformOwnerActions() {
		return appErrs.Forbidden("owner access required")
	}
	return nil
}

// ---------- types ----------

type Warehouse struct {
	ID                 string    `json:"id"`
	Code               string    `json:"code"`
	Name               string    `json:"name"`
	ExternalLocationID *int      `json:"externalLocationId,omitempty"`
	IsDefault          bool      `json:"isDefault"`
	IsActive           bool      `json:"isActive"`
	Address            *string   `json:"address,omitempty"`
	Note               *string   `json:"note,omitempty"`
	DisplayOrder       int       `json:"displayOrder"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type InventorySetting struct {
	SetupCompleted       bool       `json:"setupCompleted"`
	SetupCompletedAt     *time.Time `json:"setupCompletedAt,omitempty"`
	DefaultCostingMethod string     `json:"defaultCostingMethod"`
	BlockNegativeStock   bool       `json:"blockNegativeStock"`
	WarehouseCount       int        `json:"warehouseCount"`
}

type ListWarehousesResponse struct {
	Warehouses []Warehouse `json:"warehouses"`
}

type WarehouseInput struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Address      *string `json:"address"`
	Note         *string `json:"note"`
	IsActive     *bool   `json:"isActive"`
	DisplayOrder *int    `json:"displayOrder"`
}

type UpdateSettingParams struct {
	DefaultCostingMethod *string `json:"defaultCostingMethod"`
	BlockNegativeStock   *bool   `json:"blockNegativeStock"`
}

// ---------- setting endpoints ----------

//encore:api auth method=GET path=/api/v1/inventory/setting
func GetSetting(ctx context.Context) (*InventorySetting, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	s, err := loadSetting(ctx, conn)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inv_warehouse WHERE deleted_at IS NULL`).Scan(&s.WarehouseCount); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return s, nil
}

//encore:api auth method=PATCH path=/api/v1/inventory/setting
func UpdateSetting(ctx context.Context, p *UpdateSettingParams) (*InventorySetting, error) {
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

	if _, err := loadSetting(ctx, conn); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if p.DefaultCostingMethod != nil {
		method, ok := normalizeCostingMethod(*p.DefaultCostingMethod)
		if !ok {
			return nil, appErrs.BadRequest("metode HPP harus salah satu: fifo, lifo, average")
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE inv_setting SET default_costing_method = $1, updated_by = $2, updated_at = now()`,
			method, nullUUID(u.AccountID)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if p.BlockNegativeStock != nil {
		if _, err := conn.ExecContext(ctx,
			`UPDATE inv_setting SET block_negative_stock = $1, updated_by = $2, updated_at = now()`,
			*p.BlockNegativeStock, nullUUID(u.AccountID)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	s, err := loadSetting(ctx, conn)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inv_warehouse WHERE deleted_at IS NULL`).Scan(&s.WarehouseCount); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return s, nil
}

//encore:api auth method=POST path=/api/v1/inventory/setup/complete
func CompleteSetup(ctx context.Context) (*InventorySetting, error) {
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

	if _, err := loadSetting(ctx, conn); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if _, err := conn.ExecContext(ctx, `
		UPDATE inv_setting
		SET setup_completed = true,
		    setup_completed_at = COALESCE(setup_completed_at, now()),
		    updated_by = $1,
		    updated_at = now()`, nullUUID(u.AccountID)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	s, err := loadSetting(ctx, conn)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inv_warehouse WHERE deleted_at IS NULL`).Scan(&s.WarehouseCount); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return s, nil
}

// loadSetting reads the singleton inv_setting row, creating it lazily if missing.
func loadSetting(ctx context.Context, conn *sql.Conn) (*InventorySetting, error) {
	s := &InventorySetting{}
	var completedAt sql.NullTime
	err := conn.QueryRowContext(ctx, `
		SELECT setup_completed, setup_completed_at, default_costing_method, block_negative_stock
		FROM inv_setting
		ORDER BY created_at
		LIMIT 1`).Scan(&s.SetupCompleted, &completedAt, &s.DefaultCostingMethod, &s.BlockNegativeStock)
	if errors.Is(err, sql.ErrNoRows) {
		if _, ierr := conn.ExecContext(ctx, `
			INSERT INTO inv_setting (setup_completed, default_costing_method, block_negative_stock)
			VALUES (false, 'average', true)`); ierr != nil {
			return nil, ierr
		}
		s.DefaultCostingMethod = CostingAverage
		s.BlockNegativeStock = true
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		t := completedAt.Time
		s.SetupCompletedAt = &t
	}
	return s, nil
}

// ---------- warehouse endpoints ----------

//encore:api auth method=GET path=/api/v1/inventory/warehouses
func ListWarehouses(ctx context.Context) (*ListWarehousesResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT id, code, name, external_location_id, is_default, is_active,
		       address, note, display_order, created_at, updated_at
		FROM inv_warehouse
		WHERE deleted_at IS NULL
		ORDER BY is_default DESC, display_order, name`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	out := make([]Warehouse, 0)
	for rows.Next() {
		w, err := scanWarehouse(rows.Scan)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, w)
	}
	return &ListWarehousesResponse{Warehouses: out}, nil
}

//encore:api auth method=POST path=/api/v1/inventory/warehouses
func CreateWarehouse(ctx context.Context, p *WarehouseInput) (*Warehouse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, appErrs.BadRequest("nama gudang wajib diisi")
	}
	code := strings.TrimSpace(p.Code)
	if code == "" {
		code = normalizeWarehouseCode(name)
	} else {
		code = normalizeWarehouseCode(code)
	}
	isActive := true
	if p.IsActive != nil {
		isActive = *p.IsActive
	}
	displayOrder := 0
	if p.DisplayOrder != nil {
		displayOrder = *p.DisplayOrder
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var dup bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM inv_warehouse WHERE code = $1 AND deleted_at IS NULL)`,
		code).Scan(&dup); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if dup {
		return nil, appErrs.BadRequest("kode gudang sudah dipakai")
	}

	row := conn.QueryRowContext(ctx, `
		INSERT INTO inv_warehouse (code, name, is_active, address, note, display_order, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, code, name, external_location_id, is_default, is_active,
		          address, note, display_order, created_at, updated_at`,
		code, name, isActive, trimPtr(p.Address), trimPtr(p.Note), displayOrder, nullUUID(u.AccountID))
	w, err := scanWarehouse(row.Scan)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &w, nil
}

//encore:api auth method=PATCH path=/api/v1/inventory/warehouses/:id
func UpdateWarehouse(ctx context.Context, id string, p *WarehouseInput) (*Warehouse, error) {
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

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, appErrs.BadRequest("nama gudang wajib diisi")
	}
	isActive := true
	if p.IsActive != nil {
		isActive = *p.IsActive
	}

	row := conn.QueryRowContext(ctx, `
		UPDATE inv_warehouse
		SET name = $2,
		    address = $3,
		    note = $4,
		    is_active = $5,
		    display_order = COALESCE($6, display_order),
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, code, name, external_location_id, is_default, is_active,
		          address, note, display_order, created_at, updated_at`,
		id, name, trimPtr(p.Address), trimPtr(p.Note), isActive, p.DisplayOrder)
	w, err := scanWarehouse(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("gudang tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &w, nil
}

//encore:api auth method=DELETE path=/api/v1/inventory/warehouses/:id
func DeleteWarehouse(ctx context.Context, id string) error {
	u, err := getUser()
	if err != nil {
		return err
	}
	if err := requireOwner(u); err != nil {
		return err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var isDefault bool
	err = conn.QueryRowContext(ctx,
		`SELECT is_default FROM inv_warehouse WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return appErrs.NotFound("gudang tidak ditemukan")
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if isDefault {
		return appErrs.BadRequest("gudang default tidak bisa dihapus")
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE inv_warehouse SET deleted_at = now(), deleted_by = $2, updated_at = now() WHERE id = $1`,
		id, nullUUID(u.AccountID)); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}

// ---------- helpers ----------

func scanWarehouse(scan func(dest ...any) error) (Warehouse, error) {
	var w Warehouse
	var extLoc sql.NullInt64
	var address, note sql.NullString
	if err := scan(
		&w.ID, &w.Code, &w.Name, &extLoc, &w.IsDefault, &w.IsActive,
		&address, &note, &w.DisplayOrder, &w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return w, err
	}
	if extLoc.Valid {
		v := int(extLoc.Int64)
		w.ExternalLocationID = &v
	}
	if address.Valid && strings.TrimSpace(address.String) != "" {
		w.Address = &address.String
	}
	if note.Valid && strings.TrimSpace(note.String) != "" {
		w.Note = &note.String
	}
	return w, nil
}

func trimPtr(s *string) any {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return t
}

func nullUUID(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
