-- Coding test simulator (codesim) — question bank + exam sessions

CREATE TABLE IF NOT EXISTS codesim_blueprint (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        VARCHAR(80) UNIQUE NOT NULL,
    title       TEXT NOT NULL,
    config_json JSONB NOT NULL,
    is_public   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS codesim_mcq_item (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tags               TEXT[] NOT NULL,
    difficulty         VARCHAR(20) NOT NULL,
    question           TEXT NOT NULL,
    choices            JSONB NOT NULL,
    correct_id         VARCHAR(10) NOT NULL,
    explanation        TEXT NOT NULL,
    wrong_explanations JSONB NOT NULL DEFAULT '{}',
    best_practices     JSONB NOT NULL DEFAULT '[]',
    learning_objective TEXT,
    points             INTEGER NOT NULL DEFAULT 10,
    topic              VARCHAR(60),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_codesim_mcq_tags ON codesim_mcq_item USING gin(tags);

CREATE TABLE IF NOT EXISTS codesim_build_task (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family               VARCHAR(40) NOT NULL,
    title                TEXT NOT NULL,
    spec_markdown        TEXT NOT NULL,
    starter_code         TEXT NOT NULL,
    solution_code        TEXT NOT NULL,
    solution_explanation TEXT NOT NULL,
    rubric_json          JSONB NOT NULL DEFAULT '{}',
    test_cases           JSONB NOT NULL DEFAULT '[]',
    best_practices       JSONB NOT NULL DEFAULT '[]',
    common_mistakes      JSONB NOT NULL DEFAULT '[]',
    learning_objective   TEXT,
    difficulty           VARCHAR(20) NOT NULL,
    points               INTEGER NOT NULL DEFAULT 40,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS codesim_debug_task (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family             VARCHAR(40) NOT NULL,
    title              TEXT NOT NULL,
    broken_code        TEXT NOT NULL,
    solution_code      TEXT NOT NULL,
    bug_description    TEXT,
    root_cause         TEXT NOT NULL,
    fix_explanation    TEXT NOT NULL,
    test_cases         JSONB NOT NULL DEFAULT '[]',
    best_practices     JSONB NOT NULL DEFAULT '[]',
    common_mistakes    JSONB NOT NULL DEFAULT '[]',
    learning_objective TEXT,
    difficulty         VARCHAR(20) NOT NULL,
    points             INTEGER NOT NULL DEFAULT 35,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS codesim_exam_session (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     UUID NOT NULL REFERENCES tenant_account(id),
    blueprint_id   UUID REFERENCES codesim_blueprint(id),
    seed           BIGINT NOT NULL,
    status         VARCHAR(20) NOT NULL DEFAULT 'setup',
    questions_json JSONB NOT NULL DEFAULT '[]',
    answers_json   JSONB NOT NULL DEFAULT '{}',
    started_at     TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ,
    submitted_at   TIMESTAMPTZ,
    score_json     JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_codesim_session_account ON codesim_exam_session(account_id, created_at DESC);

CREATE TABLE IF NOT EXISTS codesim_proctor_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES codesim_exam_session(id) ON DELETE CASCADE,
    event_type  VARCHAR(40) NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_codesim_proctor_session ON codesim_proctor_event(session_id, created_at);

CREATE TABLE IF NOT EXISTS codesim_custom_blueprint (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL REFERENCES tenant_account(id),
    title       TEXT NOT NULL,
    config_json JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
