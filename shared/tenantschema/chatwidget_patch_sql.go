package tenantschema

// ChatWidgetPatchSQL is idempotent DDL for web chat widget tables (Epic 1).
const ChatWidgetPatchSQL = `
CREATE TABLE IF NOT EXISTS chat_widget_config (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    is_enabled          BOOLEAN NOT NULL DEFAULT false,
    install_id          UUID,
    welcome_message     TEXT,
    persona_name        VARCHAR(120),
    persona_avatar_url  TEXT,
    position            VARCHAR(20) NOT NULL DEFAULT 'bottom-right',
    locale              VARCHAR(10) NOT NULL DEFAULT 'id',
    custom_tokens       JSONB NOT NULL DEFAULT '{}',
    allowed_domains     JSONB NOT NULL DEFAULT '[]',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS web_chat_session (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    visitor_token_hash VARCHAR(64) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    metadata        JSONB NOT NULL DEFAULT '{}',
    order_state     JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_web_chat_session_visitor
    ON web_chat_session(visitor_token_hash, created_at DESC);

CREATE TABLE IF NOT EXISTS web_chat_message (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES web_chat_session(id) ON DELETE CASCADE,
    client_message_id VARCHAR(80),
    role            VARCHAR(20) NOT NULL,
    body            TEXT NOT NULL,
    content_type    VARCHAR(20) NOT NULL DEFAULT 'text',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_web_chat_msg_session
    ON web_chat_message(session_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_web_chat_msg_client_id
    ON web_chat_message(session_id, client_message_id) WHERE client_message_id IS NOT NULL;
`
