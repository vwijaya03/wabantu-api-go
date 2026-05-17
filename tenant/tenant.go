package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

// ---------- schema-name validation ----------

var schemaNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// ---------- types ----------

type TenantRow struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CompanyRow struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenantId"`
	SchemaName string `json:"schemaName"`
}

// ---------- request / response ----------

type CreateTenantParams struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type CreateTenantResponse struct {
	Tenant  TenantRow  `json:"tenant"`
	Company CompanyRow `json:"company"`
}

type ListTenantsResponse struct {
	Tenants []TenantRow `json:"tenants"`
}

// ---------- API endpoints ----------

// CreateTenant provisions a new tenant: system rows + schema + DDL.
//
//encore:api private method=POST path=/internal/tenant/create
func CreateTenant(ctx context.Context, p *CreateTenantParams) (*CreateTenantResponse, error) {
	schemaName := fmt.Sprintf("t_%s", p.Slug)
	if len(schemaName) > 63 {
		schemaName = schemaName[:63]
	}

	tx, err := DB.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal("begin tx: " + err.Error())
	}
	defer tx.Rollback()

	var t TenantRow
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tenant (slug, name, status)
		 VALUES ($1, $2, 'active')
		 RETURNING id, slug, name, status, created_at, updated_at`,
		p.Slug, p.Name,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, appErrs.Internal("insert tenant: " + err.Error())
	}

	var c CompanyRow
	err = tx.QueryRowContext(ctx,
		`INSERT INTO tenant_company (tenant_id, schema_name)
		 VALUES ($1, $2)
		 RETURNING id, tenant_id, schema_name`,
		t.ID, schemaName,
	).Scan(&c.ID, &c.TenantID, &c.SchemaName)
	if err != nil {
		return nil, appErrs.Internal("insert company: " + err.Error())
	}

	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal("commit tx: " + err.Error())
	}

	if err := RunTenantDDL(ctx, schemaName); err != nil {
		DB.Exec(ctx, "DELETE FROM tenant_company WHERE id = $1", c.ID)
		DB.Exec(ctx, "DELETE FROM tenant WHERE id = $1", t.ID)
		return nil, appErrs.Internal("bootstrap schema: " + err.Error())
	}

	return &CreateTenantResponse{Tenant: t, Company: c}, nil
}

// GetTenantByID returns a single tenant by primary key.
//
//encore:api private method=GET path=/internal/tenant/by-id/:id
func GetTenantByID(ctx context.Context, id string) (*TenantRow, error) {
	var t TenantRow
	err := DB.QueryRow(ctx,
		`SELECT id, slug, name, status, created_at, updated_at
		 FROM tenant WHERE id = $1 AND deleted_at IS NULL`, id,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("tenant not found")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &t, nil
}

// GetTenantBySlug returns a single tenant by unique slug.
//
//encore:api private method=GET path=/internal/tenant/by-slug/:slug
func GetTenantBySlug(ctx context.Context, slug string) (*TenantRow, error) {
	var t TenantRow
	err := DB.QueryRow(ctx,
		`SELECT id, slug, name, status, created_at, updated_at
		 FROM tenant WHERE slug = $1 AND deleted_at IS NULL`, slug,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("tenant not found")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &t, nil
}

// ListTenants returns all active (non-deleted) tenants.
//
//encore:api private method=GET path=/internal/tenant/list
func ListTenants(ctx context.Context) (*ListTenantsResponse, error) {
	rows, err := DB.Query(ctx,
		`SELECT id, slug, name, status, created_at, updated_at
		 FROM tenant WHERE deleted_at IS NULL
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var tenants []TenantRow
	for rows.Next() {
		var t TenantRow
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		tenants = append(tenants, t)
	}
	if tenants == nil {
		tenants = []TenantRow{}
	}
	return &ListTenantsResponse{Tenants: tenants}, nil
}

// ---------- exported helpers ----------

// TenantConn returns a *sql.Conn with search_path set to the given tenant
// schema. The caller MUST close the connection when done.
func TenantConn(ctx context.Context, schemaName string) (*sql.Conn, error) {
	if !schemaNameRe.MatchString(schemaName) {
		return nil, fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := DB.Stdlib().Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn: %w", err)
	}
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s"`, schemaName))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return conn, nil
}

// FindUniqueSlug appends _N (or a timestamp) to base until no tenant row
// has that slug.
func FindUniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 0; i <= 50; i++ {
		if i > 0 {
			candidate = fmt.Sprintf("%s_%d", base, i)
		}
		var exists bool
		err := DB.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM tenant WHERE slug = $1)", candidate,
		).Scan(&exists)
		if err != nil {
			return "", fmt.Errorf("slug check: %w", err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return fmt.Sprintf("%s_%d", base, time.Now().UnixMilli()), nil
}

// RunTenantDDL creates the tenant schema and all tenant-scoped tables.
func RunTenantDDL(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}

	conn, err := DB.Stdlib().Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schemaName)); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s"`, schemaName)); err != nil {
		return fmt.Errorf("set search_path: %w", err)
	}
	if _, err := conn.ExecContext(ctx, tenantDDL); err != nil {
		return fmt.Errorf("run DDL: %w", err)
	}
	return nil
}

// ---------- tenant-schema DDL ----------

const tenantDDL = `
-- business_profile: single-row per tenant, AI context
CREATE TABLE IF NOT EXISTS business_profile (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_name       TEXT NOT NULL,
    description         TEXT,
    address             TEXT,
    opening_hours       TEXT,
    products_services   TEXT,
    base_pricing        TEXT,
    delivery_area       TEXT,
    greeting_template   TEXT,
    tone                VARCHAR(20)  NOT NULL DEFAULT 'friendly',
    ai_enabled          BOOLEAN      NOT NULL DEFAULT true,
    reporting_timezone  VARCHAR(100) NOT NULL DEFAULT 'Asia/Jakarta',
    catalog_website_url TEXT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- business_catalog_item: product/service catalog
CREATE TABLE IF NOT EXISTS business_catalog_item (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_code       VARCHAR(64)  NOT NULL,
    name                TEXT         NOT NULL,
    description         TEXT,
    sell_price          DECIMAL(15,4),
    sell_unit           VARCHAR(40),
    is_active           BOOLEAN      NOT NULL DEFAULT true,
    source              VARCHAR(20)  NOT NULL DEFAULT 'manual',
    barcode             VARCHAR(128),
    external_updated_at TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ,
    deleted_by          UUID
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_source_code
    ON business_catalog_item(source, external_code);

-- knowledge_base_entry: Q/A pairs for AI
CREATE TABLE IF NOT EXISTS knowledge_base_entry (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question    VARCHAR(500) NOT NULL,
    answer      TEXT         NOT NULL,
    category    VARCHAR(60),
    is_active   BOOLEAN      NOT NULL DEFAULT true,
    source      VARCHAR(20)  NOT NULL DEFAULT 'manual',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID
);
CREATE INDEX IF NOT EXISTS idx_kb_entry_category
    ON knowledge_base_entry(category);

-- whatsapp_channel: connected WA numbers
CREATE TABLE IF NOT EXISTS whatsapp_channel (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider             VARCHAR(20)  NOT NULL DEFAULT 'meta_cloud',
    display_name         VARCHAR(120) NOT NULL,
    phone_number         VARCHAR(32)  NOT NULL,
    meta_phone_number_id VARCHAR(64),
    meta_waba_id         VARCHAR(64),
    meta_app_id          VARCHAR(64),
    meta_app_secret      TEXT,
    access_token         TEXT,
    status               VARCHAR(20)  NOT NULL DEFAULT 'disconnected',
    last_error           TEXT,
    connected_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_wa_channel_phone
    ON whatsapp_channel(phone_number);

-- contact
CREATE TABLE IF NOT EXISTS contact (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number  VARCHAR(32)  UNIQUE NOT NULL,
    display_name  VARCHAR(200),
    notes         TEXT,
    tags          JSONB        NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    deleted_by    UUID
);

-- conversation
CREATE TABLE IF NOT EXISTS conversation (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id           UUID         NOT NULL,
    contact_id           UUID         NOT NULL,
    status               VARCHAR(20)  NOT NULL DEFAULT 'open',
    ai_handled           BOOLEAN      NOT NULL DEFAULT true,
    last_message_at      TIMESTAMPTZ,
    last_message_preview VARCHAR(280),
    unread_count         INTEGER      NOT NULL DEFAULT 0,
    assigned_to_user_id  UUID,
    assigned_to_name     VARCHAR(120),
    handoff_reason       VARCHAR(280),
    ai_paused_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at           TIMESTAMPTZ,
    deleted_by           UUID,
    UNIQUE(channel_id, contact_id)
);

-- message
CREATE TABLE IF NOT EXISTS message (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID         NOT NULL,
    external_id     VARCHAR(128) UNIQUE,
    direction       VARCHAR(10)  NOT NULL,
    author          VARCHAR(10)  NOT NULL,
    type            VARCHAR(20)  NOT NULL DEFAULT 'text',
    body            TEXT,
    metadata        JSONB        NOT NULL DEFAULT '{}',
    status          VARCHAR(20)  NOT NULL DEFAULT 'sent',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_message_conv_created
    ON message(conversation_id, created_at);

-- lead
CREATE TABLE IF NOT EXISTS lead (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id       UUID,
    conversation_id  UUID,
    phone_number     VARCHAR(32)  NOT NULL,
    name             VARCHAR(120),
    product_interest VARCHAR(200),
    budget           VARCHAR(120),
    location         VARCHAR(120),
    status           VARCHAR(20)  NOT NULL DEFAULT 'new',
    notes            TEXT,
    metadata         JSONB        NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    deleted_by       UUID
);
CREATE INDEX IF NOT EXISTS idx_lead_phone
    ON lead(phone_number);
CREATE INDEX IF NOT EXISTS idx_lead_status_created
    ON lead(status, created_at);

-- subscription
CREATE TABLE IF NOT EXISTS subscription (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_code     VARCHAR(32)  NOT NULL DEFAULT 'starter',
    plan_name     VARCHAR(80)  NOT NULL DEFAULT 'Starter',
    is_trial      BOOLEAN      NOT NULL DEFAULT true,
    trial_ends_at TIMESTAMPTZ,
    status        VARCHAR(20)  NOT NULL DEFAULT 'active',
    provider      VARCHAR(20),
    provider_ref  VARCHAR(120),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- invoice
CREATE TABLE IF NOT EXISTS invoice (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_no  VARCHAR(50) UNIQUE NOT NULL,
    plan_code   VARCHAR(32) NOT NULL,
    plan_name   VARCHAR(80) NOT NULL,
    amount_idr  INTEGER     NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'issued',
    issued_at   TIMESTAMPTZ NOT NULL,
    paid_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_invoice_issued ON invoice(issued_at);

-- usage_event
CREATE TABLE IF NOT EXISTS usage_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type  VARCHAR(60)  NOT NULL,
    quantity    INTEGER      NOT NULL DEFAULT 1,
    metadata    JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_usage_event_type_created
    ON usage_event(event_type, created_at);

-- usage_aggregate
CREATE TABLE IF NOT EXISTS usage_aggregate (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    period_start TIMESTAMPTZ NOT NULL,
    period_end   TIMESTAMPTZ NOT NULL,
    event_type   VARCHAR(60) NOT NULL,
    total_count  INTEGER     NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(period_start, event_type)
);

-- payment_transaction
CREATE TABLE IF NOT EXISTS payment_transaction (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id   UUID,
    provider     VARCHAR(20)  NOT NULL,
    provider_ref VARCHAR(120),
    amount_idr   INTEGER      NOT NULL,
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending',
    paid_at      TIMESTAMPTZ,
    metadata     JSONB        NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- "order" (quoted — reserved word)
CREATE TABLE IF NOT EXISTS "order" (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id      UUID,
    conversation_id UUID,
    order_number    VARCHAR(50) UNIQUE,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    items           JSONB       NOT NULL DEFAULT '[]',
    total_idr       INTEGER     NOT NULL DEFAULT 0,
    notes           TEXT,
    metadata        JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    deleted_by      UUID
);

-- conversation_summary
CREATE TABLE IF NOT EXISTS conversation_summary (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID UNIQUE NOT NULL,
    summary         TEXT        NOT NULL,
    key_topics      JSONB       NOT NULL DEFAULT '[]',
    sentiment       VARCHAR(20),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- webhook_event
CREATE TABLE IF NOT EXISTS webhook_event (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source       VARCHAR(40)  NOT NULL,
    event_type   VARCHAR(60)  NOT NULL,
    payload      JSONB        NOT NULL DEFAULT '{}',
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending',
    attempts     INTEGER      NOT NULL DEFAULT 0,
    last_error   TEXT,
    processed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_webhook_event_status
    ON webhook_event(status);
CREATE INDEX IF NOT EXISTS idx_webhook_event_created
    ON webhook_event(created_at);
`
