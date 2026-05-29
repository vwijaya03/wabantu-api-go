package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

type CloneRecurringToBillingParams struct {
	RecurringIDs []string `json:"recurringIds"`
}

type CloneRecurringSkipped struct {
	RecurringID string `json:"recurringId"`
	Title       string `json:"title"`
	Reason      string `json:"reason"`
}

type CloneRecurringToBillingResponse struct {
	Created []ChecklistTemplate       `json:"created"`
	Skipped []CloneRecurringSkipped   `json:"skipped"`
}

func recurringDueFields(nextRun string, dayOfMonth *int) (anchor string, dom int) {
	dom = 1
	if dayOfMonth != nil && *dayOfMonth >= 1 && *dayOfMonth <= 31 {
		dom = *dayOfMonth
	}
	if strings.TrimSpace(nextRun) != "" {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(nextRun)); err == nil {
			if dayOfMonth == nil || *dayOfMonth < 1 {
				dom = t.Day()
			}
			y, m := t.Year(), t.Month()
			return synthesizeDueAnchor(y, m, dom), dom
		}
	}
	now := time.Now().UTC()
	return synthesizeDueAnchor(now.Year(), now.Month(), dom), dom
}

func billingTemplateExists(ctx context.Context, conn *sql.Conn, title string) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM fin_checklist_template
		  WHERE is_active = true AND frequency = 'monthly'
		    AND lower(trim(title)) = lower(trim($1))
		)`, title).Scan(&exists)
	return exists, err
}

//encore:api auth method=POST path=/api/v1/finance/checklist/clone-from-recurring
func CloneRecurringToBilling(ctx context.Context, p *CloneRecurringToBillingParams) (*CloneRecurringToBillingResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil || len(p.RecurringIDs) == 0 {
		return nil, appErrs.BadRequest("pilih minimal satu transaksi otomatis")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	resp := &CloneRecurringToBillingResponse{
		Created: []ChecklistTemplate{},
		Skipped: []CloneRecurringSkipped{},
	}

	seen := make(map[string]struct{}, len(p.RecurringIDs))
	for _, id := range p.RecurringIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}

		var title, typ, freq, nextRun string
		var amount float64
		var walletID string
		var categoryID, description sql.NullString
		var dayOfMonth sql.NullInt64
		var isActive bool

		err := conn.QueryRowContext(ctx, `
			SELECT title, type, frequency, amount, wallet_id, category_id, description,
			       day_of_month, next_run_date::text, is_active
			FROM fin_recurring
			WHERE id = $1 AND deleted_at IS NULL`, id,
		).Scan(&title, &typ, &freq, &amount, &walletID, &categoryID, &description,
			&dayOfMonth, &nextRun, &isActive)
		if err == sql.ErrNoRows {
			resp.Skipped = append(resp.Skipped, CloneRecurringSkipped{
				RecurringID: id, Title: "?", Reason: "tidak ditemukan",
			})
			continue
		}
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}

		skip := func(reason string) {
			resp.Skipped = append(resp.Skipped, CloneRecurringSkipped{
				RecurringID: id, Title: title, Reason: reason,
			})
		}

		if !isActive {
			skip("transaksi otomatis tidak aktif")
			continue
		}
		if freq != "monthly" {
			skip("hanya frekuensi bulanan yang bisa di-clone")
			continue
		}
		if typ != "expense" {
			skip("hanya pengeluaran yang bisa jadi tagihan bulanan")
			continue
		}
		if amount <= 0 {
			skip("nominal tidak valid")
			continue
		}
		if strings.TrimSpace(title) == "" {
			skip("judul kosong")
			continue
		}

		exists, err := billingTemplateExists(ctx, conn, title)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if exists {
			skip("sudah ada di tagihan bulanan (judul sama)")
			continue
		}

		var domPtr *int
		if dayOfMonth.Valid {
			v := int(dayOfMonth.Int64)
			domPtr = &v
		}
		anchor, dom := recurringDueFields(nextRun, domPtr)

		var catPtr, walletPtr *string
		if categoryID.Valid && categoryID.String != "" {
			c := categoryID.String
			catPtr = &c
		}
		if walletID != "" {
			walletPtr = &walletID
		}
		var descPtr *string
		if description.Valid && strings.TrimSpace(description.String) != "" {
			d := description.String
			descPtr = &d
		} else {
			d := fmt.Sprintf("Diclone dari transaksi otomatis: %s", title)
			descPtr = &d
		}

		var tplID string
		err = conn.QueryRowContext(ctx, `
			INSERT INTO fin_checklist_template
			 (title, description, amount_hint, category_id, wallet_id, frequency, day_of_month, due_anchor_date, display_order, created_by)
			 VALUES ($1,$2,$3,$4,$5,'monthly',$6,$7,0,$8)
			 RETURNING id`,
			strings.TrimSpace(title), descPtr, amount, catPtr, walletPtr, dom, anchor, u.AccountID,
		).Scan(&tplID)
		if err != nil {
			return nil, appErrs.Internal("gagal clone ke tagihan bulanan: " + err.Error())
		}

		t := ChecklistTemplate{
			ID: tplID, Title: title, Frequency: "monthly",
			IsActive: true, CreatedAt: time.Now(),
		}
		s := fmt.Sprintf("%.2f", amount)
		t.AmountHint = &s
		t.DayOfMonth = &dom
		t.DueAnchorDate = &anchor
		if catPtr != nil {
			t.CategoryID = catPtr
		}
		if walletPtr != nil {
			t.WalletID = walletPtr
		}
		resp.Created = append(resp.Created, t)
	}

	if len(resp.Created) == 0 && len(resp.Skipped) == 0 {
		return nil, appErrs.BadRequest("tidak ada item yang dipilih")
	}

	return resp, nil
}
