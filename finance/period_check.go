package finance

import (
	"context"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
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
	conn, err := tenant.TenantConn(ctx, tenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()
	return ensurePeriodUnlocked(ctx, conn, walletPeriod(dateStr))
}
