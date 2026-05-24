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
// BUDGET
// ============================================================

type Budget struct {
	ID           string    `json:"id"`
	CategoryID   string    `json:"categoryId"`
	CategoryName string    `json:"categoryName"`
	Period       string    `json:"period"`
	Amount       string    `json:"amount"`
	Spent        string    `json:"spent"`
	Remaining    string    `json:"remaining"`
	Pct          int       `json:"pct"`
	Status       string    `json:"status"` // "ok"|"warn"|"over"
	CreatedAt    time.Time `json:"createdAt"`
}

type BudgetListParams struct {
	Period string `query:"period"` // YYYY-MM
}

type BudgetListResponse struct {
	Budgets []Budget `json:"budgets"`
	Period  string   `json:"period"`
}

type UpsertBudgetParams struct {
	CategoryID string  `json:"categoryId"`
	Period     string  `json:"period"`
	Amount     float64 `json:"amount"`
}

//encore:api auth method=GET path=/api/v1/finance/budgets
func ListBudgets(ctx context.Context, p *BudgetListParams) (*BudgetListResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	period := p.Period
	if period == "" {
		period = time.Now().Format("2006-01")
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT b.id, b.category_id, COALESCE(c.name,''), b.period, b.amount,
		       COALESCE(SUM(t.amount),0) AS spent,
		       b.created_at
		FROM fin_budget b
		LEFT JOIN fin_category c ON c.id = b.category_id
		LEFT JOIN fin_transaction t ON
		    t.category_id = b.category_id
		    AND to_char(t.transaction_date,'YYYY-MM') = b.period
		    AND t.type = 'expense'
		    AND t.status = 'approved'
		    AND t.deleted_at IS NULL
		WHERE b.period = $1
		GROUP BY b.id, b.category_id, c.name, b.period, b.amount, b.created_at
		ORDER BY c.name`, period)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var budgets []Budget
	for rows.Next() {
		var b Budget
		var amount, spent float64
		if err := rows.Scan(&b.ID, &b.CategoryID, &b.CategoryName, &b.Period, &amount, &spent, &b.CreatedAt); err != nil {
			continue
		}
		remaining := amount - spent
		pct := 0
		if amount > 0 {
			pct = int(spent / amount * 100)
		}
		status := "ok"
		if pct >= 100 {
			status = "over"
		} else if pct >= 80 {
			status = "warn"
		}
		b.Amount = fmt.Sprintf("%.2f", amount)
		b.Spent = fmt.Sprintf("%.2f", spent)
		b.Remaining = fmt.Sprintf("%.2f", remaining)
		b.Pct = pct
		b.Status = status
		budgets = append(budgets, b)
	}
	if budgets == nil {
		budgets = []Budget{}
	}
	return &BudgetListResponse{Budgets: budgets, Period: period}, nil
}

//encore:api auth method=POST path=/api/v1/finance/budgets
func UpsertBudget(ctx context.Context, p *UpsertBudgetParams) (*Budget, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p.Amount <= 0 {
		return nil, appErrs.BadRequest("anggaran harus lebih dari 0")
	}
	if p.CategoryID == "" {
		return nil, appErrs.BadRequest("kategori harus dipilih")
	}
	if p.Period == "" {
		p.Period = time.Now().Format("2006-01")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_budget (category_id, period, amount, created_by)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (category_id, period) DO UPDATE SET amount=$3, updated_at=now()
		 RETURNING id`,
		p.CategoryID, p.Period, p.Amount, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditFinance(ctx, conn, u, "budget", id, "upsert", nil, p)
	return &Budget{ID: id, CategoryID: p.CategoryID, Period: p.Period,
		Amount: fmt.Sprintf("%.2f", p.Amount), CreatedAt: time.Now()}, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/budgets/:id
func DeleteBudget(ctx context.Context, id string) (*OKResponse, error) {
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
	conn.ExecContext(ctx, `DELETE FROM fin_budget WHERE id=$1`, id)
	return &OKResponse{OK: true}, nil
}

// BudgetSummary returns category spending vs budget for a period,
// used to show over-budget warnings.
type BudgetSummaryResponse struct {
	Period       string   `json:"period"`
	TotalBudget  string   `json:"totalBudget"`
	TotalSpent   string   `json:"totalSpent"`
	OverBudget   []string `json:"overBudget"`   // category names
	WarnBudget   []string `json:"warnBudget"`
}

//encore:api auth method=GET path=/api/v1/finance/budgets/summary
func BudgetSummary(ctx context.Context, p *BudgetListParams) (*BudgetSummaryResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	period := p.Period
	if period == "" {
		period = time.Now().Format("2006-01")
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT COALESCE(c.name,''), b.amount,
		       COALESCE(SUM(t.amount),0) AS spent
		FROM fin_budget b
		LEFT JOIN fin_category c ON c.id=b.category_id
		LEFT JOIN fin_transaction t ON
		    t.category_id=b.category_id
		    AND to_char(t.transaction_date,'YYYY-MM')=b.period
		    AND t.type='expense' AND t.status='approved' AND t.deleted_at IS NULL
		WHERE b.period=$1
		GROUP BY c.name, b.amount`, period)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var totalBudget, totalSpent float64
	var over, warn []string
	for rows.Next() {
		var name string
		var budget, spent float64
		rows.Scan(&name, &budget, &spent)
		totalBudget += budget
		totalSpent += spent
		pct := 0
		if budget > 0 {
			pct = int(spent / budget * 100)
		}
		if pct >= 100 {
			over = append(over, name)
		} else if pct >= 80 {
			warn = append(warn, name)
		}
	}
	if over == nil {
		over = []string{}
	}
	if warn == nil {
		warn = []string{}
	}
	return &BudgetSummaryResponse{
		Period:      period,
		TotalBudget: fmt.Sprintf("%.2f", totalBudget),
		TotalSpent:  fmt.Sprintf("%.2f", totalSpent),
		OverBudget:  over,
		WarnBudget:  warn,
	}, nil
}

// CategorySpending is used for reporting/charts.
type CategorySpendingItem struct {
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	Total        string `json:"total"`
	TxnCount     int    `json:"txnCount"`
}

type CategorySpendingResponse struct {
	Items  []CategorySpendingItem `json:"items"`
	Period string                 `json:"period"`
}

//encore:api auth method=GET path=/api/v1/finance/reports/category-spending
func CategorySpending(ctx context.Context, p *BudgetListParams) (*CategorySpendingResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	period := p.Period
	if period == "" {
		period = time.Now().Format("2006-01")
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT COALESCE(t.category_id::text,''), COALESCE(c.name,'(Tanpa Kategori)'),
		       SUM(t.amount), COUNT(*)
		FROM fin_transaction t
		LEFT JOIN fin_category c ON c.id=t.category_id
		WHERE t.type='expense' AND t.status='approved' AND t.deleted_at IS NULL
		  AND to_char(t.transaction_date,'YYYY-MM')=$1
		GROUP BY t.category_id, c.name
		ORDER BY SUM(t.amount) DESC`, period)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []CategorySpendingItem
	for rows.Next() {
		var it CategorySpendingItem
		var total float64
		rows.Scan(&it.CategoryID, &it.CategoryName, &total, &it.TxnCount)
		it.Total = fmt.Sprintf("%.2f", total)
		items = append(items, it)
	}
	if items == nil {
		items = []CategorySpendingItem{}
	}
	return &CategorySpendingResponse{Items: items, Period: period}, nil
}

// MonthlyComparison returns income/expense totals for last N months.
type MonthlyComparisonItem struct {
	Period  string `json:"period"`
	Income  string `json:"income"`
	Expense string `json:"expense"`
	Net     string `json:"net"`
}

type MonthlyComparisonResponse struct {
	Items []MonthlyComparisonItem `json:"items"`
}

type MonthlyComparisonParams struct {
	Months int `query:"months"` // default 6
}

//encore:api auth method=GET path=/api/v1/finance/reports/monthly-comparison
func MonthlyComparison(ctx context.Context, p *MonthlyComparisonParams) (*MonthlyComparisonResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	months := p.Months
	if months <= 0 || months > 24 {
		months = 6
	}

	periods := make([]string, months)
	for i := 0; i < months; i++ {
		periods[i] = time.Now().AddDate(0, -i, 0).Format("2006-01")
	}

	placeholders := make([]string, len(periods))
	args := make([]any, len(periods))
	for i, p := range periods {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = p
	}

	q := fmt.Sprintf(`
		SELECT to_char(transaction_date,'YYYY-MM') AS period,
		       COALESCE(SUM(CASE WHEN type IN ('income','dividend','interest','cashback') THEN amount ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN type IN ('expense','investment_buy') THEN amount ELSE 0 END),0)
		FROM fin_transaction
		WHERE status='approved' AND deleted_at IS NULL
		  AND to_char(transaction_date,'YYYY-MM') IN (%s)
		GROUP BY to_char(transaction_date,'YYYY-MM')
		ORDER BY period DESC`, strings.Join(placeholders, ","))

	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	dataMap := map[string]*MonthlyComparisonItem{}
	for rows.Next() {
		var pr string
		var inc, exp float64
		rows.Scan(&pr, &inc, &exp)
		dataMap[pr] = &MonthlyComparisonItem{
			Period:  pr,
			Income:  fmt.Sprintf("%.2f", inc),
			Expense: fmt.Sprintf("%.2f", exp),
			Net:     fmt.Sprintf("%.2f", inc-exp),
		}
	}

	var items []MonthlyComparisonItem
	for _, p := range periods {
		if v, ok := dataMap[p]; ok {
			items = append(items, *v)
		} else {
			items = append(items, MonthlyComparisonItem{Period: p, Income: "0.00", Expense: "0.00", Net: "0.00"})
		}
	}
	return &MonthlyComparisonResponse{Items: items}, nil
}

// Suppress unused sql import warning
var _ = sql.ErrNoRows
