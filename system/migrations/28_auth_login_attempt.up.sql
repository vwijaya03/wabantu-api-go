-- Login audit (client + platform-admin portals + bootstrap). Never stores password, token or
-- bootstrap secret. Retention/cleanup is an ops task (index on at).
CREATE TABLE IF NOT EXISTS auth_login_attempt (
    id          BIGSERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    portal      TEXT NOT NULL,          -- client | admin | bootstrap
    email       TEXT NOT NULL DEFAULT '',
    account_id  UUID NULL,
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    result      TEXT NOT NULL,          -- success | failure | blocked
    reason      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS auth_login_attempt_at_idx ON auth_login_attempt (at DESC);
CREATE INDEX IF NOT EXISTS auth_login_attempt_email_idx ON auth_login_attempt (email, at DESC);
CREATE INDEX IF NOT EXISTS auth_login_attempt_ip_idx ON auth_login_attempt (ip, at DESC);
