package finance

import (
	"context"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// CheckPeriodUnlockedForDate rejects edits/deletes when the finance period for
// the given YYYY-MM-DD date is locked.
func CheckPeriodUnlockedForDate(ctx context.Context, tenantSchema, dateStr string) error {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return CheckCurrentPeriodUnlocked(ctx, tenantSchema)
	}
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		return appErrs.BadRequest("format tanggal harus YYYY-MM-DD")
	}
	sch, err := prepareTenant(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	pool := tenantPool()
	return ensurePeriodUnlocked(ctx, sch, pool, walletPeriod(dateStr))
}
