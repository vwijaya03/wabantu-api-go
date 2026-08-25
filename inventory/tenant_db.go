package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

func prepareTenant(ctx context.Context, schema string) (appdb.SchemaSQL, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.SchemaSQL{}, err
	}
	return appdb.SchemaSQL{Schema: schema}, nil
}

func tenantDB() *sql.DB {
	return tenant.DataDB.Stdlib()
}

// tenantTables longest-first so shorter names are not partially matched.
var tenantTables = []string{
	"inv_stock_transaction_line",
	"pur_purchase_order_line",
	"inv_bundle_component",
	"inv_document_sequence",
	"inv_stock_transaction",
	"inv_sales_return_line",
	"business_catalog_item",
	"business_profile",
	"inv_stock_movement",
	"inv_stock_balance",
	"inv_sales_return",
	"pur_purchase_order",
	"inv_cost_layer",
	"inv_invoice_line",
	"pur_bill_line",
	"inv_setting",
	"inv_warehouse",
	"inv_invoice",
	"pur_bill",
	"inv_sku",
	"contact",
}

var (
	orderQuotedRE   = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+"order"`)
	orderUnquotedRE = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+order\b`)
	invTableREs     = make([]*regexp.Regexp, len(tenantTables))
)

func init() {
	for i, table := range tenantTables {
		invTableREs[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(table) + `\b`)
	}
}

func qualSQL(sch appdb.SchemaSQL, sql string) string {
	if strings.Contains(sql, sch.Schema) {
		return sql
	}
	out := sql
	for i, table := range tenantTables {
		out = invTableREs[i].ReplaceAllString(out, sch.T(table))
	}
	replaceOrder := func(m string) string {
		parts := strings.Fields(m)
		if len(parts) < 2 {
			return m
		}
		return parts[0] + " " + sch.T("order")
	}
	out = orderQuotedRE.ReplaceAllStringFunc(out, replaceOrder)
	out = orderUnquotedRE.ReplaceAllStringFunc(out, replaceOrder)
	return out
}

func qrow(ctx context.Context, sch appdb.SchemaSQL, q querier, query string, args ...any) appdb.Scannable {
	qualified := qualSQL(sch, query)
	if pool, ok := q.(*sql.DB); ok {
		return appdb.PoolQueryRow(ctx, pool, qualified, args...)
	}
	return q.QueryRowContext(ctx, qualified, args...)
}

func qexec(ctx context.Context, sch appdb.SchemaSQL, q querier, query string, args ...any) (sql.Result, error) {
	qualified := qualSQL(sch, query)
	if pool, ok := q.(*sql.DB); ok {
		return appdb.ExecPool(ctx, pool, qualified, args...)
	}
	return q.ExecContext(ctx, qualified, args...)
}

func qquery(ctx context.Context, sch appdb.SchemaSQL, q querier, query string, args ...any) (*sql.Rows, error) {
	qualified := qualSQL(sch, query)
	if pool, ok := q.(*sql.DB); ok {
		return appdb.QueryContextPool(ctx, pool, qualified, args...)
	}
	return q.QueryContext(ctx, qualified, args...)
}

func qformat(ctx context.Context, sch appdb.SchemaSQL, q querier, format string, args ...any) (*sql.Rows, error) {
	return qquery(ctx, sch, q, fmt.Sprintf(format, args...))
}
