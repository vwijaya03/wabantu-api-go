package tenantschema

import (
	"context"
	"database/sql"
	"strings"
)

// TableExists reports whether a named table exists in the given tenant schema.
func TableExists(ctx context.Context, q any, schema, table string) (bool, error) {
	return scanExists(ctx, Q(q), `
		SELECT EXISTS (
		  SELECT 1
		  FROM pg_catalog.pg_class c
		  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		  WHERE n.nspname = $1
		    AND c.relname = $2
		    AND c.relkind IN ('r', 'p', 'v', 'm')
		)`, schema, table)
}

// ColumnExists reports whether a column exists on a table in the given schema.
func ColumnExists(ctx context.Context, q any, schema, table, column string) (bool, error) {
	return scanExists(ctx, Q(q), `
		SELECT EXISTS (
		  SELECT 1
		  FROM pg_catalog.pg_attribute a
		  JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		  WHERE n.nspname = $1
		    AND c.relname = $2
		    AND a.attname = $3
		    AND a.attnum > 0
		    AND NOT a.attisdropped
		)`, schema, table, column)
}

// IndexExists reports whether a named index exists in the given schema.
func IndexExists(ctx context.Context, q any, schema, indexName string) (bool, error) {
	return scanExists(ctx, Q(q), `
		SELECT EXISTS (
		  SELECT 1
		  FROM pg_catalog.pg_class c
		  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		  WHERE n.nspname = $1
		    AND c.relname = $2
		    AND c.relkind IN ('i', 'I')
		)`, schema, indexName)
}

// ContactRuntimeReady — inbox + pricing contact columns.
func ContactRuntimeReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, col := range []string{"status", "price_type_id", "birth_date"} {
		ok, err := ColumnExists(ctx, q, schema, "contact", col)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// PricingReady — multi-price catalog tables.
func PricingReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, t := range []string{"business_price_type", "business_catalog_item_price"} {
		ok, err := TableExists(ctx, q, schema, t)
		if err != nil || !ok {
			return false, err
		}
	}
	return ContactRuntimeReady(ctx, q, schema)
}

// CatalogIndexReady — partial unique index on catalog SKU.
func CatalogIndexReady(ctx context.Context, q any, schema string) (bool, error) {
	return IndexExists(ctx, q, schema, "idx_catalog_source_code")
}

// FinanceModuleReady — finance tables through latest patch.
func FinanceModuleReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, t := range []string{
		"fin_wallet", "fin_transaction", "fin_report_job",
		"fin_category", "fin_transaction_type",
	} {
		ok, err := TableExists(ctx, q, schema, t)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := ColumnExists(ctx, q, schema, "fin_asset", "unit_multiplier")
	if err != nil || !ok {
		return false, err
	}
	ok, err = ColumnExists(ctx, q, schema, "fin_checklist_template", "due_anchor_date")
	return ok, err
}

// EventsModuleReady — events module tables through latest patch.
func EventsModuleReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, t := range []string{"evt_event", "evt_patient", "evt_staff_roster", "evt_export_job"} {
		ok, err := TableExists(ctx, q, schema, t)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := ColumnExists(ctx, q, schema, "evt_patient", "contact_id")
	if err != nil || !ok {
		return false, err
	}
	return ColumnExists(ctx, q, schema, "evt_event_person", "counts_toward_meals")
}

// KnowledgeBaseReady — Q/A table for AI knowledge base module.
func KnowledgeBaseReady(ctx context.Context, q any, schema string) (bool, error) {
	return TableExists(ctx, q, schema, "knowledge_base_entry")
}

// TenantPatchReady — schema_patch.go fully applied (branches, workflow, indexes).
func TenantPatchReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, t := range []string{"branch", "workflow_rule"} {
		ok, err := TableExists(ctx, q, schema, t)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := ColumnExists(ctx, q, schema, "contact", "status")
	if err != nil || !ok {
		return false, err
	}
	ok, err = IndexExists(ctx, q, schema, "idx_contact_status_updated")
	if err != nil || !ok {
		return false, err
	}
	return IndexExists(ctx, q, schema, "idx_catalog_source_code")
}

// OrderPaymentProofPatchReady — Fase 2 payment proof columns on order + business_profile.
func OrderPaymentProofPatchReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	ok, err := ColumnExists(ctx, q, schema, "order", "payment_status")
	if err != nil || !ok {
		return false, err
	}
	ok, err = ColumnExists(ctx, q, schema, "business_profile", "payment_verification_mode")
	if err != nil || !ok {
		return false, err
	}
	return IndexExists(ctx, q, schema, "idx_order_payment_status")
}

// OrderIncomePatchReady — order income wallet column (+ dedup index when finance exists).
func OrderIncomePatchReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	ok, err := ColumnExists(ctx, q, schema, "order", "income_wallet_id")
	if err != nil || !ok {
		return false, err
	}
	finExists, err := TableExists(ctx, q, schema, "fin_transaction")
	if err != nil || !finExists {
		return true, nil
	}
	return IndexExists(ctx, q, schema, "idx_fin_txn_order_income_ref")
}

// PIIReady — encrypted PII columns present (contact + lead).
func PIIReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, col := range []string{"phone_number_enc", "phone_number_idx"} {
		ok, err := ColumnExists(ctx, q, schema, "contact", col)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := ColumnExists(ctx, q, schema, "lead", "phone_number_enc")
	return ok, err
}

// InventoryModuleReady — inventory/HPP module tables present.
func InventoryModuleReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	for _, t := range []string{
		"inv_setting", "inv_warehouse", "inv_sku",
		"inv_cost_layer", "inv_stock_balance", "inv_stock_movement",
		"inv_bundle_component",
		"inv_document_sequence", "pur_purchase_order", "pur_purchase_order_line",
		"pur_bill", "pur_bill_line",
		"inv_invoice", "inv_invoice_line", "inv_sales_return", "inv_sales_return_line",
		"inv_stock_transaction", "inv_stock_transaction_line",
	} {
		ok, err := TableExists(ctx, q, schema, t)
		if err != nil || !ok {
			return false, err
		}
	}
	return ColumnExists(ctx, q, schema, "inv_setting", "purchase_posts_expense")
}

// CloudTenantReady — migrated / fully provisioned tenant (skip all runtime DDL).
func CloudTenantReady(ctx context.Context, q any, schema string) (bool, error) {
	q = Q(q)
	checks := []func(context.Context, any, string) (bool, error){
		TenantPatchReady,
		PricingReady,
		KnowledgeBaseReady,
		FinanceModuleReady,
		EventsModuleReady,
		PIIReady,
		OrderIncomePatchReady,
		OrderPaymentProofPatchReady,
		InventoryModuleReady,
	}
	for _, fn := range checks {
		ok, err := fn(ctx, q, schema)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Legacy conn-based helpers for DDL paths that still use search_path sessions.

func schemaFromConn(ctx context.Context, conn *sql.Conn) (string, error) {
	var raw string
	if err := conn.QueryRowContext(ctx, `SELECT current_setting('search_path')`).Scan(&raw); err != nil {
		return "", err
	}
	part := raw
	if idx := strings.IndexByte(raw, ','); idx >= 0 {
		part = raw[:idx]
	}
	return trimQuotes(part), nil
}

// TableExistsConn reports table presence using search_path on conn (DDL sessions only).
func TableExistsConn(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return TableExists(ctx, conn, schema, table)
}

// ColumnExistsConn reports column presence using search_path on conn (DDL sessions only).
func ColumnExistsConn(ctx context.Context, conn *sql.Conn, table, column string) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return ColumnExists(ctx, conn, schema, table, column)
}

// ContactRuntimeReadyConn is for DDL sessions that use search_path.
func ContactRuntimeReadyConn(ctx context.Context, conn *sql.Conn) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return ContactRuntimeReady(ctx, conn, schema)
}

// PricingReadyConn is for DDL sessions that use search_path.
func PricingReadyConn(ctx context.Context, conn *sql.Conn) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return PricingReady(ctx, conn, schema)
}

// FinanceModuleReadyConn is for DDL sessions that use search_path.
func FinanceModuleReadyConn(ctx context.Context, conn *sql.Conn) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return FinanceModuleReady(ctx, conn, schema)
}

// CatalogIndexReadyConn is for DDL sessions that use search_path.
func CatalogIndexReadyConn(ctx context.Context, conn *sql.Conn) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return CatalogIndexReady(ctx, conn, schema)
}

// CloudTenantReadyConn is for DDL sessions that use search_path.
func CloudTenantReadyConn(ctx context.Context, conn *sql.Conn) (bool, error) {
	schema, err := schemaFromConn(ctx, conn)
	if err != nil {
		return false, err
	}
	return CloudTenantReady(ctx, conn, schema)
}

func trimQuotes(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"`)
}
