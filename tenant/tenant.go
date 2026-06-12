package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/system"
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

// TenantIDBySchema resolves the system tenant id for a tenant schema name.
func TenantIDBySchema(ctx context.Context, schema string) (string, error) {
	var id string
	err := system.DB.QueryRow(ctx,
		`SELECT tc.tenant_id
		 FROM tenant_company tc
		 JOIN tenant t ON t.id = tc.tenant_id
		 WHERE tc.schema_name = $1 AND t.deleted_at IS NULL
		 LIMIT 1`,
		schema,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListSchemaNames returns tenant schema names registered in tenant_company.
func ListSchemaNames(ctx context.Context) ([]string, error) {
	rows, err := system.DB.Query(ctx,
		`SELECT tc.schema_name
		 FROM tenant_company tc
		 JOIN tenant t ON t.id = tc.tenant_id
		 WHERE tc.schema_name IS NOT NULL AND tc.schema_name <> ''
		   AND t.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			continue
		}
		names = append(names, s)
	}
	return names, rows.Err()
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
//encore:api private method=POST path=/api/v1/internal/tenant/create
func CreateTenant(ctx context.Context, p *CreateTenantParams) (*CreateTenantResponse, error) {
	schemaName := fmt.Sprintf("t_%s", p.Slug)
	if len(schemaName) > 63 {
		schemaName = schemaName[:63]
	}

	tx, err := system.DB.Stdlib().BeginTx(ctx, nil)
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
		system.DB.Exec(ctx, "DELETE FROM tenant_company WHERE id = $1", c.ID)
		system.DB.Exec(ctx, "DELETE FROM tenant WHERE id = $1", t.ID)
		return nil, appErrs.Internal("bootstrap schema: " + err.Error())
	}

	return &CreateTenantResponse{Tenant: t, Company: c}, nil
}

// GetTenantByID returns a single tenant by primary key.
//
//encore:api private method=GET path=/api/v1/internal/tenant/by-id/:id
func GetTenantByID(ctx context.Context, id string) (*TenantRow, error) {
	var t TenantRow
	err := system.DB.QueryRow(ctx,
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
//encore:api private method=GET path=/api/v1/internal/tenant/by-slug/:slug
func GetTenantBySlug(ctx context.Context, slug string) (*TenantRow, error) {
	var t TenantRow
	err := system.DB.QueryRow(ctx,
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

// RunMigrateAllTenantSchemas applies idempotent DDL patches to every tenant schema.
// Callable from exec scripts, other packages, and the private API wrapper below.
func RunMigrateAllTenantSchemas(ctx context.Context) (*MigrateSchemasResponse, error) {
	schemas, err := ListSchemaNames(ctx)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var patched, failed int
	var patchErrors []string
	for _, schema := range schemas {
		if err := RunSchemaPatches(ctx, schema); err != nil {
			failed++
			patchErrors = append(patchErrors, fmt.Sprintf("%s: %v", schema, err))
			continue
		}
		patched++
	}
	return &MigrateSchemasResponse{Patched: patched, Failed: failed, Errors: patchErrors}, nil
}

// MigrateAllTenantSchemas applies idempotent DDL patches to every tenant schema.
//
//encore:api private method=POST path=/api/v1/internal/tenant/migrate-schemas
func MigrateAllTenantSchemas(ctx context.Context) (*MigrateSchemasResponse, error) {
	return RunMigrateAllTenantSchemas(ctx)
}

type MigrateSchemasResponse struct {
	Patched int      `json:"patched"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

// ListTenants returns all active (non-deleted) tenants.
//
//encore:api private method=GET path=/api/v1/internal/tenant/list
func ListTenants(ctx context.Context) (*ListTenantsResponse, error) {
	rows, err := system.DB.Query(ctx,
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
	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn: %w", err)
	}
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s", public`, schemaName))
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
		err := system.DB.QueryRow(ctx,
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

	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schemaName)); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := ensureCloudSchemaDeployGrants(ctx, conn, schemaName); err != nil {
		return fmt.Errorf("cloud schema grants: %w", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO "%s"`, schemaName)); err != nil {
		return fmt.Errorf("set search_path: %w", err)
	}
	if _, err := conn.ExecContext(ctx, tenantDDL); err != nil {
		return fmt.Errorf("run DDL: %w", err)
	}
	if err := RunSchemaPatches(ctx, schemaName); err != nil {
		return fmt.Errorf("schema patches: %w", err)
	}
	if err := seedFinanceTransactionTypes(ctx, conn); err != nil {
		return fmt.Errorf("seed finance transaction types: %w", err)
	}
	if err := seedFinanceCategories(ctx, conn); err != nil {
		return fmt.Errorf("seed finance categories: %w", err)
	}
	if err := seedFinanceWallet(ctx, conn); err != nil {
		return fmt.Errorf("seed finance wallet: %w", err)
	}
	if err := seedFinanceApprovalSetting(ctx, conn); err != nil {
		return fmt.Errorf("seed finance approval setting: %w", err)
	}
	if err := ensureCloudSchemaDeployGrants(ctx, conn, schemaName); err != nil {
		return fmt.Errorf("cloud schema grants final: %w", err)
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
    outbound_webhook_url TEXT,
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
    ON business_catalog_item(source, external_code)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_catalog_name
    ON business_catalog_item(name, external_code)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_catalog_barcode
    ON business_catalog_item(barcode)
    WHERE deleted_at IS NULL AND barcode IS NOT NULL;

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
    status        VARCHAR(20)  NOT NULL DEFAULT 'active',
    tags          JSONB        NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    deleted_by    UUID
);
CREATE INDEX IF NOT EXISTS idx_contact_updated
    ON contact(updated_at DESC, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_contact_phone
    ON contact(phone_number)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_contact_status_updated
    ON contact(status, updated_at DESC)
    WHERE deleted_at IS NULL;

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

-- usage_aggregate (monthly period key YYYY-MM)
CREATE TABLE IF NOT EXISTS usage_aggregate (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   VARCHAR(60) NOT NULL,
    period       VARCHAR(7)  NOT NULL,
    quantity     BIGINT      NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(event_type, period)
);

-- quota_topup: paid add-on quota for the current period (strict, non-recurring)
CREATE TABLE IF NOT EXISTS quota_topup (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  UUID,
    topup_code  VARCHAR(60)  NOT NULL,
    event_type  VARCHAR(60)  NOT NULL,
    period      VARCHAR(7)   NOT NULL,
    quantity    BIGINT       NOT NULL CHECK (quantity > 0),
    amount_idr  INTEGER      NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'paid',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_quota_topup_period_event
    ON quota_topup(period, event_type) WHERE status = 'paid';

-- payment_transaction (Midtrans QRIS)
CREATE TABLE IF NOT EXISTS payment_transaction (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id              UUID,
    order_id                UUID,
    midtrans_order_id       VARCHAR(120) UNIQUE,
    midtrans_transaction_id VARCHAR(120),
    amount_idr              BIGINT       NOT NULL,
    description             TEXT,
    status                  VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    payment_type            VARCHAR(20),
    qr_url                  TEXT,
    expires_at              TIMESTAMPTZ,
    paid_at                 TIMESTAMPTZ,
    metadata                JSONB        NOT NULL DEFAULT '{}',
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at              TIMESTAMPTZ,
    deleted_by              UUID
);

-- broadcast_campaign
CREATE TABLE IF NOT EXISTS broadcast_campaign (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(200) NOT NULL,
    message_body    TEXT         NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'draft',
    scheduled_at    TIMESTAMPTZ,
    total_recipients INTEGER     NOT NULL DEFAULT 0,
    sent_count      INTEGER      NOT NULL DEFAULT 0,
    failed_count    INTEGER      NOT NULL DEFAULT 0,
    created_by      UUID,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    deleted_by      UUID
);

-- broadcast_recipient
CREATE TABLE IF NOT EXISTS broadcast_recipient (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id  UUID         NOT NULL REFERENCES broadcast_campaign(id),
    phone_number VARCHAR(32)  NOT NULL,
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending',
    last_error   TEXT,
    sent_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_broadcast_recipient_campaign
    ON broadcast_recipient(campaign_id, status);

-- "order" (quoted — reserved word)
CREATE TABLE IF NOT EXISTS "order" (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id             UUID,
    conversation_id        UUID,
    items                  JSONB        NOT NULL DEFAULT '[]',
    shipping_address       JSONB        NOT NULL DEFAULT '{}',
    notes                  TEXT,
    status                 VARCHAR(20)  NOT NULL DEFAULT 'draft',
    tracking_number        VARCHAR(120),
    courier                VARCHAR(60),
    payment_transaction_id UUID,
    subtotal               DECIMAL(15,4) NOT NULL DEFAULT 0,
    shipping_cost          DECIMAL(15,4) NOT NULL DEFAULT 0,
    total                  DECIMAL(15,4) NOT NULL DEFAULT 0,
    created_by             UUID,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at             TIMESTAMPTZ,
    deleted_by             UUID
);
CREATE INDEX IF NOT EXISTS idx_order_status_created
    ON "order"(status, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_order_contact_created
    ON "order"(contact_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- conversation_summary
CREATE TABLE IF NOT EXISTS conversation_summary (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID UNIQUE NOT NULL,
    summary         TEXT        NOT NULL,
    message_count   INTEGER     NOT NULL DEFAULT 0,
    key_topics      JSONB       NOT NULL DEFAULT '[]',
    sentiment       VARCHAR(20),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- webhook_event (outbound delivery tracking)
CREATE TABLE IF NOT EXISTS webhook_event (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   VARCHAR(60)  NOT NULL,
    payload      JSONB        NOT NULL DEFAULT '{}',
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending',
    attempts     INTEGER      NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_webhook_event_status
    ON webhook_event(status);
CREATE INDEX IF NOT EXISTS idx_webhook_event_created
    ON webhook_event(created_at);

-- ============================================================
-- FINANCE MODULE
-- ============================================================

CREATE TABLE IF NOT EXISTS fin_wallet (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    type            VARCHAR(20)  NOT NULL DEFAULT 'cash',
    institution     VARCHAR(100),
    account_no      VARCHAR(50),
    currency        VARCHAR(5)   NOT NULL DEFAULT 'IDR',
    initial_balance NUMERIC(18,2) NOT NULL DEFAULT 0,
    color           VARCHAR(7),
    icon            VARCHAR(50),
    is_active       BOOLEAN      NOT NULL DEFAULT true,
    visibility      VARCHAR(20)  NOT NULL DEFAULT 'all',
    display_order   INT          NOT NULL DEFAULT 0,
    created_by      UUID         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS fin_wallet_balance (
    wallet_id   UUID PRIMARY KEY,
    balance     NUMERIC(18,2) NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fin_category (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(80)  NOT NULL,
    type          VARCHAR(20)  NOT NULL DEFAULT 'any',
    parent_id     UUID,
    icon          VARCHAR(50),
    color         VARCHAR(7),
    is_system     BOOLEAN      NOT NULL DEFAULT false,
    display_order INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS fin_transaction_type (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code           VARCHAR(40)  NOT NULL,
    label          VARCHAR(100) NOT NULL,
    flow           VARCHAR(20)  NOT NULL,
    category_kind  VARCHAR(20)  NOT NULL DEFAULT 'any',
    show_in_quick  BOOLEAN      NOT NULL DEFAULT false,
    display_order  INT          NOT NULL DEFAULT 0,
    is_system      BOOLEAN      NOT NULL DEFAULT false,
    owner_only     BOOLEAN      NOT NULL DEFAULT false,
    is_active      BOOLEAN      NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_fin_txn_type_code ON fin_transaction_type(code) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS fin_transaction (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type             VARCHAR(20)   NOT NULL,
    amount           NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    currency         VARCHAR(5)    NOT NULL DEFAULT 'IDR',
    wallet_id        UUID          NOT NULL,
    to_wallet_id     UUID,
    category_id      UUID,
    description      TEXT,
    notes            TEXT,
    reference_no     VARCHAR(100),
    transaction_date DATE          NOT NULL DEFAULT CURRENT_DATE,
    status           VARCHAR(20)   NOT NULL DEFAULT 'approved',
    approved_by      UUID,
    approved_at      TIMESTAMPTZ,
    rejected_reason  TEXT,
    tags             TEXT[]        NOT NULL DEFAULT '{}',
    attachment_urls  JSONB         NOT NULL DEFAULT '[]',
    recurring_id     UUID,
    asset_id         UUID,
    asset_qty        NUMERIC(18,6),
    asset_price_per_unit NUMERIC(18,4),
    asset_fee        NUMERIC(18,2) NOT NULL DEFAULT 0,
    created_by       UUID          NOT NULL,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    deleted_by       UUID,
    period_locked    BOOLEAN       NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_fin_txn_wallet   ON fin_transaction(wallet_id, transaction_date);
CREATE INDEX IF NOT EXISTS idx_fin_txn_category ON fin_transaction(category_id);
CREATE INDEX IF NOT EXISTS idx_fin_txn_date     ON fin_transaction(transaction_date DESC);
CREATE INDEX IF NOT EXISTS idx_fin_txn_status   ON fin_transaction(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_fin_txn_export   ON fin_transaction(status, transaction_date DESC, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_fin_txn_asset    ON fin_transaction(asset_id) WHERE asset_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS fin_asset (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             VARCHAR(100) NOT NULL,
    ticker           VARCHAR(20),
    type             VARCHAR(20)  NOT NULL DEFAULT 'stock',
    unit_name        VARCHAR(20)  NOT NULL DEFAULT 'lot',
    unit_multiplier  NUMERIC(18,6) NOT NULL DEFAULT 1,
    price_unit_name  VARCHAR(20),
    wallet_id        UUID         NOT NULL,
    notes            TEXT,
    is_active        BOOLEAN      NOT NULL DEFAULT true,
    created_by       UUID         NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

ALTER TABLE fin_asset ADD COLUMN IF NOT EXISTS unit_multiplier NUMERIC(18,6) NOT NULL DEFAULT 1;
ALTER TABLE fin_asset ADD COLUMN IF NOT EXISTS price_unit_name VARCHAR(20);
UPDATE fin_asset SET unit_multiplier = 100
  WHERE type = 'stock' AND lower(trim(unit_name)) = 'lot' AND unit_multiplier = 1;
UPDATE fin_asset SET price_unit_name = 'lembar'
  WHERE type = 'stock' AND lower(trim(unit_name)) = 'lot' AND (price_unit_name IS NULL OR price_unit_name = '');

CREATE TABLE IF NOT EXISTS fin_asset_price (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id    UUID          NOT NULL,
    price       NUMERIC(18,4) NOT NULL,
    recorded_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    recorded_by UUID          NOT NULL,
    source      VARCHAR(50)   NOT NULL DEFAULT 'manual'
);
CREATE INDEX IF NOT EXISTS idx_fin_asset_price_latest ON fin_asset_price(asset_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS fin_recurring (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title            VARCHAR(100)  NOT NULL,
    type             VARCHAR(20)   NOT NULL,
    amount           NUMERIC(18,2) NOT NULL,
    wallet_id        UUID          NOT NULL,
    to_wallet_id     UUID,
    category_id      UUID,
    description      TEXT,
    frequency        VARCHAR(20)   NOT NULL DEFAULT 'monthly',
    frequency_value  INT           NOT NULL DEFAULT 1,
    day_of_month     INT,
    day_of_week      INT,
    mode             VARCHAR(20)   NOT NULL DEFAULT 'auto',
    start_date       DATE          NOT NULL DEFAULT CURRENT_DATE,
    end_date         DATE,
    max_occurrences  INT,
    occurrences_done INT           NOT NULL DEFAULT 0,
    next_run_date    DATE          NOT NULL DEFAULT CURRENT_DATE,
    is_active        BOOLEAN       NOT NULL DEFAULT true,
    created_by       UUID          NOT NULL,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_fin_recurring_next ON fin_recurring(next_run_date) WHERE is_active AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS fin_recurring_log (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recurring_id UUID        NOT NULL,
    run_date     DATE        NOT NULL,
    status       VARCHAR(20) NOT NULL DEFAULT 'success',
    error_msg    TEXT,
    txn_id       UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fin_budget (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID          NOT NULL,
    period      VARCHAR(7)    NOT NULL,
    amount      NUMERIC(18,2) NOT NULL,
    created_by  UUID          NOT NULL,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (category_id, period)
);

CREATE TABLE IF NOT EXISTS fin_checklist_template (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title        VARCHAR(100)  NOT NULL,
    description  TEXT,
    amount_hint  NUMERIC(18,2),
    category_id  UUID,
    wallet_id    UUID,
    frequency    VARCHAR(20)   NOT NULL DEFAULT 'daily',
    day_of_month INT,
    due_anchor_date DATE,
    is_active    BOOLEAN       NOT NULL DEFAULT true,
    display_order INT          NOT NULL DEFAULT 0,
    created_by   UUID          NOT NULL,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fin_checklist_item (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id  UUID        NOT NULL,
    due_date     DATE        NOT NULL,
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    transaction_id UUID,
    completed_by UUID,
    completed_at TIMESTAMPTZ,
    note         TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_fin_checklist_date ON fin_checklist_item(due_date, status);

CREATE TABLE IF NOT EXISTS fin_approval_setting (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    enabled          BOOLEAN       NOT NULL DEFAULT false,
    amount_threshold NUMERIC(18,2),
    require_for_types TEXT[]       NOT NULL DEFAULT '{}',
    updated_by       UUID,
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fin_period_lock (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    period    VARCHAR(7)  NOT NULL UNIQUE,
    locked_by UUID        NOT NULL,
    locked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    note      TEXT
);

CREATE TABLE IF NOT EXISTS fin_audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(50)  NOT NULL,
    entity_id   UUID         NOT NULL,
    action      VARCHAR(50)  NOT NULL,
    actor_id    UUID         NOT NULL,
    actor_role  VARCHAR(20)  NOT NULL,
    before_data JSONB,
    after_data  JSONB,
    ip_address  VARCHAR(45),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_fin_audit_entity  ON fin_audit_log(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_fin_audit_created ON fin_audit_log(created_at DESC);

CREATE TABLE IF NOT EXISTS fin_report_job (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type         VARCHAR(30)  NOT NULL,
    params       JSONB        NOT NULL DEFAULT '{}',
    status       VARCHAR(20)  NOT NULL DEFAULT 'queued',
    download_url TEXT,
    error_msg    TEXT,
    created_by   UUID         NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
`
