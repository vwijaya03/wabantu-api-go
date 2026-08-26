// Package inventory implements the stock / HPP (inventory costing) module.
//
// PR-A1 scope: per-tenant setting (setup gate + default costing method),
// warehouses, and the schema/seed foundation. Stock movements, costing layers,
// purchasing, and order integration are added in later PRs (A2+).
package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

func getUser() (*types.AuthUser, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil {
		return nil, appErrs.Unauthenticated("missing auth data")
	}
	if !u.HasEffectiveTenantContext() {
		return nil, appErrs.Forbidden("tenant context required — pantau tenant dari konsol admin")
	}
	if err := u.RequireModule("inventory"); err != nil {
		return nil, err
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
	CustomerLabel      *string   `json:"customerLabel,omitempty"`
	ExternalLocationID *int      `json:"externalLocationId,omitempty"`
	IsDefault          bool      `json:"isDefault"`
	IsActive           bool      `json:"isActive"`
	Address            *string   `json:"address,omitempty"`
	Note               *string   `json:"note,omitempty"`
	DisplayOrder       int       `json:"displayOrder"`
	IsDeleted          bool      `json:"isDeleted,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type InventorySetting struct {
	SetupCompleted            bool       `json:"setupCompleted"`
	SetupCompletedAt          *time.Time `json:"setupCompletedAt,omitempty"`
	DefaultCostingMethod      string     `json:"defaultCostingMethod"`
	BlockNegativeStock        bool       `json:"blockNegativeStock"`
	PurchasePostsExpense      bool       `json:"purchasePostsExpense"`
	WarehouseCount            int        `json:"warehouseCount"`
	WizardInterviewCompleted  bool       `json:"wizardInterviewCompleted"`
}

type ListWarehousesParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
	All      bool   `query:"all"` // true = semua baris (dropdown); tanpa page = default all
}

type ListWarehousesResponse struct {
	Warehouses []Warehouse `json:"warehouses"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
}

type WarehouseInput struct {
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	CustomerLabel *string `json:"customerLabel"`
	Address       *string `json:"address"`
	Note         *string `json:"note"`
	IsActive     *bool   `json:"isActive"`
	DisplayOrder *int    `json:"displayOrder"`
}

type UpdateSettingParams struct {
	DefaultCostingMethod *string `json:"defaultCostingMethod"`
	BlockNegativeStock   *bool   `json:"blockNegativeStock"`
	PurchasePostsExpense *bool   `json:"purchasePostsExpense"`
}

// ---------- setting endpoints ----------

//encore:api auth method=GET path=/api/v1/inventory/setting
func GetSetting(ctx context.Context) (*InventorySetting, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	s, err := loadSetting(ctx, sch, pool)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return enrichSetting(ctx, sch, pool, s)
}

func enrichSetting(ctx context.Context, sch appdb.SchemaSQL, q querier, s *InventorySetting) (*InventorySetting, error) {
	if err := qrow(ctx, sch, q,
		`SELECT COUNT(*) FROM inv_warehouse WHERE deleted_at IS NULL`).Scan(&s.WarehouseCount); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if answers, rec, werr := loadWizardSnapshot(ctx, sch, q); werr == nil {
		s.WizardInterviewCompleted = wizardInterviewCompleted(answers, rec)
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
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	if _, err := loadSetting(ctx, sch, pool); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if p.DefaultCostingMethod != nil {
		method, ok := normalizeCostingMethod(*p.DefaultCostingMethod)
		if !ok {
			return nil, appErrs.BadRequest("metode HPP harus salah satu: fifo, lifo, average")
		}
		if _, err := qexec(ctx, sch, pool,
			`UPDATE inv_setting SET default_costing_method = $1, updated_by = $2, updated_at = now()`,
			method, nullUUID(u.AccountID)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if p.BlockNegativeStock != nil {
		if _, err := qexec(ctx, sch, pool,
			`UPDATE inv_setting SET block_negative_stock = $1, updated_by = $2, updated_at = now()`,
			*p.BlockNegativeStock, nullUUID(u.AccountID)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if p.PurchasePostsExpense != nil {
		if _, err := qexec(ctx, sch, pool,
			`UPDATE inv_setting SET purchase_posts_expense = $1, updated_by = $2, updated_at = now()`,
			*p.PurchasePostsExpense, nullUUID(u.AccountID)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	s, err := loadSetting(ctx, sch, pool)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return enrichSetting(ctx, sch, pool, s)
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
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	s, err := loadSetting(ctx, sch, pool)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if s.SetupCompleted {
		return s, nil
	}
	answers, rec, err := loadWizardSnapshot(ctx, sch, pool)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := validateInventorySetupActivation(answers, rec); err != nil {
		return nil, err
	}
	if _, err := qexec(ctx, sch, pool, `
		UPDATE inv_setting
		SET setup_completed = true,
		    setup_completed_at = COALESCE(setup_completed_at, now()),
		    updated_by = $1,
		    updated_at = now()`, nullUUID(u.AccountID)); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	s, err = loadSetting(ctx, sch, pool)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return enrichSetting(ctx, sch, pool, s)
}

// loadSetting reads the singleton inv_setting row, creating it lazily if missing.
func loadSetting(ctx context.Context, sch appdb.SchemaSQL, q querier) (*InventorySetting, error) {
	if err := ensureInventoryModuleSchema(ctx, sch.Schema); err != nil {
		return nil, err
	}
	s := &InventorySetting{}
	var completedAt sql.NullTime
	err := qrow(ctx, sch, q, `
		SELECT setup_completed, setup_completed_at, default_costing_method, block_negative_stock, purchase_posts_expense
		FROM inv_setting
		ORDER BY created_at
		LIMIT 1`).Scan(&s.SetupCompleted, &completedAt, &s.DefaultCostingMethod, &s.BlockNegativeStock, &s.PurchasePostsExpense)
	if errors.Is(err, sql.ErrNoRows) {
		if _, ierr := qexec(ctx, sch, q, `
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
func ListWarehouses(ctx context.Context, p *ListWarehousesParams) (*ListWarehousesResponse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	if p == nil {
		p = &ListWarehousesParams{}
	}
	paginate := !p.All && (p.Page > 0 || p.PageSize > 0)
	page, pageSize := p.Page, p.PageSize
	if paginate {
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 25
		}
	}

	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if searchQ := strings.TrimSpace(p.Q); searchQ != "" {
		where += fmt.Sprintf(` AND (
			w.name ILIKE $%d OR w.code ILIKE $%d
			OR COALESCE(w.address,'') ILIKE $%d OR COALESCE(w.note,'') ILIKE $%d
		)`, idx, idx, idx, idx)
		args = append(args, "%"+searchQ+"%")
		idx++
	}

	var total int
	if err := qrow(ctx, sch, pool,
		fmt.Sprintf(`SELECT COUNT(*) FROM inv_warehouse w %s`, where), args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	query := fmt.Sprintf(`
		SELECT w.id, w.code, w.name, w.customer_label, w.external_location_id, w.is_default, w.is_active,
		       w.address, w.note, w.display_order, w.created_at, w.updated_at,
		       w.deleted_at IS NOT NULL AS is_deleted
		FROM inv_warehouse w
		%s
		ORDER BY w.is_default DESC, w.display_order, w.name`, where)
	if paginate {
		args = append(args, pageSize, (page-1)*pageSize)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", idx, idx+1)
	}

	rows, err := qquery(ctx, sch, pool, query, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	out := make([]Warehouse, 0)
	for rows.Next() {
		var w Warehouse
		var extLoc sql.NullInt64
		var customerLabel, address, note sql.NullString
		if err := rows.Scan(
			&w.ID, &w.Code, &w.Name, &customerLabel, &extLoc, &w.IsDefault, &w.IsActive,
			&address, &note, &w.DisplayOrder, &w.CreatedAt, &w.UpdatedAt, &w.IsDeleted,
		); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if extLoc.Valid {
			v := int(extLoc.Int64)
			w.ExternalLocationID = &v
		}
		if customerLabel.Valid && strings.TrimSpace(customerLabel.String) != "" {
			v := customerLabel.String
			w.CustomerLabel = &v
		}
		if address.Valid && strings.TrimSpace(address.String) != "" {
			w.Address = &address.String
		}
		if note.Valid && strings.TrimSpace(note.String) != "" {
			w.Note = &note.String
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	resp := &ListWarehousesResponse{
		Warehouses: out,
		Total:      total,
	}
	if paginate {
		resp.Page = page
		resp.PageSize = pageSize
	} else {
		resp.Page = 1
		resp.PageSize = total
	}
	return resp, nil
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

	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	var dup bool
	if err := qrow(ctx, sch, pool,
		`SELECT EXISTS(SELECT 1 FROM inv_warehouse WHERE code = $1 AND deleted_at IS NULL)`,
		code).Scan(&dup); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if dup {
		return nil, appErrs.BadRequest("kode gudang sudah dipakai")
	}

	row := qrow(ctx, sch, pool, `
		INSERT INTO inv_warehouse (code, name, customer_label, is_active, address, note, display_order, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, code, name, customer_label, external_location_id, is_default, is_active,
		          address, note, display_order, created_at, updated_at`,
		code, name, trimPtr(p.CustomerLabel), isActive, trimPtr(p.Address), trimPtr(p.Note), displayOrder, nullUUID(u.AccountID))
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
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, appErrs.BadRequest("nama gudang wajib diisi")
	}
	isActive := true
	if p.IsActive != nil {
		isActive = *p.IsActive
	}

	row := qrow(ctx, sch, pool, `
		UPDATE inv_warehouse
		SET name = $2,
		    customer_label = $3,
		    address = $4,
		    note = $5,
		    is_active = $6,
		    display_order = COALESCE($7, display_order),
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, code, name, customer_label, external_location_id, is_default, is_active,
		          address, note, display_order, created_at, updated_at`,
		id, name, trimPtr(p.CustomerLabel), trimPtr(p.Address), trimPtr(p.Note), isActive, p.DisplayOrder)
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
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return err
	}
	pool := tenantDB()

	var isDefault bool
	err = qrow(ctx, sch, pool,
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
	usage, err := loadWarehouseUsage(ctx, sch, pool, id)
	if err != nil {
		return err
	}
	if usage.inUse() {
		return appErrs.BadRequest(usage.message() + " — nonaktifkan saja atau pastikan tidak ada referensi")
	}
	if _, err := qexec(ctx, sch, pool, `DELETE FROM inv_warehouse WHERE id = $1`, id); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}

//encore:api auth method=POST path=/api/v1/inventory/warehouses/:id/reactivate
func ReactivateWarehouse(ctx context.Context, id string) (*Warehouse, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pool := tenantDB()

	row := qrow(ctx, sch, pool, `
		UPDATE inv_warehouse
		SET deleted_at = NULL, deleted_by = NULL, is_active = true, updated_at = now()
		WHERE id = $1
		RETURNING id, code, name, customer_label, external_location_id, is_default, is_active,
		          address, note, display_order, created_at, updated_at`, id)
	w, err := scanWarehouse(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("gudang tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &w, nil
}

// ---------- helpers ----------

func scanWarehouse(scan func(dest ...any) error) (Warehouse, error) {
	var w Warehouse
	var extLoc sql.NullInt64
	var customerLabel, address, note sql.NullString
	if err := scan(
		&w.ID, &w.Code, &w.Name, &customerLabel, &extLoc, &w.IsDefault, &w.IsActive,
		&address, &note, &w.DisplayOrder, &w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return w, err
	}
	if extLoc.Valid {
		v := int(extLoc.Int64)
		w.ExternalLocationID = &v
	}
	if customerLabel.Valid && strings.TrimSpace(customerLabel.String) != "" {
		v := customerLabel.String
		w.CustomerLabel = &v
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

func nullFloat(v float64) any {
	return v
}
