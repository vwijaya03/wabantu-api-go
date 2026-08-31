package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// TenantQuerier is satisfied by *sql.DB, *sql.Conn, *sql.Tx, and TenantScope.
type TenantQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) Scannable
}

// TenantScope runs tenant DML with schema-qualified table names (no SET search_path).
type TenantScope struct {
	Q    TenantQuerier
	Sch  SchemaSQL
	pool *sql.DB // set for pool-backed scopes; enables 08P01 retry on QueryRow/Exec
}

// tenantTableNames longest-first so shorter names are not partially matched.
var tenantTableNames = []string{
	"evt_event_therapy_slot_template",
	"evt_staff_roster_volunteer",
	"evt_staff_roster_therapy",
	"business_catalog_item_price",
	"inv_stock_transaction_line",
	"pur_purchase_order_line",
	"evt_event_assignment",
	"evt_event_volunteer",
	"evt_event_therapy",
	"evt_person_therapy",
	"fin_wallet_balance",
	"fin_transaction_type",
	"fin_checklist_template",
	"fin_checklist_item",
	"fin_approval_setting",
	"fin_recurring_log",
	"inv_bundle_component",
	"inv_document_sequence",
	"inv_stock_transaction",
	"inv_sales_return_line",
	"evt_volunteer_role",
	"evt_staff_roster",
	"evt_export_job",
	"evt_event_person",
	"evt_audit_log",
	"evt_time_slot",
	"fin_asset_price",
	"fin_report_job",
	"fin_period_lock",
	"fin_transaction",
	"fin_recurring",
	"fin_audit_log",
	"fin_category",
	"broadcast_recipient",
	"broadcast_campaign",
	"business_catalog_item",
	"business_price_type",
	"conversation_summary",
	"inv_stock_movement",
	"inv_stock_balance",
	"inv_sales_return",
	"knowledge_base_entry",
	"payment_transaction",
	"pur_purchase_order",
	"inv_invoice_line",
	"pur_bill_line",
	"usage_aggregate",
	"workflow_rule",
	"evt_therapy",
	"evt_patient",
	"evt_event",
	"evt_task",
	"fin_wallet",
	"fin_budget",
	"fin_asset",
	"inv_setting",
	"inv_warehouse",
	"inv_invoice",
	"inv_cost_layer",
	"quota_topup",
	"whatsapp_channel",
	"business_profile",
	"webhook_event",
	"pur_bill",
	"inv_sku",
	"conversation",
	"subscription",
	"usage_event",
	"invoice",
	"contact",
	"message",
	"branch",
	"lead",
}

var (
	// Match quoted or unquoted reserved table name "order" as a whole identifier.
	orderQuotedRE   = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+"order"`)
	orderUnquotedRE = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+order\b`)
	tenantTableRE = make([]*regexp.Regexp, len(tenantTableNames))
)

func init() {
	for i, table := range tenantTableNames {
		tenantTableRE[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(table) + `\b`)
	}
}

// QualifySQL rewrites unqualified tenant table references to "schema"."table".
// String literals (single-quoted) are left untouched so values like author='contact'
// are not mistaken for the contact table.
func QualifySQL(sch SchemaSQL, sql string) string {
	if strings.Contains(sql, sch.Schema) {
		return sql
	}
	return qualifySQLOutsideLiterals(sch, sql)
}

func qualifySQLChunk(sch SchemaSQL, sql string) string {
	out := sql
	for i, table := range tenantTableNames {
		out = tenantTableRE[i].ReplaceAllString(out, sch.T(table))
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

func qualifySQLOutsideLiterals(sch SchemaSQL, sql string) string {
	var b strings.Builder
	b.Grow(len(sql) + 32)
	for i := 0; i < len(sql); {
		if sql[i] != '\'' {
			j := i
			for j < len(sql) && sql[j] != '\'' {
				j++
			}
			b.WriteString(qualifySQLChunk(sch, sql[i:j]))
			i = j
			continue
		}
		b.WriteByte('\'')
		i++
		for i < len(sql) {
			ch := sql[i]
			b.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					b.WriteByte(sql[i+1])
					i += 2
					continue
				}
				i++
				break
			}
			i++
		}
	}
	return b.String()
}

// OpenTenantScope returns a scope for schema-qualified DML on pool (no search_path).
func OpenTenantScope(pool *sql.DB, schema string) TenantScope {
	return TenantScope{Q: AsTenantQuerier(pool), Sch: SchemaSQL{Schema: schema}, pool: pool}
}

// WithQ returns the same scope using a different querier (e.g. *sql.Tx).
func (ts TenantScope) WithQ(q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) TenantScope {
	return TenantScope{Q: AsTenantQuerier(q), Sch: ts.Sch}
}

func (ts TenantScope) T(table string) string {
	return ts.Sch.T(table)
}

func (ts TenantScope) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return ts.Q.ExecContext(ctx, QualifySQL(ts.Sch, query), args...)
}

func (ts TenantScope) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return ts.Q.QueryContext(ctx, QualifySQL(ts.Sch, query), args...)
}

func (ts TenantScope) QueryRowContext(ctx context.Context, query string, args ...any) Scannable {
	return ts.Q.QueryRowContext(ctx, QualifySQL(ts.Sch, query), args...)
}

func (ts TenantScope) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if ts.pool != nil {
		return ts.pool.BeginTx(ctx, opts)
	}
	beginner, ok := ts.Q.(interface {
		BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return nil, fmt.Errorf("db: cannot begin transaction on this querier")
	}
	return beginner.BeginTx(ctx, opts)
}
