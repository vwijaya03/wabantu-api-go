package finance

import (
	"context"
	"strings"
	"time"

	appdb "encore.app/wabantu/shared/db"
)

const defaultFinanceTimezone = "Asia/Jakarta"

func financeLocation(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) *time.Location {
	var tz string
	if err := qrow(ctx, sch, q,
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

func financeNow(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) time.Time {
	return time.Now().In(financeLocation(ctx, sch, q))
}

func financeToday(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) string {
	return financeNow(ctx, sch, q).Format("2006-01-02")
}

func financePeriod(ctx context.Context, sch appdb.SchemaSQL, q finQuerier) string {
	return financeNow(ctx, sch, q).Format("2006-01")
}

func financePeriods(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, months int) []string {
	now := financeNow(ctx, sch, q)
	periods := make([]string, months)
	for i := 0; i < months; i++ {
		periods[i] = now.AddDate(0, -i, 0).Format("2006-01")
	}
	return periods
}
