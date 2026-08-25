package finance

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

// finQuerier is satisfied by *sql.DB and *sql.Tx.
type finQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func prepareTenant(ctx context.Context, schema string) (appdb.SchemaSQL, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.SchemaSQL{}, err
	}
	return appdb.SchemaSQL{Schema: schema}, nil
}

func tenantPool() *sql.DB {
	return db.Stdlib()
}

// tenantTables longest-first so shorter names are not partially matched.
var tenantTables = []string{
	"fin_wallet_balance",
	"fin_transaction_type",
	"fin_checklist_template",
	"fin_checklist_item",
	"fin_approval_setting",
	"fin_recurring_log",
	"fin_asset_price",
	"fin_report_job",
	"fin_period_lock",
	"fin_transaction",
	"fin_recurring",
	"fin_audit_log",
	"fin_category",
	"fin_wallet",
	"fin_budget",
	"fin_asset",
	"business_profile",
}

var (
	orderTableRE = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+"?order"?\b`)
	finTableREs  = make([]*regexp.Regexp, len(tenantTables))
)

func init() {
	for i, table := range tenantTables {
		finTableREs[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(table) + `\b`)
	}
}

func qualSQL(sch appdb.SchemaSQL, sql string) string {
	if strings.Contains(sql, sch.Schema) {
		return sql
	}
	out := sql
	for i, table := range tenantTables {
		out = finTableREs[i].ReplaceAllString(out, sch.T(table))
	}
	out = orderTableRE.ReplaceAllStringFunc(out, func(m string) string {
		parts := strings.Fields(m)
		if len(parts) < 2 {
			return m
		}
		return parts[0] + " " + sch.T("order")
	})
	return out
}

func qrow(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, query string, args ...any) *sql.Row {
	return q.QueryRowContext(ctx, qualSQL(sch, query), args...)
}

func qexec(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, query string, args ...any) (sql.Result, error) {
	return q.ExecContext(ctx, qualSQL(sch, query), args...)
}

func qquery(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, query string, args ...any) (*sql.Rows, error) {
	return q.QueryContext(ctx, qualSQL(sch, query), args...)
}

func qformat(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, format string, args ...any) (*sql.Rows, error) {
	return qquery(ctx, sch, tenantPool(), fmt.Sprintf(format, args...))
}
