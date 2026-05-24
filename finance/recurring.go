package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/cron"
	"encore.dev/rlog"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

// ============================================================
// RECURRING TRANSACTIONS
// ============================================================

type Recurring struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Type            string    `json:"type"`
	Amount          string    `json:"amount"`
	WalletID        string    `json:"walletId"`
	ToWalletID      *string   `json:"toWalletId,omitempty"`
	CategoryID      *string   `json:"categoryId,omitempty"`
	Description     *string   `json:"description,omitempty"`
	Frequency       string    `json:"frequency"`
	FrequencyValue  int       `json:"frequencyValue"`
	DayOfMonth      *int      `json:"dayOfMonth,omitempty"`
	DayOfWeek       *int      `json:"dayOfWeek,omitempty"`
	Mode            string    `json:"mode"`
	StartDate       string    `json:"startDate"`
	EndDate         *string   `json:"endDate,omitempty"`
	MaxOccurrences  *int      `json:"maxOccurrences,omitempty"`
	OccurrencesDone int       `json:"occurrencesDone"`
	NextRunDate     string    `json:"nextRunDate"`
	IsActive        bool      `json:"isActive"`
	CreatedAt       time.Time `json:"createdAt"`
}

type RecurringListResponse struct {
	Items []Recurring `json:"items"`
}

type CreateRecurringParams struct {
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	Amount         float64  `json:"amount"`
	WalletID       string   `json:"walletId"`
	ToWalletID     *string  `json:"toWalletId,omitempty"`
	CategoryID     *string  `json:"categoryId,omitempty"`
	Description    *string  `json:"description,omitempty"`
	Frequency      string   `json:"frequency"`
	FrequencyValue int      `json:"frequencyValue"`
	DayOfMonth     *int     `json:"dayOfMonth,omitempty"`
	DayOfWeek      *int     `json:"dayOfWeek,omitempty"`
	Mode           string   `json:"mode"`
	StartDate      string   `json:"startDate"`
	EndDate        *string  `json:"endDate,omitempty"`
	MaxOccurrences *int     `json:"maxOccurrences,omitempty"`
}

var validFrequencies = map[string]bool{"daily": true, "weekly": true, "monthly": true, "yearly": true}

//encore:api auth method=GET path=/api/v1/finance/recurring
func ListRecurring(ctx context.Context) (*RecurringListResponse, error) {
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

	rows, err := conn.QueryContext(ctx, `
		SELECT id, title, type, amount, wallet_id, to_wallet_id, category_id, description,
		       frequency, frequency_value, day_of_month, day_of_week, mode,
		       start_date::text, end_date::text, max_occurrences, occurrences_done,
		       next_run_date::text, is_active, created_at
		FROM fin_recurring WHERE deleted_at IS NULL ORDER BY next_run_date`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []Recurring
	for rows.Next() {
		var r Recurring
		var toWalletID, categoryID, description sql.NullString
		var endDate sql.NullString
		var dayOfMonth, dayOfWeek, maxOcc sql.NullInt64
		var amount float64
		err := rows.Scan(&r.ID, &r.Title, &r.Type, &amount, &r.WalletID,
			&toWalletID, &categoryID, &description,
			&r.Frequency, &r.FrequencyValue, &dayOfMonth, &dayOfWeek, &r.Mode,
			&r.StartDate, &endDate, &maxOcc, &r.OccurrencesDone,
			&r.NextRunDate, &r.IsActive, &r.CreatedAt)
		if err != nil {
			continue
		}
		r.Amount = fmt.Sprintf("%.2f", amount)
		if toWalletID.Valid {
			r.ToWalletID = &toWalletID.String
		}
		if categoryID.Valid {
			r.CategoryID = &categoryID.String
		}
		if description.Valid {
			r.Description = &description.String
		}
		if endDate.Valid {
			r.EndDate = &endDate.String
		}
		if dayOfMonth.Valid {
			v := int(dayOfMonth.Int64)
			r.DayOfMonth = &v
		}
		if dayOfWeek.Valid {
			v := int(dayOfWeek.Int64)
			r.DayOfWeek = &v
		}
		if maxOcc.Valid {
			v := int(maxOcc.Int64)
			r.MaxOccurrences = &v
		}
		items = append(items, r)
	}
	if items == nil {
		items = []Recurring{}
	}
	return &RecurringListResponse{Items: items}, nil
}

//encore:api auth method=POST path=/api/v1/finance/recurring
func CreateRecurring(ctx context.Context, p *CreateRecurringParams) (*Recurring, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Title) == "" {
		return nil, appErrs.BadRequest("judul harus diisi")
	}
	if !validTxnTypes[p.Type] {
		return nil, appErrs.BadRequest("jenis transaksi tidak valid")
	}
	if p.Amount <= 0 {
		return nil, appErrs.BadRequest("jumlah harus lebih dari 0")
	}
	if !validFrequencies[p.Frequency] {
		return nil, appErrs.BadRequest("frekuensi tidak valid")
	}
	if p.FrequencyValue <= 0 {
		p.FrequencyValue = 1
	}
	if p.Mode != "reminder" {
		p.Mode = "auto"
	}
	if p.StartDate == "" {
		p.StartDate = time.Now().Format("2006-01-02")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var id string
	err = conn.QueryRowContext(ctx,
		`INSERT INTO fin_recurring
		 (title,type,amount,wallet_id,to_wallet_id,category_id,description,
		  frequency,frequency_value,day_of_month,day_of_week,mode,
		  start_date,end_date,max_occurrences,next_run_date,created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		 RETURNING id`,
		strings.TrimSpace(p.Title), p.Type, p.Amount, p.WalletID,
		p.ToWalletID, p.CategoryID, p.Description,
		p.Frequency, p.FrequencyValue, p.DayOfMonth, p.DayOfWeek, p.Mode,
		p.StartDate, p.EndDate, p.MaxOccurrences, p.StartDate, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &Recurring{
		ID: id, Title: p.Title, Type: p.Type,
		Amount: fmt.Sprintf("%.2f", p.Amount),
		WalletID: p.WalletID, Frequency: p.Frequency,
		FrequencyValue: p.FrequencyValue, Mode: p.Mode,
		StartDate: p.StartDate, NextRunDate: p.StartDate,
		IsActive: true, OccurrencesDone: 0, CreatedAt: time.Now(),
	}, nil
}

//encore:api auth method=DELETE path=/api/v1/finance/recurring/:id
func DeleteRecurring(ctx context.Context, id string) (*OKResponse, error) {
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
	conn.ExecContext(ctx, `UPDATE fin_recurring SET deleted_at=now(), is_active=false WHERE id=$1`, id)
	return &OKResponse{OK: true}, nil
}

// ============================================================
// RECURRING CRON — runs daily at 07:00 WIB (00:00 UTC)
// ============================================================

var _ = cron.NewJob("finance-recurring", cron.JobConfig{
	Title:    "Process Finance Recurring Transactions",
	Schedule: "0 0 * * *", // 00:00 UTC = 07:00 WIB
	Endpoint: ProcessAllRecurring,
})

//encore:api private method=POST path=/api/v1/finance/recurring/process
func ProcessAllRecurring(ctx context.Context) error {
	schemas, err := tenant.ListSchemaNames(ctx)
	if err != nil {
		rlog.Error("recurring: list schemas", "err", err)
		return err
	}

	today := time.Now().Format("2006-01-02")
	for _, schema := range schemas {
		if err := processRecurringForTenant(ctx, schema, today); err != nil {
			rlog.Warn("recurring: tenant failed", "schema", schema, "err", err)
		}
	}
	return nil
}

func processRecurringForTenant(ctx context.Context, schema, today string) error {
	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return err
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `
		SELECT id, type, amount, wallet_id, to_wallet_id, category_id, description,
		       frequency, frequency_value, day_of_month, day_of_week, mode,
		       end_date::text, max_occurrences, occurrences_done, next_run_date::text
		FROM fin_recurring
		WHERE is_active=true AND deleted_at IS NULL AND next_run_date<=$1`, today)
	if err != nil {
		return err
	}
	defer rows.Close()

	type recRow struct {
		id, typ, walletID string
		toWalletID, categoryID, description, endDate sql.NullString
		mode, nextRunDate                            string
		amount                                       float64
		frequency                                    string
		freqVal, dayOfMonth, dayOfWeek               int
		maxOcc, occDone                              sql.NullInt64
	}

	var recs []recRow
	for rows.Next() {
		var r recRow
		var domN, dowN sql.NullInt64
		rows.Scan(&r.id, &r.typ, &r.amount, &r.walletID, &r.toWalletID,
			&r.categoryID, &r.description, &r.frequency, &r.freqVal,
			&domN, &dowN, &r.mode, &r.endDate, &r.maxOcc, &r.occDone, &r.nextRunDate)
		if domN.Valid {
			r.dayOfMonth = int(domN.Int64)
		}
		if dowN.Valid {
			r.dayOfWeek = int(dowN.Int64)
		}
		recs = append(recs, r)
	}
	rows.Close()

	for _, r := range recs {
		// Check end_date
		if r.endDate.Valid && r.endDate.String != "" && r.endDate.String < today {
			conn.ExecContext(ctx, `UPDATE fin_recurring SET is_active=false WHERE id=$1`, r.id)
			continue
		}
		// Check max_occurrences
		if r.maxOcc.Valid && r.occDone.Valid && r.occDone.Int64 >= r.maxOcc.Int64 {
			conn.ExecContext(ctx, `UPDATE fin_recurring SET is_active=false WHERE id=$1`, r.id)
			continue
		}

		var logStatus, errMsg, txnID string
		if r.mode == "auto" {
			// Create transaction
			var newID string
			err := conn.QueryRowContext(ctx,
				`INSERT INTO fin_transaction
				 (type,amount,currency,wallet_id,to_wallet_id,category_id,description,
				  transaction_date,status,tags,recurring_id,created_by)
				 VALUES ($1,$2,'IDR',$3,$4,$5,$6,$7,'approved','{}','00000000-0000-0000-0000-000000000000',$8)
				 RETURNING id`,
				r.typ, r.amount, r.walletID, nullStr(r.toWalletID), nullStr(r.categoryID),
				nullStr(r.description), today, r.id,
			).Scan(&newID)
			if err != nil {
				logStatus = "failed"
				errMsg = err.Error()
				rlog.Warn("recurring txn failed", "id", r.id, "err", err)
			} else {
				logStatus = "success"
				txnID = newID
				refreshWalletBalance(ctx, conn, r.walletID)
				if r.toWalletID.Valid {
					refreshWalletBalance(ctx, conn, r.toWalletID.String)
				}
			}
		} else {
			// reminder mode — just log, notification handled by notif service
			logStatus = "reminded"
		}

		// Log
		conn.ExecContext(ctx,
			`INSERT INTO fin_recurring_log (recurring_id, run_date, status, error_msg, txn_id)
			 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,'')::uuid)`,
			r.id, today, logStatus, errMsg, txnID)

		// Update next_run_date and occurrences_done
		nextRun := calcNextRunDate(today, r.frequency, r.freqVal, r.dayOfMonth, r.dayOfWeek)
		conn.ExecContext(ctx,
			`UPDATE fin_recurring SET next_run_date=$1, occurrences_done=occurrences_done+1, updated_at=now() WHERE id=$2`,
			nextRun, r.id)

		// Pause after 3 consecutive failures
		var failCount int
		conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM fin_recurring_log WHERE recurring_id=$1 AND status='failed' AND run_date>=(CURRENT_DATE-3)`, r.id,
		).Scan(&failCount)
		if failCount >= 3 {
			conn.ExecContext(ctx, `UPDATE fin_recurring SET is_active=false WHERE id=$1`, r.id)
			rlog.Warn("recurring paused after 3 failures", "id", r.id)
		}
	}
	return nil
}

func nullStr(n sql.NullString) interface{} {
	if n.Valid {
		return n.String
	}
	return nil
}

func calcNextRunDate(from, frequency string, value, dayOfMonth, dayOfWeek int) string {
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		return time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	}
	switch frequency {
	case "daily":
		return t.AddDate(0, 0, value).Format("2006-01-02")
	case "weekly":
		return t.AddDate(0, 0, 7*value).Format("2006-01-02")
	case "monthly":
		next := t.AddDate(0, value, 0)
		if dayOfMonth > 0 && dayOfMonth <= 28 {
			next = time.Date(next.Year(), next.Month(), dayOfMonth, 0, 0, 0, 0, time.UTC)
		}
		return next.Format("2006-01-02")
	case "yearly":
		return t.AddDate(value, 0, 0).Format("2006-01-02")
	}
	return t.AddDate(0, 1, 0).Format("2006-01-02")
}

// Suppress unused
var _ = strings.TrimSpace
