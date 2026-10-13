package simulator

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// Scanner is a single result row.
type Scanner interface{ Scan(dest ...any) error }

// RowQuerier runs a single-row query (adapters: SQLDB for *sql.DB, a closure for appdb.TenantScope).
type RowQuerier func(ctx context.Context, query string, args ...any) Scanner

// SQLDB adapts *sql.DB.
func SQLDB(db *sql.DB) RowQuerier {
	return func(ctx context.Context, q string, a ...any) Scanner { return db.QueryRowContext(ctx, q, a...) }
}

// Qualifier maps a table name to its SQL reference (nil = unqualified, search_path scoped).
type Qualifier func(table string) string

// SchemaQualifier returns a Qualifier for an explicit tenant schema ("schema"."table").
func SchemaQualifier(schema string) Qualifier {
	return func(t string) string { return strconv.Quote(schema) + "." + strconv.Quote(t) }
}

// LoadState returns the buyer's quoted price tier and the conversation's latest order.
// Best-effort: errors leave the fields nil.
func LoadState(ctx context.Context, q RowQuerier, qual Qualifier, convoID string) (*PricingView, *OrderView) {
	if qual == nil {
		qual = func(t string) string { return strconv.Quote(t) }
	}
	var pricing *PricingView
	var code, label string
	if err := q(ctx, `
		SELECT t.code, t.label FROM `+qual("conversation")+` c
		JOIN `+qual("contact")+` ct ON ct.id = c.contact_id
		JOIN `+qual("business_price_type")+` t ON t.deleted_at IS NULL AND t.is_active = true
		     AND (t.id = ct.price_type_id OR t.is_default = true)
		WHERE c.id = $1
		ORDER BY (t.id = ct.price_type_id) IS TRUE DESC, t.is_default DESC, t.display_order
		LIMIT 1`, convoID).Scan(&code, &label); err == nil {
		pricing = &PricingView{Tier: code, TierLabel: label}
	}
	var id, status, payStatus string
	var total float64
	var items, meta []byte
	var updated time.Time
	if err := q(ctx, `
		SELECT id::text, status, payment_status, COALESCE(total, 0)::float8, COALESCE(items, '[]'::jsonb),
		       COALESCE(payment_proof_meta, '{}'::jsonb), updated_at
		FROM `+qual("order")+` WHERE conversation_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, convoID).Scan(&id, &status, &payStatus, &total, &items, &meta, &updated); err != nil {
		return pricing, nil
	}
	v := BuildOrderView(id, status, payStatus, total, items, meta, updated.UTC().Format(time.RFC3339Nano))
	return pricing, &v
}
