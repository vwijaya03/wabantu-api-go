package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// ============================================================
// DAILY EXPENSE CHECKLIST
// ============================================================

type ChecklistTemplate struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description *string   `json:"description,omitempty"`
	AmountHint  *string   `json:"amountHint,omitempty"`
	CategoryID  *string   `json:"categoryId,omitempty"`
	WalletID    *string   `json:"walletId,omitempty"`
	Frequency   string    `json:"frequency"`
	DayOfMonth  *int      `json:"dayOfMonth,omitempty"`
	IsActive    bool      `json:"isActive"`
	Order       int       `json:"order"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ChecklistItem struct {
	ID            string    `json:"id"`
	TemplateID    string    `json:"templateId"`
	TemplateTitle string    `json:"templateTitle"`
	DueDate       string    `json:"dueDate"`
	Status        string    `json:"status"`
	TransactionID *string   `json:"transactionId,omitempty"`
	CompletedBy   *string   `json:"completedBy,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	Note          *string   `json:"notes,omitempty"`
	AmountHint    *string   `json:"amountHint,omitempty"`
	CategoryID    *string   `json:"categoryId,omitempty"`
	WalletID      *string   `json:"walletId,omitempty"`
}

type ChecklistListResponse struct {
	Templates []ChecklistTemplate `json:"templates"`
}

type TodayChecklistResponse struct {
	Items   []ChecklistItem `json:"items"`
	Date    string          `json:"date"`
	Pending int             `json:"pending"`
}

type CreateChecklistTemplateParams struct {
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	AmountHint  *float64 `json:"amountHint,omitempty"`
	CategoryID  *string `json:"categoryId,omitempty"`
	WalletID    *string `json:"walletId,omitempty"`
	Frequency   string  `json:"frequency"`
	DayOfMonth  *int    `json:"dayOfMonth,omitempty"`
	Order       int     `json:"order"`
}

type ChecklistActionParams struct {
	ItemID        string  `json:"itemId"`
	Action        string  `json:"action"` // "done"|"skip"
	Note          *string `json:"note,omitempty"`
	TransactionID *string `json:"transactionId,omitempty"`
}

//encore:api auth method=GET path=/api/v1/finance/checklist/templates
func ListChecklistTemplates(ctx context.Context) (*ChecklistListResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT id, title, description, amount_hint, category_id, wallet_id,
		       frequency, day_of_month, is_active, display_order, created_at
		FROM fin_checklist_template WHERE is_active=true ORDER BY display_order, title`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var tpls []ChecklistTemplate
	for rows.Next() {
		var t ChecklistTemplate
		var desc, catID, walletID sql.NullString
		var amtHint sql.NullFloat64
		var domN sql.NullInt64
		rows.Scan(&t.ID, &t.Title, &desc, &amtHint, &catID, &walletID,
			&t.Frequency, &domN, &t.IsActive, &t.Order, &t.CreatedAt)
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
		if domN.Valid {
			v := int(domN.Int64)
			t.DayOfMonth = &v
		}
		tpls = append(tpls, t)
	}
	if tpls == nil {
		tpls = []ChecklistTemplate{}
	}
	return &ChecklistListResponse{Templates: tpls}, nil
}

//encore:api auth method=POST path=/api/v1/finance/checklist/templates
func CreateChecklistTemplate(ctx context.Context, p *CreateChecklistTemplateParams) (*ChecklistTemplate, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Title) == "" {
		return nil, appErrs.BadRequest("judul checklist tidak boleh kosong")
	}
	if p.Frequency == "" {
		p.Frequency = "daily"
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_checklist_template
		 (title, description, amount_hint, category_id, wallet_id, frequency, day_of_month, display_order, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		strings.TrimSpace(p.Title), p.Description, p.AmountHint, p.CategoryID, p.WalletID,
		p.Frequency, p.DayOfMonth, p.Order, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	t := &ChecklistTemplate{
		ID: id, Title: p.Title, CategoryID: p.CategoryID,
		WalletID: p.WalletID, Frequency: p.Frequency,
		DayOfMonth: p.DayOfMonth, IsActive: true, Order: p.Order, CreatedAt: time.Now(),
	}
	if p.AmountHint != nil {
		s := fmt.Sprintf("%.2f", *p.AmountHint)
		t.AmountHint = &s
	}
	return t, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/checklist/templates/:id
func DeleteChecklistTemplate(ctx context.Context, id string) (*OKResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	conn.ExecContext(ctx, `UPDATE fin_checklist_template SET is_active=false WHERE id=$1`, id)
	return &OKResponse{OK: true}, nil
}

// GetTodayChecklist returns today's checklist items (auto-creates missing ones).
//
//encore:api auth method=GET path=/api/v1/finance/checklist/today
func GetTodayChecklist(ctx context.Context) (*TodayChecklistResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	today := time.Now().Format("2006-01-02")
	dom := time.Now().Day()

	// Ensure checklist items exist for today
	rows, err := conn.QueryContext(ctx, `
		SELECT id, frequency, day_of_month, amount_hint, category_id, wallet_id
		FROM fin_checklist_template WHERE is_active=true`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		var id, freq string
		var dayM sql.NullInt64
		var amtHint sql.NullFloat64
		var catID, walletID sql.NullString
		rows.Scan(&id, &freq, &dayM, &amtHint, &catID, &walletID)

		include := false
		switch freq {
		case "daily":
			include = true
		case "monthly":
			include = dayM.Valid && int(dayM.Int64) == dom
		}
		if !include {
			continue
		}
		// Upsert — only insert if not yet existing for today
		conn.ExecContext(ctx,
			`INSERT INTO fin_checklist_item (template_id, due_date) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			id, today)
	}

	// Fetch items
	itemRows, err := conn.QueryContext(ctx, `
		SELECT i.id, i.template_id, t.title, i.due_date::text, i.status,
		       i.transaction_id, i.completed_by, i.completed_at, i.note,
		       t.amount_hint, t.category_id, t.wallet_id
		FROM fin_checklist_item i
		JOIN fin_checklist_template t ON t.id=i.template_id
		WHERE i.due_date=$1
		ORDER BY i.status, t.display_order`, today)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer itemRows.Close()

	var items []ChecklistItem
	pending := 0
	for itemRows.Next() {
		var it ChecklistItem
		var txnID, completedBy, note sql.NullString
		var completedAt sql.NullTime
		var amtHint sql.NullFloat64
		var catID, walletID sql.NullString
		itemRows.Scan(&it.ID, &it.TemplateID, &it.TemplateTitle, &it.DueDate, &it.Status,
			&txnID, &completedBy, &completedAt, &note, &amtHint, &catID, &walletID)
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
		if it.Status == "pending" {
			pending++
		}
		items = append(items, it)
	}
	if items == nil {
		items = []ChecklistItem{}
	}
	return &TodayChecklistResponse{Items: items, Date: today, Pending: pending}, nil
}

//encore:api auth method=POST path=/api/v1/finance/checklist/action
func ChecklistAction(ctx context.Context, p *ChecklistActionParams) (*ChecklistItem, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if p.Action != "done" && p.Action != "skip" {
		return nil, appErrs.BadRequest("action harus done atau skip")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	status := "done"
	if p.Action == "skip" {
		status = "skipped"
	}
	_, err = conn.ExecContext(ctx,
		`UPDATE fin_checklist_item SET status=$1, completed_by=$2, completed_at=now(),
		 note=COALESCE($3,note), transaction_id=COALESCE($4::uuid,transaction_id)
		 WHERE id=$5`,
		status, u.AccountID, p.Note,
		func() interface{} {
			if p.TransactionID != nil {
				return *p.TransactionID
			}
			return nil
		}(),
		p.ItemID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &ChecklistItem{ID: p.ItemID, Status: status}, nil
}

// suppress unused
var _ = sql.ErrNoRows
