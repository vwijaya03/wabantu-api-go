package ai

import (
	"fmt"
	"time"
)

// ScannedAnomaly is one anomaly row collected during a cron scan.
type ScannedAnomaly struct {
	TenantID     string
	TenantSchema string
	TriageAnomalyEntry
}

// TriageScanResult summarizes a multi-tenant anomaly scan run.
type TriageScanResult struct {
	TenantsScanned int
	TenantsFailed  int
	RowsCollected  int
}

// TriageAnomalyGlobalCap is the max rows collected across all tenants per cron run.
func TriageAnomalyGlobalCap() int {
	return triageAnomalyGlobalCap
}

// TriageScanLimitForTenant returns per-tenant fetch limit given remaining global cap.
func TriageScanLimitForTenant(remainingGlobal int) int {
	return triageScanLimitForTenant(remainingGlobal)
}

func triageScanLimitForTenant(remainingGlobal int) int {
	if remainingGlobal <= 0 {
		return 0
	}
	if triageAnomalyPerTenantLimit < remainingGlobal {
		return triageAnomalyPerTenantLimit
	}
	return remainingGlobal
}

func formatPGInterval(d time.Duration) string {
	secs := int(d.Seconds())
	if secs <= 0 {
		return "0 seconds"
	}
	if secs%3600 == 0 {
		hours := secs / 3600
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%d seconds", secs)
}
