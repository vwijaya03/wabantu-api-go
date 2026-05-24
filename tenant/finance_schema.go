package tenant

import (
	"context"
	"database/sql"
	"fmt"
)

// financeSchemaPatchSQL creates finance tables on existing tenant schemas (idempotent).
const financeSchemaPatchSQL = `
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
CREATE INDEX IF NOT EXISTS idx_fin_txn_asset    ON fin_transaction(asset_id) WHERE asset_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS fin_asset (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    ticker      VARCHAR(20),
    type        VARCHAR(20)  NOT NULL DEFAULT 'stock',
    unit_name   VARCHAR(20)  NOT NULL DEFAULT 'lot',
    wallet_id   UUID         NOT NULL,
    notes       TEXT,
    is_active   BOOLEAN      NOT NULL DEFAULT true,
    created_by  UUID         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

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

func runFinanceSchemaAndSeed(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, financeSchemaPatchSQL); err != nil {
		return fmt.Errorf("finance DDL: %w", err)
	}
	if err := seedFinanceCategories(ctx, conn); err != nil {
		return fmt.Errorf("finance seed categories: %w", err)
	}
	if err := seedFinanceWallet(ctx, conn); err != nil {
		return fmt.Errorf("finance seed wallet: %w", err)
	}
	if err := seedFinanceApprovalSetting(ctx, conn); err != nil {
		return fmt.Errorf("finance seed approval: %w", err)
	}
	return nil
}
