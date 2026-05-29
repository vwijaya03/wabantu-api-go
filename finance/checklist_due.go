package finance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// parseMonthlyDueDateInput validates YYYY-MM-DD and returns anchor + day-of-month (1–31).
func parseMonthlyDueDateInput(raw string) (anchor string, dayOfMonth int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, appErrs.BadRequest("tanggal jatuh tempo wajib diisi (YYYY-MM-DD)")
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return "", 0, appErrs.BadRequest("format tanggal jatuh tempo harus YYYY-MM-DD")
	}
	if t.Year() < 2000 || t.Year() > 2100 {
		return "", 0, appErrs.BadRequest("tahun tanggal jatuh tempo tidak valid")
	}
	return t.Format("2006-01-02"), t.Day(), nil
}

// synthesizeDueAnchor builds YYYY-MM-DD from day-of-month using the given calendar month.
func synthesizeDueAnchor(year int, month time.Month, dayOfMonth int) string {
	if dayOfMonth < 1 {
		dayOfMonth = 1
	}
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if dayOfMonth > last {
		dayOfMonth = last
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, int(month), dayOfMonth)
}

func attachDueAnchorDate(t *ChecklistTemplate, anchor sql.NullTime, dom sql.NullInt64, ref time.Time) {
	if anchor.Valid {
		s := anchor.Time.Format("2006-01-02")
		t.DueAnchorDate = &s
		day := anchor.Time.Day()
		t.DayOfMonth = &day
		return
	}
	if dom.Valid {
		v := int(dom.Int64)
		t.DayOfMonth = &v
		s := synthesizeDueAnchor(ref.Year(), ref.Month(), v)
		t.DueAnchorDate = &s
	}
}

// reconcilePendingChecklistItems removes editable future items so ensureMonthlyBillingItems can recreate due dates.
func reconcilePendingChecklistItems(ctx context.Context, conn *sql.Conn, templateID, monthStart string) error {
	_, err := conn.ExecContext(ctx, `
		DELETE FROM fin_checklist_item
		WHERE template_id = $1
		  AND status = 'pending'
		  AND transaction_id IS NULL
		  AND due_date >= $2::date`,
		templateID, monthStart)
	return err
}

// resolveMonthlyDueFields prefers dueDate (YYYY-MM-DD); falls back to dayOfMonth for legacy clients.
func resolveMonthlyDueFields(dueDate string, dayOfMonth *int) (anchor string, dom int, err error) {
	if strings.TrimSpace(dueDate) != "" {
		return parseMonthlyDueDateInput(dueDate)
	}
	if dayOfMonth == nil || *dayOfMonth < 1 || *dayOfMonth > 31 {
		return "", 0, appErrs.BadRequest("tagihan bulanan membutuhkan tanggal jatuh tempo")
	}
	now := time.Now().UTC()
	anchor = synthesizeDueAnchor(now.Year(), now.Month(), *dayOfMonth)
	return anchor, *dayOfMonth, nil
}

func currentMonthStart(ctx context.Context, conn *sql.Conn) string {
	today := financeToday(ctx, conn)
	if len(today) >= 7 {
		return today[:7] + "-01"
	}
	now := time.Now().UTC()
	return fmt.Sprintf("%04d-%02d-01", now.Year(), int(now.Month()))
}
