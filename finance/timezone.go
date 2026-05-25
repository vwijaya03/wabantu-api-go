package finance

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const defaultFinanceTimezone = "Asia/Jakarta"

func financeLocation(ctx context.Context, conn *sql.Conn) *time.Location {
	var tz string
	if err := conn.QueryRowContext(ctx,
		`SELECT reporting_timezone FROM business_profile ORDER BY created_at ASC LIMIT 1`,
	).Scan(&tz); err != nil {
		tz = defaultFinanceTimezone
	}
	loc, err := time.LoadLocation(strings.TrimSpace(tz))
	if err != nil {
		loc, _ = time.LoadLocation(defaultFinanceTimezone)
	}
	if loc == nil {
		loc = time.FixedZone("WIB", 7*60*60)
	}
	return loc
}

func financeNow(ctx context.Context, conn *sql.Conn) time.Time {
	return time.Now().In(financeLocation(ctx, conn))
}

func financeToday(ctx context.Context, conn *sql.Conn) string {
	return financeNow(ctx, conn).Format("2006-01-02")
}

func financePeriod(ctx context.Context, conn *sql.Conn) string {
	return financeNow(ctx, conn).Format("2006-01")
}

func financePeriods(ctx context.Context, conn *sql.Conn, months int) []string {
	now := financeNow(ctx, conn)
	periods := make([]string, months)
	for i := 0; i < months; i++ {
		periods[i] = now.AddDate(0, -i, 0).Format("2006-01")
	}
	return periods
}
