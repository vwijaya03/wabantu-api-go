// Package tenantschema introspects tenant Postgres schemas so runtime code can
// skip DDL on Encore Cloud (app role has DML only, not CREATE/ALTER/DROP).
package tenantschema

import (
	"context"
	"database/sql"
)

func tableExists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.tables
		  WHERE table_schema = current_schema() AND table_name = $1
		)`, table).Scan(&exists)
	return exists, err
}

func columnExists(ctx context.Context, conn *sql.Conn, table, column string) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.columns
		  WHERE table_schema = current_schema()
		    AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	return exists, err
}

// IndexExists reports whether a named index exists in the current schema.
func IndexExists(ctx context.Context, conn *sql.Conn, indexName string) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM pg_indexes
		  WHERE schemaname = current_schema() AND indexname = $1
		)`, indexName).Scan(&exists)
	return exists, err
}

// ContactRuntimeReady — inbox + pricing contact columns.
func ContactRuntimeReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	for _, col := range []string{"status", "price_type_id", "birth_date"} {
		ok, err := columnExists(ctx, conn, "contact", col)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// PricingReady — multi-price catalog tables.
func PricingReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	for _, t := range []string{"business_price_type", "business_catalog_item_price"} {
		ok, err := tableExists(ctx, conn, t)
		if err != nil || !ok {
			return false, err
		}
	}
	return ContactRuntimeReady(ctx, conn)
}

// CatalogIndexReady — partial unique index on catalog SKU.
func CatalogIndexReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	return IndexExists(ctx, conn, "idx_catalog_source_code")
}

// FinanceModuleReady — finance tables through latest patch.
func FinanceModuleReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	for _, t := range []string{"fin_wallet", "fin_transaction", "fin_report_job"} {
		ok, err := tableExists(ctx, conn, t)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := columnExists(ctx, conn, "fin_asset", "unit_multiplier")
	if err != nil || !ok {
		return false, err
	}
	ok, err = columnExists(ctx, conn, "fin_checklist_template", "due_anchor_date")
	return ok, err
}

// EventsModuleReady — events module tables through latest patch.
func EventsModuleReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	for _, t := range []string{"evt_event", "evt_patient", "evt_staff_roster", "evt_export_job"} {
		ok, err := tableExists(ctx, conn, t)
		if err != nil || !ok {
			return false, err
		}
	}
	return columnExists(ctx, conn, "evt_patient", "contact_id")
}

// TenantPatchReady — schema_patch.go fully applied (branches, workflow, indexes).
func TenantPatchReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	for _, t := range []string{"branch", "workflow_rule"} {
		ok, err := tableExists(ctx, conn, t)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := columnExists(ctx, conn, "contact", "status")
	if err != nil || !ok {
		return false, err
	}
	ok, err = IndexExists(ctx, conn, "idx_contact_status_updated")
	if err != nil || !ok {
		return false, err
	}
	return IndexExists(ctx, conn, "idx_catalog_source_code")
}

// PIIReady — encrypted PII columns present (contact + lead).
func PIIReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	for _, col := range []string{"phone_number_enc", "phone_number_idx"} {
		ok, err := columnExists(ctx, conn, "contact", col)
		if err != nil || !ok {
			return false, err
		}
	}
	ok, err := columnExists(ctx, conn, "lead", "phone_number_enc")
	return ok, err
}

// CloudTenantReady — migrated / fully provisioned tenant (skip all runtime DDL).
func CloudTenantReady(ctx context.Context, conn *sql.Conn) (bool, error) {
	checks := []func(context.Context, *sql.Conn) (bool, error){
		TenantPatchReady,
		PricingReady,
		FinanceModuleReady,
		EventsModuleReady,
		PIIReady,
	}
	for _, fn := range checks {
		ok, err := fn(ctx, conn)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
