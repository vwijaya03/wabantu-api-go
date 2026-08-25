package finance

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
)

var txnTypeCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

type TransactionType struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Label         string `json:"label"`
	Flow          string `json:"flow"`          // income | expense | transfer | adjustment
	CategoryKind  string `json:"categoryKind"`  // income | expense | investment | any
	ShowInQuick   bool   `json:"showInQuick"`
	DisplayOrder  int    `json:"displayOrder"`
	IsSystem      bool   `json:"isSystem"`
	OwnerOnly     bool   `json:"ownerOnly"`
	IsActive      bool   `json:"isActive"`
}

type ListTransactionTypesParams struct {
	Q          string `query:"q"`
	Page       int    `query:"page"`
	PageSize   int    `query:"pageSize"`
	ActiveOnly bool   `query:"activeOnly"`
}

type ListTransactionTypesResponse struct {
	Items []TransactionType `json:"items"`
	Total int               `json:"total"`
}

type CreateTransactionTypeParams struct {
	Code         string `json:"code"`
	Label        string `json:"label"`
	Flow         string `json:"flow"`
	CategoryKind string `json:"categoryKind"`
	ShowInQuick  bool   `json:"showInQuick"`
	DisplayOrder int    `json:"displayOrder"`
}

type UpdateTransactionTypeParams struct {
	Label        *string `json:"label,omitempty"`
	Flow         *string `json:"flow,omitempty"`
	CategoryKind *string `json:"categoryKind,omitempty"`
	ShowInQuick  *bool   `json:"showInQuick,omitempty"`
	DisplayOrder *int    `json:"displayOrder,omitempty"`
	IsActive     *bool   `json:"isActive,omitempty"`
}

var validTxnFlows = map[string]bool{
	"income": true, "expense": true, "transfer": true, "adjustment": true,
}

var validCategoryKinds = map[string]bool{
	"income": true, "expense": true, "investment": true, "any": true,
}

//encore:api auth method=GET path=/api/v1/finance/transaction-types
func ListTransactionTypes(ctx context.Context, p *ListTransactionTypesParams) (*ListTransactionTypesResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil {
		p = &ListTransactionTypesParams{}
	}
	page := p.Page
	if page < 1 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	where := []string{"deleted_at IS NULL"}
	args := []any{}
	i := 1
	if p.ActiveOnly {
		where = append(where, "is_active=true")
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		where = append(where, fmt.Sprintf("(label ILIKE $%d OR code ILIKE $%d)", i, i))
		args = append(args, "%"+q+"%")
		i++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := qrow(ctx, sch, q,
		fmt.Sprintf(`SELECT COUNT(*) FROM fin_transaction_type WHERE %s`, whereSQL), args...,
	).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	rows, err := qquery(ctx, sch, q, fmt.Sprintf(`
		SELECT id, code, label, flow, category_kind, show_in_quick, display_order,
		       is_system, owner_only, is_active
		FROM fin_transaction_type
		WHERE %s
		ORDER BY display_order, label
		LIMIT $%d OFFSET $%d`, whereSQL, i, i+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	items, err := scanTransactionTypes(rows)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []TransactionType{}
	}
	return &ListTransactionTypesResponse{Items: items, Total: total}, nil
}

//encore:api auth method=POST path=/api/v1/finance/transaction-types tag:owner
func CreateTransactionType(ctx context.Context, p *CreateTransactionTypeParams) (*TransactionType, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	code := strings.ToLower(strings.TrimSpace(p.Code))
	if err := validateTxnTypeInput(code, p.Label, p.Flow, p.CategoryKind); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	var exists bool
	qrow(ctx, sch, q,
		`SELECT EXISTS(SELECT 1 FROM fin_transaction_type WHERE code=$1 AND deleted_at IS NULL)`, code,
	).Scan(&exists)
	if exists {
		return nil, appErrs.BadRequest("kode jenis transaksi sudah dipakai")
	}

	var id string
	err = qrow(ctx, sch, q, `
		INSERT INTO fin_transaction_type
		 (code, label, flow, category_kind, show_in_quick, display_order, is_system, owner_only, is_active)
		 VALUES ($1,$2,$3,$4,$5,$6,false,$7,true)
		 RETURNING id`,
		code, strings.TrimSpace(p.Label), p.Flow, p.CategoryKind, p.ShowInQuick, p.DisplayOrder,
		p.Flow == "adjustment",
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return loadTransactionTypeByID(ctx, sch, q, id)
}

//encore:api auth method=PUT path=/api/v1/finance/transaction-types/:id tag:owner
func UpdateTransactionType(ctx context.Context, id string, p *UpdateTransactionTypeParams) (*TransactionType, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	var isSystem bool
	if err := qrow(ctx, sch, q,
		`SELECT is_system FROM fin_transaction_type WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&isSystem); err != nil {
		return nil, appErrs.NotFound("jenis transaksi tidak ditemukan")
	}

	if p.Label != nil {
		if strings.TrimSpace(*p.Label) == "" {
			return nil, appErrs.BadRequest("label tidak boleh kosong")
		}
		qexec(ctx, sch, q, `UPDATE fin_transaction_type SET label=$1 WHERE id=$2`, strings.TrimSpace(*p.Label), id)
	}
	if !isSystem {
		if p.Flow != nil {
			if !validTxnFlows[*p.Flow] {
				return nil, appErrs.BadRequest("alur (flow) tidak valid")
			}
			qexec(ctx, sch, q, `UPDATE fin_transaction_type SET flow=$1 WHERE id=$2`, *p.Flow, id)
		}
		if p.CategoryKind != nil {
			if !validCategoryKinds[*p.CategoryKind] {
				return nil, appErrs.BadRequest("jenis kategori tidak valid")
			}
			qexec(ctx, sch, q, `UPDATE fin_transaction_type SET category_kind=$1 WHERE id=$2`, *p.CategoryKind, id)
		}
	}
	if p.ShowInQuick != nil {
		qexec(ctx, sch, q, `UPDATE fin_transaction_type SET show_in_quick=$1 WHERE id=$2`, *p.ShowInQuick, id)
	}
	if p.DisplayOrder != nil {
		qexec(ctx, sch, q, `UPDATE fin_transaction_type SET display_order=$1 WHERE id=$2`, *p.DisplayOrder, id)
	}
	if p.IsActive != nil {
		qexec(ctx, sch, q, `UPDATE fin_transaction_type SET is_active=$1 WHERE id=$2`, *p.IsActive, id)
	}
	return loadTransactionTypeByID(ctx, sch, q, id)
}

//encore:api auth method=DELETE path=/api/v1/finance/transaction-types/:id tag:owner
func DeleteTransactionType(ctx context.Context, id string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	var isSystem bool
	var code string
	if err := qrow(ctx, sch, q,
		`SELECT is_system, code FROM fin_transaction_type WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&isSystem, &code); err != nil {
		return nil, appErrs.NotFound("jenis transaksi tidak ditemukan")
	}
	if isSystem {
		return nil, appErrs.BadRequest("jenis bawaan sistem tidak bisa dihapus")
	}
	var inUse bool
	qrow(ctx, sch, q,
		`SELECT EXISTS(SELECT 1 FROM fin_transaction WHERE type=$1 AND deleted_at IS NULL LIMIT 1)`, code,
	).Scan(&inUse)
	if inUse {
		return nil, appErrs.BadRequest("jenis masih dipakai transaksi — nonaktifkan saja")
	}
	qexec(ctx, sch, q, `UPDATE fin_transaction_type SET deleted_at=now(), is_active=false WHERE id=$1`, id)
	return &OKResponse{OK: true}, nil
}

func validateTxnTypeInput(code, label, flow, categoryKind string) error {
	code = strings.TrimSpace(code)
	if !txnTypeCodeRe.MatchString(code) {
		return appErrs.BadRequest("kode harus huruf kecil, angka, underscore (contoh: biaya_kirim)")
	}
	if strings.TrimSpace(label) == "" {
		return appErrs.BadRequest("label tidak boleh kosong")
	}
	if !validTxnFlows[flow] {
		return appErrs.BadRequest("flow harus income, expense, transfer, atau adjustment")
	}
	if categoryKind == "" {
		categoryKind = "any"
	}
	if !validCategoryKinds[categoryKind] {
		return appErrs.BadRequest("categoryKind tidak valid")
	}
	return nil
}

func scanTransactionTypes(rows *sql.Rows) ([]TransactionType, error) {
	var out []TransactionType
	for rows.Next() {
		var t TransactionType
		if err := rows.Scan(&t.ID, &t.Code, &t.Label, &t.Flow, &t.CategoryKind,
			&t.ShowInQuick, &t.DisplayOrder, &t.IsSystem, &t.OwnerOnly, &t.IsActive); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadTransactionTypeByID(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, id string) (*TransactionType, error) {
	row := qrow(ctx, sch, q, `
		SELECT id, code, label, flow, category_kind, show_in_quick, display_order,
		       is_system, owner_only, is_active
		FROM fin_transaction_type WHERE id=$1 AND deleted_at IS NULL`, id)
	var t TransactionType
	if err := row.Scan(&t.ID, &t.Code, &t.Label, &t.Flow, &t.CategoryKind,
		&t.ShowInQuick, &t.DisplayOrder, &t.IsSystem, &t.OwnerOnly, &t.IsActive); err != nil {
		return nil, appErrs.NotFound("jenis transaksi tidak ditemukan")
	}
	return &t, nil
}

func loadTransactionTypeByCode(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, code string) (*TransactionType, error) {
	row := qrow(ctx, sch, q, `
		SELECT id, code, label, flow, category_kind, show_in_quick, display_order,
		       is_system, owner_only, is_active
		FROM fin_transaction_type
		WHERE code=$1 AND deleted_at IS NULL AND is_active=true`, code)
	var t TransactionType
	if err := row.Scan(&t.ID, &t.Code, &t.Label, &t.Flow, &t.CategoryKind,
		&t.ShowInQuick, &t.DisplayOrder, &t.IsSystem, &t.OwnerOnly, &t.IsActive); err != nil {
		return nil, appErrs.NotFound("jenis transaksi tidak valid")
	}
	return &t, nil
}
