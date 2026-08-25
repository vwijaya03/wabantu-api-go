package finance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

// ============================================================
// MONTHLY BILLING CHECKLIST (tagihan bulanan)
// Template = master; fin_checklist_item = instance per due_date.
// Semua item periode status=done → buat transaksi expense (idempotent per item).
// ============================================================

type ListChecklistTemplatesParams struct {
	Q          string `query:"q"`
	Page       int    `query:"page"`
	PageSize   int    `query:"pageSize"`
	Frequency  string `query:"frequency"`
	ActiveOnly bool   `query:"activeOnly"`
}

type ListChecklistTemplatesPaginatedResponse struct {
	Items []ChecklistTemplate `json:"items"`
	Total int                 `json:"total"`
}

type UpdateChecklistTemplateParams struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	AmountHint  *float64 `json:"amountHint,omitempty"`
	CategoryID  *string  `json:"categoryId,omitempty"`
	WalletID    *string  `json:"walletId,omitempty"`
	Frequency   *string  `json:"frequency,omitempty"`
	DayOfMonth  *int     `json:"dayOfMonth,omitempty"`
	DueDate     *string  `json:"dueDate,omitempty"`
	Order       *int     `json:"order,omitempty"`
	IsActive    *bool    `json:"isActive,omitempty"`
}

type MonthlyBillingParams struct {
	Period string `query:"period"`
}

type MonthlyBillingResponse struct {
	Period       string          `json:"period"`
	Items        []ChecklistItem `json:"items"`
	Total        int             `json:"total"`
	Checked      int             `json:"checked"`
	AllChecked   bool            `json:"allChecked"`
	AllPosted    bool            `json:"allPosted"`
	PostedCount  int             `json:"postedCount"`
}

type ToggleMonthlyBillingParams struct {
	ItemID  string `json:"itemId"`
	Checked bool   `json:"checked"`
}

type ToggleMonthlyBillingResponse struct {
	Item    ChecklistItem          `json:"item"`
	Billing MonthlyBillingResponse `json:"billing"`
}

func parseBillingPeriod(period string) (time.Time, time.Time, string, error) {
	period = strings.TrimSpace(period)
	if period == "" {
		now := time.Now()
		period = now.Format("2006-01")
	}
	parts := strings.Split(period, "-")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, "", appErrs.BadRequest("period harus format YYYY-MM")
	}
	y, err := strconv.Atoi(parts[0])
	if err != nil || y < 2000 || y > 2100 {
		return time.Time{}, time.Time{}, "", appErrs.BadRequest("tahun period tidak valid")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 1 || m > 12 {
		return time.Time{}, time.Time{}, "", appErrs.BadRequest("bulan period tidak valid")
	}
	start := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return start, end, fmt.Sprintf("%04d-%02d", y, m), nil
}

// ensureMonthlyBillingItems upserts checklist rows for the month in one statement.
// Do not run Exec while a Rows cursor is open on the same *sql.Conn — that deadlocks
// the connection until timeout ("driver: bad connection").
func ensureMonthlyBillingItems(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, periodStart time.Time) error {
	monthStart := periodStart.Format("2006-01-02")
	_, err := qexec(ctx, sch, q, `
		INSERT INTO fin_checklist_item (template_id, due_date)
		SELECT
		  t.id,
		  (
		    date_trunc('month', $1::date)::date
		    + (
		        LEAST(
		          GREATEST(COALESCE(t.day_of_month, 1), 1),
		          EXTRACT(DAY FROM (date_trunc('month', $1::date) + interval '1 month - 1 day'))::int
		        ) - 1
		      ) * interval '1 day'
		  )::date
		FROM fin_checklist_template t
		WHERE t.is_active = true AND t.frequency = 'monthly'
		ON CONFLICT (template_id, due_date) DO NOTHING`,
		monthStart)
	return err
}

func scanChecklistItemRow(
	rows interface {
		Scan(dest ...any) error
	},
) (ChecklistItem, error) {
	var it ChecklistItem
	var txnID, completedBy, note sql.NullString
	var completedAt sql.NullTime
	var amtHint sql.NullFloat64
	var catID, walletID sql.NullString
	var titleEnc, titleLegacy string
	if err := rows.Scan(&it.ID, &it.TemplateID, &titleEnc, &titleLegacy, &it.DueDate, &it.Status,
		&txnID, &completedBy, &completedAt, &note, &amtHint, &catID, &walletID); err != nil {
		return it, err
	}
	var decErr error
	it.TemplateTitle, decErr = decryptFinanceTitle(titleEnc, titleLegacy)
	if decErr != nil {
		return it, decErr
	}
	if txnID.Valid {
		it.TransactionID = &txnID.String
	}
	if completedBy.Valid {
		it.CompletedBy = &completedBy.String
	}
	if completedAt.Valid {
		it.CompletedAt = &completedAt.Time
	}
	if note.Valid {
		it.Note = &note.String
	}
	if amtHint.Valid {
		s := fmt.Sprintf("%.2f", amtHint.Float64)
		it.AmountHint = &s
	}
	if catID.Valid {
		it.CategoryID = &catID.String
	}
	if walletID.Valid {
		it.WalletID = &walletID.String
	}
	return it, nil
}

func repairOrphanChecklistTransactionIDs(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, periodStart, periodEnd time.Time) error {
	startStr := periodStart.Format("2006-01-02")
	endStr := periodEnd.Format("2006-01-02")
	_, err := qexec(ctx, sch, q, `
		UPDATE fin_checklist_item i
		SET transaction_id = NULL
		FROM fin_checklist_template t
		WHERE i.template_id = t.id
		  AND t.frequency = 'monthly'
		  AND i.due_date >= $1 AND i.due_date < $2
		  AND i.transaction_id IS NOT NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM fin_transaction ft
		    WHERE ft.id = i.transaction_id AND ft.deleted_at IS NULL
		  )`, startStr, endStr)
	return err
}

func syncMonthlyBillingPeriod(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, tenantSchema string, u *types.AuthUser, periodStart, periodEnd time.Time, periodLabel string) error {
	if err := repairOrphanChecklistTransactionIDs(ctx, sch, q, periodStart, periodEnd); err != nil {
		return appErrs.Internal(err.Error())
	}
	return tryPostMonthlyBillingTransactions(ctx, sch, q, tenantSchema, u, periodStart, periodEnd, periodLabel)
}

func loadMonthlyBilling(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, periodStart, periodEnd time.Time, periodLabel string) (*MonthlyBillingResponse, error) {
	startStr := periodStart.Format("2006-01-02")
	endStr := periodEnd.Format("2006-01-02")

	itemRows, err := qquery(ctx, sch, q, `
		SELECT i.id, i.template_id, COALESCE(t.title_enc,''), t.title, i.due_date::text, i.status,
		       CASE WHEN ft.id IS NOT NULL THEN ft.id::text ELSE NULL END,
		       i.completed_by, i.completed_at, i.note,
		       t.amount_hint, t.category_id, t.wallet_id
		FROM fin_checklist_item i
		JOIN fin_checklist_template t ON t.id=i.template_id
		LEFT JOIN fin_transaction ft ON ft.id = i.transaction_id AND ft.deleted_at IS NULL
		WHERE t.is_active=true AND t.frequency='monthly'
		  AND i.due_date >= $1 AND i.due_date < $2
		ORDER BY i.due_date, t.display_order, t.title`, startStr, endStr)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer itemRows.Close()

	var items []ChecklistItem
	checked, posted := 0, 0
	for itemRows.Next() {
		it, err := scanChecklistItemRow(itemRows)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if it.Status == "done" {
			checked++
		}
		if it.TransactionID != nil && *it.TransactionID != "" {
			posted++
		}
		items = append(items, it)
	}
	if items == nil {
		items = []ChecklistItem{}
	}
	total := len(items)
	allChecked := total > 0 && checked == total
	allPosted := total > 0 && posted == total
	return &MonthlyBillingResponse{
		Period:      periodLabel,
		Items:       items,
		Total:       total,
		Checked:     checked,
		AllChecked:  allChecked,
		AllPosted:   allPosted,
		PostedCount: posted,
	}, nil
}

//encore:api auth method=GET path=/api/v1/finance/checklist/templates/manage
func ListChecklistTemplatesPaginated(ctx context.Context, p *ListChecklistTemplatesParams) (*ListChecklistTemplatesPaginatedResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 || p.PageSize > 100 {
		p.PageSize = 20
	}

	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	conditions := []string{"1=1"}
	args := []any{}
	i := 1
	if p.ActiveOnly {
		conditions = append(conditions, "is_active=true")
	}
	if strings.TrimSpace(p.Frequency) != "" {
		conditions = append(conditions, fmt.Sprintf("frequency=$%d", i))
		args = append(args, strings.TrimSpace(p.Frequency))
		i++
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		conditions = append(conditions, fmt.Sprintf("(title ILIKE $%d OR COALESCE(description,'') ILIKE $%d)", i, i))
		args = append(args, "%"+q+"%")
		i++
	}
	where := strings.Join(conditions, " AND ")

	var total int
	if err := qrow(ctx, sch, q, `SELECT COUNT(*) FROM fin_checklist_template WHERE `+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	ref := financeNow(ctx, sch, q)

	offset := (p.Page - 1) * p.PageSize
	listArgs := append(append([]any{}, args...), p.PageSize, offset)
	rows, err := qquery(ctx, sch, q, fmt.Sprintf(`
		SELECT id, title, description, amount_hint, category_id, wallet_id,
		       frequency, day_of_month, due_anchor_date, is_active, display_order, created_at
		FROM fin_checklist_template WHERE %s
		ORDER BY display_order, title
		LIMIT $%d OFFSET $%d`, where, i, i+1), listArgs...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	items, err := scanChecklistTemplates(rows, ref)
	if err != nil {
		return nil, err
	}
	return &ListChecklistTemplatesPaginatedResponse{Items: items, Total: total}, nil
}

func scanChecklistTemplates(rows *sql.Rows, ref time.Time) ([]ChecklistTemplate, error) {
	var tpls []ChecklistTemplate
	for rows.Next() {
		var t ChecklistTemplate
		var desc, catID, walletID sql.NullString
		var amtHint sql.NullFloat64
		var domN sql.NullInt64
		var anchor sql.NullTime
		if err := rows.Scan(&t.ID, &t.Title, &desc, &amtHint, &catID, &walletID,
			&t.Frequency, &domN, &anchor, &t.IsActive, &t.Order, &t.CreatedAt); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if desc.Valid {
			t.Description = &desc.String
		}
		if amtHint.Valid {
			s := fmt.Sprintf("%.2f", amtHint.Float64)
			t.AmountHint = &s
		}
		if catID.Valid {
			t.CategoryID = &catID.String
		}
		if walletID.Valid {
			t.WalletID = &walletID.String
		}
		attachDueAnchorDate(&t, anchor, domN, ref)
		tpls = append(tpls, t)
	}
	if tpls == nil {
		tpls = []ChecklistTemplate{}
	}
	return tpls, rows.Err()
}

//encore:api auth method=PATCH path=/api/v1/finance/checklist/templates/:id
func UpdateChecklistTemplate(ctx context.Context, id string, p *UpdateChecklistTemplateParams) (*ChecklistTemplate, error) {
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

	sets := []string{}
	args := []any{}
	i := 1
	dueScheduleChanged := false
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" {
			return nil, appErrs.BadRequest("judul tidak boleh kosong")
		}
		sets = append(sets, fmt.Sprintf("title=$%d", i))
		args = append(args, t)
		i++
	}
	if p.Description != nil {
		sets = append(sets, fmt.Sprintf("description=$%d", i))
		args = append(args, p.Description)
		i++
	}
	if p.AmountHint != nil {
		sets = append(sets, fmt.Sprintf("amount_hint=$%d", i))
		args = append(args, p.AmountHint)
		i++
	}
	if p.CategoryID != nil {
		sets = append(sets, fmt.Sprintf("category_id=$%d", i))
		args = append(args, nullUUID(*p.CategoryID))
		i++
	}
	if p.WalletID != nil {
		sets = append(sets, fmt.Sprintf("wallet_id=$%d", i))
		args = append(args, nullUUID(*p.WalletID))
		i++
	}
	if p.Frequency != nil {
		sets = append(sets, fmt.Sprintf("frequency=$%d", i))
		args = append(args, strings.TrimSpace(*p.Frequency))
		i++
	}
	if p.DueDate != nil {
		anchor, dom, err := parseMonthlyDueDateInput(*p.DueDate)
		if err != nil {
			return nil, err
		}
		sets = append(sets, fmt.Sprintf("due_anchor_date=$%d", i), fmt.Sprintf("day_of_month=$%d", i+1))
		args = append(args, anchor, dom)
		i += 2
		dueScheduleChanged = true
	} else if p.DayOfMonth != nil {
		if *p.DayOfMonth < 1 || *p.DayOfMonth > 31 {
			return nil, appErrs.BadRequest("tanggal jatuh tempo tidak valid (hari 1–31)")
		}
		ref := financeNow(ctx, sch, q)
		anchor := synthesizeDueAnchor(ref.Year(), ref.Month(), *p.DayOfMonth)
		sets = append(sets, fmt.Sprintf("day_of_month=$%d", i), fmt.Sprintf("due_anchor_date=$%d", i+1))
		args = append(args, *p.DayOfMonth, anchor)
		i += 2
		dueScheduleChanged = true
	}
	if p.Order != nil {
		sets = append(sets, fmt.Sprintf("display_order=$%d", i))
		args = append(args, *p.Order)
		i++
	}
	if p.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active=$%d", i))
		args = append(args, *p.IsActive)
		i++
	}
	if len(sets) == 0 {
		return nil, appErrs.BadRequest("tidak ada field untuk diperbarui")
	}
	args = append(args, id)
	res, err := qexec(ctx, sch, q,
		fmt.Sprintf(`UPDATE fin_checklist_template SET %s WHERE id=$%d`, strings.Join(sets, ", "), i),
		args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, appErrs.NotFound("template tidak ditemukan")
	}

	if dueScheduleChanged {
		if err := reconcilePendingChecklistItems(ctx, sch, q, id, currentMonthStart(ctx, sch, q)); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}

	var t ChecklistTemplate
	var desc, catID, walletID sql.NullString
	var amtHint sql.NullFloat64
	var domN sql.NullInt64
	var anchor sql.NullTime
	err = qrow(ctx, sch, q, `
		SELECT id, title, description, amount_hint, category_id, wallet_id,
		       frequency, day_of_month, due_anchor_date, is_active, display_order, created_at
		FROM fin_checklist_template WHERE id=$1`, id).Scan(
		&t.ID, &t.Title, &desc, &amtHint, &catID, &walletID,
		&t.Frequency, &domN, &anchor, &t.IsActive, &t.Order, &t.CreatedAt)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if desc.Valid {
		t.Description = &desc.String
	}
	if amtHint.Valid {
		s := fmt.Sprintf("%.2f", amtHint.Float64)
		t.AmountHint = &s
	}
	if catID.Valid {
		t.CategoryID = &catID.String
	}
	if walletID.Valid {
		t.WalletID = &walletID.String
	}
	attachDueAnchorDate(&t, anchor, domN, financeNow(ctx, sch, q))
	return &t, nil
}

func nullUUID(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

//encore:api auth method=GET path=/api/v1/finance/checklist/monthly
func GetMonthlyBillingChecklist(ctx context.Context, p *MonthlyBillingParams) (*MonthlyBillingResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	periodStart, periodEnd, label, err := parseBillingPeriod(p.Period)
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()
	if err := ensureMonthlyBillingItems(ctx, sch, q, periodStart); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := syncMonthlyBillingPeriod(ctx, sch, q, u.TenantSchema, u, periodStart, periodEnd, label); err != nil {
		return nil, err
	}
	return loadMonthlyBilling(ctx, sch, q, periodStart, periodEnd, label)
}

//encore:api auth method=POST path=/api/v1/finance/checklist/monthly/toggle
func ToggleMonthlyBillingItem(ctx context.Context, p *ToggleMonthlyBillingParams) (*ToggleMonthlyBillingResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ItemID) == "" {
		return nil, appErrs.BadRequest("itemId wajib")
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	var dueDate, status, freq string
	err = qrow(ctx, sch, q, `
		SELECT i.due_date::text, i.status, t.frequency
		FROM fin_checklist_item i
		JOIN fin_checklist_template t ON t.id=i.template_id
		WHERE i.id=$1`, p.ItemID).Scan(&dueDate, &status, &freq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appErrs.NotFound("item checklist tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}
	if freq != "monthly" {
		return nil, appErrs.BadRequest("item bukan tagihan bulanan")
	}

	periodLabel := dueDate[:7]
	periodStart, periodEnd, label, err := parseBillingPeriod(periodLabel)
	if err != nil {
		return nil, err
	}

	if !p.Checked {
		if err := removeChecklistBillingTransaction(ctx, sch, q, u, p.ItemID); err != nil {
			return nil, err
		}
	}

	newStatus := "pending"
	if p.Checked {
		newStatus = "done"
	}
	_, err = qexec(ctx, sch, q,
		`UPDATE fin_checklist_item SET status=$1,
		 completed_by=CASE WHEN $2='done' THEN $3::uuid ELSE NULL END,
		 completed_at=CASE WHEN $2='done' THEN now() ELSE NULL END
		 WHERE id=$4`,
		newStatus, newStatus, u.AccountID, p.ItemID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if err := ensureMonthlyBillingItems(ctx, sch, q, periodStart); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := syncMonthlyBillingPeriod(ctx, sch, q, u.TenantSchema, u, periodStart, periodEnd, label); err != nil {
		return nil, err
	}
	billing, err := loadMonthlyBilling(ctx, sch, q, periodStart, periodEnd, label)
	if err != nil {
		return nil, err
	}
	var item ChecklistItem
	for _, it := range billing.Items {
		if it.ID == p.ItemID {
			item = it
			break
		}
	}
	if item.ID == "" {
		item = ChecklistItem{ID: p.ItemID, Status: newStatus}
	}
	return &ToggleMonthlyBillingResponse{Item: item, Billing: *billing}, nil
}

func tryPostMonthlyBillingTransactions(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, tenantSchema string, u *types.AuthUser, periodStart, periodEnd time.Time, periodLabel string) error {
	startStr := periodStart.Format("2006-01-02")
	endStr := periodEnd.Format("2006-01-02")

	var unpostedDone int
	err := qrow(ctx, sch, q, `
		SELECT COUNT(*)::int
		FROM fin_checklist_item i
		JOIN fin_checklist_template t ON t.id=i.template_id
		WHERE t.is_active=true AND t.frequency='monthly'
		  AND i.due_date >= $1 AND i.due_date < $2
		  AND i.status='done' AND i.transaction_id IS NULL`, startStr, endStr).Scan(&unpostedDone)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if unpostedDone == 0 {
		return nil
	}

	rows, err := qquery(ctx, sch, q, `
		SELECT i.id, i.due_date::text, COALESCE(t.title_enc,''), t.title, t.amount_hint, t.category_id, t.wallet_id
		FROM fin_checklist_item i
		JOIN fin_checklist_template t ON t.id=i.template_id
		WHERE t.is_active=true AND t.frequency='monthly'
		  AND i.due_date >= $1 AND i.due_date < $2
		  AND i.status='done' AND i.transaction_id IS NULL
		ORDER BY i.due_date`, startStr, endStr)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()

	type row struct {
		id, due, title string
		amount         float64
		categoryID     sql.NullString
		walletID       sql.NullString
	}
	var pending []row
	for rows.Next() {
		var r row
		var amt sql.NullFloat64
		var titleEnc, titleLegacy string
		if err := rows.Scan(&r.id, &r.due, &titleEnc, &titleLegacy, &amt, &r.categoryID, &r.walletID); err != nil {
			return appErrs.Internal(err.Error())
		}
		r.title, err = decryptFinanceTitle(titleEnc, titleLegacy)
		if err != nil {
			return appErrs.Internal(err.Error())
		}
		if !amt.Valid || amt.Float64 <= 0 {
			return appErrs.BadRequest(fmt.Sprintf("tagihan \"%s\" belum punya nominal — isi estimasi jumlah di template", r.title))
		}
		r.amount = amt.Float64
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}
	rows.Close()

	defaultWallet, err := resolveDefaultExpenseWallet(ctx, sch, q)
	if err != nil {
		return err
	}

	walletsToRefresh := make(map[string]struct{})
	for _, r := range pending {
		wallet := defaultWallet
		if r.walletID.Valid && r.walletID.String != "" {
			wallet = r.walletID.String
		}
		if err := assertWalletAccessible(ctx, sch, q, u, wallet); err != nil {
			return err
		}
		walletsToRefresh[wallet] = struct{}{}
	}

	dbTx, err := tenantPool().BeginTx(ctx, nil)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer dbTx.Rollback()

	for _, r := range pending {
		if err := ensurePeriodUnlocked(ctx, sch, q, walletPeriod(r.due)); err != nil {
			return err
		}

		wallet := defaultWallet
		if r.walletID.Valid && r.walletID.String != "" {
			wallet = r.walletID.String
		}

		ref := "checklist:" + r.id
		var exists bool
		if err := qrow(ctx, sch, dbTx,  `
			SELECT EXISTS(
			  SELECT 1 FROM fin_transaction
			  WHERE reference_no=$1 AND deleted_at IS NULL LIMIT 1
			)`, ref).Scan(&exists); err != nil {
			return appErrs.Internal(err.Error())
		}
		if exists {
			var txnID string
			if err := qrow(ctx, sch, dbTx, 
				`SELECT id::text FROM fin_transaction WHERE reference_no=$1 AND deleted_at IS NULL LIMIT 1`, ref,
			).Scan(&txnID); err == nil {
				_, _ = qexec(ctx, sch, dbTx,  `UPDATE fin_checklist_item SET transaction_id=$1 WHERE id=$2`, txnID, r.id)
			}
			continue
		}

		desc := fmt.Sprintf("%s (Tagihan %s)", r.title, periodLabel)
		tags := []string{"checklist-billing", periodLabel}
		var txnID string
		err = qrow(ctx, sch, dbTx,  `
			INSERT INTO fin_transaction
			 (type, amount, currency, wallet_id, category_id, description, reference_no,
			  transaction_date, status, tags, created_by)
			 VALUES ('expense', $1, 'IDR', $2, $3, $4, $5, $6, 'approved', $7, $8)
			 RETURNING id`,
			r.amount, wallet, nullStr(r.categoryID), desc, ref, r.due, tags, u.AccountID,
		).Scan(&txnID)
		if err != nil {
			return appErrs.Internal("gagal mencatat transaksi tagihan: " + err.Error())
		}
		if _, err := qexec(ctx, sch, dbTx, 
			`UPDATE fin_checklist_item SET transaction_id=$1 WHERE id=$2`, txnID, r.id); err != nil {
			return appErrs.Internal(err.Error())
		}
	}

	if err := dbTx.Commit(); err != nil {
		return appErrs.Internal(err.Error())
	}

	for w := range walletsToRefresh {
		refreshWallets(ctx, sch, q, w, nil)
	}
	_ = usage.RecordEvent(ctx, tenantSchema, "fin_transaction_created", len(pending), nil)
	return nil
}

// removeChecklistBillingTransaction soft-deletes the expense linked to a checklist item.
func removeChecklistBillingTransaction(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, u *types.AuthUser, itemID string) error {
	ref := "checklist:" + itemID

	var dueDate string
	if err := qrow(ctx, sch, q,
		`SELECT due_date::text FROM fin_checklist_item WHERE id=$1`, itemID,
	).Scan(&dueDate); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appErrs.NotFound("item checklist tidak ditemukan")
		}
		return appErrs.Internal(err.Error())
	}

	if err := ensurePeriodUnlocked(ctx, sch, q, walletPeriod(dueDate)); err != nil {
		return err
	}

	rows, err := qquery(ctx, sch, q, `
		SELECT DISTINCT wallet_id::text
		FROM fin_transaction
		WHERE deleted_at IS NULL AND type = 'expense'
		  AND (
		    reference_no = $1
		    OR id IN (
		      SELECT transaction_id FROM fin_checklist_item
		      WHERE id = $2 AND transaction_id IS NOT NULL
		    )
		  )`, ref, itemID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var walletIDs []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return appErrs.Internal(err.Error())
		}
		walletIDs = append(walletIDs, w)
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}
	rows.Close()

	res, err := qexec(ctx, sch, q, `
		UPDATE fin_transaction
		SET deleted_at = now(), deleted_by = $1, updated_at = now()
		WHERE deleted_at IS NULL AND type = 'expense'
		  AND (
		    reference_no = $2
		    OR id IN (
		      SELECT transaction_id FROM fin_checklist_item
		      WHERE id = $3 AND transaction_id IS NOT NULL
		    )
		  )`, u.AccountID, ref, itemID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}

	_, err = qexec(ctx, sch, q,
		`UPDATE fin_checklist_item SET transaction_id = NULL WHERE id = $1`, itemID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}

	if n, _ := res.RowsAffected(); n > 0 {
		for _, w := range walletIDs {
			refreshWallets(ctx, sch, q, w, nil)
		}
	}
	return nil
}

func resolveDefaultExpenseWallet(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) (string, error) {
	var walletID string
	err := qrow(ctx, sch, q, `
		SELECT id::text FROM fin_wallet
		WHERE deleted_at IS NULL AND is_active = true
		ORDER BY CASE WHEN type = 'cash' THEN 0 ELSE 1 END, display_order, created_at
		LIMIT 1`).Scan(&walletID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", appErrs.BadRequest("belum ada dompet aktif untuk mencatat pengeluaran tagihan")
	}
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	return walletID, nil
}
