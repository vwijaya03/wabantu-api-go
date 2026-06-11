package observability

import "encore.dev/rlog"

// Info logs a structured info event with consistent field names.
func Info(msg, tenantSchema, accountID string, extra ...any) {
	args := []any{"tenantSchema", tenantSchema, "accountId", accountID}
	args = append(args, extra...)
	rlog.Info(msg, args...)
}

// Warn logs a structured warning event.
func Warn(msg string, extra ...any) {
	rlog.Warn(msg, extra...)
}
