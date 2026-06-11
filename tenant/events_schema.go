package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
)

const eventsSchemaPatchSQL = `
CREATE TABLE IF NOT EXISTS evt_therapy (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    therapy_name  VARCHAR(120) NOT NULL,
    description   TEXT,
    is_active     BOOLEAN      NOT NULL DEFAULT true,
    display_order INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_evt_therapy_active ON evt_therapy(is_active) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS evt_volunteer_role (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_name     VARCHAR(120) NOT NULL,
    is_active     BOOLEAN      NOT NULL DEFAULT true,
    display_order INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS evt_task (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_name        VARCHAR(120) NOT NULL,
    assignment_type  VARCHAR(20)  NOT NULL DEFAULT 'PER_HOUR',
    is_active        BOOLEAN      NOT NULL DEFAULT true,
    display_order    INT          NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS evt_event (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name            VARCHAR(200) NOT NULL,
    event_slug            VARCHAR(120) NOT NULL,
    event_description     TEXT,
    location              VARCHAR(300),
    start_date            DATE         NOT NULL,
    end_date              DATE         NOT NULL,
    start_time            TIME         NOT NULL DEFAULT '09:00',
    end_time              TIME         NOT NULL DEFAULT '17:00',
    registration_open_at  TIMESTAMPTZ,
    registration_close_at TIMESTAMPTZ,
    status                VARCHAR(20)  NOT NULL DEFAULT 'DRAFT',
    created_by            UUID,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at            TIMESTAMPTZ,
    CONSTRAINT evt_event_status_chk CHECK (status IN ('DRAFT','PUBLISHED','CLOSED','CANCELLED','ARCHIVED'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_evt_event_slug ON evt_event(event_slug) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS evt_event_therapy (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id             UUID         NOT NULL REFERENCES evt_event(id) ON DELETE CASCADE,
    therapy_id           UUID         NOT NULL REFERENCES evt_therapy(id),
    slot_duration_minutes INT         NOT NULL DEFAULT 30,
    max_capacity         INT,
    capacity_mode        VARCHAR(20)  NOT NULL DEFAULT 'THERAPIST_COUNT',
    schedule_start_time  TIME,
    schedule_end_time    TIME,
    schedule_mode        VARCHAR(20)  NOT NULL DEFAULT 'AUTO',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (event_id, therapy_id),
    CONSTRAINT evt_capacity_mode_chk CHECK (capacity_mode IN ('THERAPIST_COUNT','SHIJIE_COUNT','FIXED')),
    CONSTRAINT evt_schedule_mode_chk CHECK (schedule_mode IN ('AUTO','MANUAL'))
);

CREATE TABLE IF NOT EXISTS evt_event_therapy_slot_template (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_therapy_id UUID NOT NULL REFERENCES evt_event_therapy(id) ON DELETE CASCADE,
    start_time       TIME NOT NULL,
    end_time         TIME NOT NULL,
    capacity         INT NOT NULL DEFAULT 1,
    sort_order       INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_therapy_id, start_time)
);

CREATE TABLE IF NOT EXISTS evt_event_person (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id          UUID         NOT NULL REFERENCES evt_event(id) ON DELETE CASCADE,
    full_name         VARCHAR(200) NOT NULL,
    person_type       VARCHAR(20)  NOT NULL,
    attendance_status VARCHAR(20)  NOT NULL DEFAULT 'PRESENT',
    arrival_time      TIME,
    departure_time    TIME,
    notes             TEXT,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ,
    CONSTRAINT evt_person_type_chk CHECK (person_type IN ('THERAPIST','SHIJIE','VOLUNTEER','DAOSHI','FASHI')),
    CONSTRAINT evt_attendance_chk CHECK (attendance_status IN ('PRESENT','NOT_PRESENT','PARTIAL'))
);
CREATE INDEX IF NOT EXISTS idx_evt_person_event ON evt_event_person(event_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS evt_person_therapy (
    person_id      UUID NOT NULL REFERENCES evt_event_person(id) ON DELETE CASCADE,
    therapy_id     UUID NOT NULL REFERENCES evt_therapy(id),
    available_from TIME,
    available_until TIME,
    PRIMARY KEY (person_id, therapy_id)
);

CREATE TABLE IF NOT EXISTS evt_event_therapist (
    person_id   UUID PRIMARY KEY REFERENCES evt_event_person(id) ON DELETE CASCADE,
    therapy_id  UUID NOT NULL REFERENCES evt_therapy(id),
    available_from TIME,
    available_until TIME
);

CREATE TABLE IF NOT EXISTS evt_event_volunteer (
    person_id         UUID PRIMARY KEY REFERENCES evt_event_person(id) ON DELETE CASCADE,
    volunteer_role_id UUID NOT NULL REFERENCES evt_volunteer_role(id),
    is_pencatat       BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS evt_event_assignment (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id     UUID         NOT NULL REFERENCES evt_event(id) ON DELETE CASCADE,
    task_id      UUID         NOT NULL REFERENCES evt_task(id),
    person_id    UUID         NOT NULL REFERENCES evt_event_person(id) ON DELETE CASCADE,
    start_time   TIME,
    end_time     TIME,
    session_name VARCHAR(120),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_evt_assignment_event ON evt_event_assignment(event_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS evt_time_slot (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id     UUID    NOT NULL REFERENCES evt_event(id) ON DELETE CASCADE,
    therapy_id   UUID    NOT NULL REFERENCES evt_therapy(id),
    slot_date    DATE    NOT NULL,
    start_time   TIME    NOT NULL,
    end_time     TIME    NOT NULL,
    capacity     INT     NOT NULL DEFAULT 0,
    booked_count INT     NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, therapy_id, slot_date, start_time)
);

CREATE TABLE IF NOT EXISTS evt_patient (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id              UUID         NOT NULL REFERENCES evt_event(id) ON DELETE CASCADE,
    therapy_id            UUID         NOT NULL REFERENCES evt_therapy(id),
    full_name_enc         TEXT         NOT NULL,
    birth_date_enc        TEXT         NOT NULL,
    normalized_name       VARCHAR(300) NOT NULL,
    normalized_birthdate  VARCHAR(10)  NOT NULL,
    complaint             TEXT,
    preferred_time        VARCHAR(120),
    reservation_status    VARCHAR(20)  NOT NULL DEFAULT 'CONFIRMED',
    slot_id               UUID         REFERENCES evt_time_slot(id),
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at            TIMESTAMPTZ,
    CONSTRAINT evt_patient_status_chk CHECK (reservation_status IN ('CONFIRMED','CANCELLED','COMPLETED'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_evt_patient_dup
    ON evt_patient(event_id, normalized_name, normalized_birthdate)
    WHERE deleted_at IS NULL AND reservation_status <> 'CANCELLED';

CREATE TABLE IF NOT EXISTS evt_audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(50)  NOT NULL,
    entity_id   UUID         NOT NULL,
    action      VARCHAR(50)  NOT NULL,
    actor_id    UUID,
    actor_role  VARCHAR(20),
    before_data JSONB,
    after_data  JSONB,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_evt_audit_created ON evt_audit_log(created_at DESC);

ALTER TABLE evt_event_volunteer ADD COLUMN IF NOT EXISTS is_pencatat BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS evt_person_therapy (
    person_id      UUID NOT NULL REFERENCES evt_event_person(id) ON DELETE CASCADE,
    therapy_id     UUID NOT NULL REFERENCES evt_therapy(id),
    available_from TIME,
    available_until TIME,
    PRIMARY KEY (person_id, therapy_id)
);

INSERT INTO evt_person_therapy (person_id, therapy_id, available_from, available_until)
SELECT et.person_id, et.therapy_id, et.available_from, et.available_until
FROM evt_event_therapist et
ON CONFLICT (person_id, therapy_id) DO NOTHING;

ALTER TABLE evt_event_therapy ADD COLUMN IF NOT EXISTS schedule_mode VARCHAR(20) NOT NULL DEFAULT 'AUTO';
ALTER TABLE evt_event_therapy DROP CONSTRAINT IF EXISTS evt_schedule_mode_chk;
ALTER TABLE evt_event_therapy ADD CONSTRAINT evt_schedule_mode_chk CHECK (schedule_mode IN ('AUTO','MANUAL'));

CREATE TABLE IF NOT EXISTS evt_event_therapy_slot_template (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_therapy_id UUID NOT NULL REFERENCES evt_event_therapy(id) ON DELETE CASCADE,
    start_time       TIME NOT NULL,
    end_time         TIME NOT NULL,
    capacity         INT NOT NULL DEFAULT 1,
    sort_order       INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_therapy_id, start_time)
);
ALTER TABLE evt_event_therapy_slot_template ADD COLUMN IF NOT EXISTS capacity INT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS evt_staff_roster (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name       VARCHAR(200) NOT NULL,
    normalized_name VARCHAR(300) NOT NULL,
    person_type     VARCHAR(20)  NOT NULL,
    notes           TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_evt_staff_roster_active
    ON evt_staff_roster(normalized_name, person_type) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS evt_staff_roster_therapy (
    roster_id   UUID NOT NULL REFERENCES evt_staff_roster(id) ON DELETE CASCADE,
    therapy_id  UUID NOT NULL REFERENCES evt_therapy(id),
    PRIMARY KEY (roster_id, therapy_id)
);

CREATE TABLE IF NOT EXISTS evt_staff_roster_volunteer (
    roster_id         UUID PRIMARY KEY REFERENCES evt_staff_roster(id) ON DELETE CASCADE,
    volunteer_role_id UUID REFERENCES evt_volunteer_role(id),
    is_pencatat       BOOLEAN NOT NULL DEFAULT false
);

ALTER TABLE evt_patient ADD COLUMN IF NOT EXISTS contact_id UUID REFERENCES contact(id);
ALTER TABLE contact ADD COLUMN IF NOT EXISTS birth_date DATE;

CREATE TABLE IF NOT EXISTS evt_export_job (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id     UUID         NOT NULL REFERENCES evt_event(id) ON DELETE CASCADE,
    kind         VARCHAR(40)  NOT NULL,
    params       JSONB        NOT NULL DEFAULT '{}',
    status       VARCHAR(20)  NOT NULL DEFAULT 'queued',
    download_url TEXT,
    file_name    VARCHAR(255),
    row_count    INT,
    error_msg    TEXT,
    created_by   UUID         NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_evt_export_job_event ON evt_export_job(event_id, created_at DESC);
`

func runEventsSchemaAndSeed(ctx context.Context, conn *sql.Conn) error {
	ready, err := tenantschema.EventsModuleReady(ctx, conn)
	if err != nil {
		return err
	}
	if !ready {
		if _, err := conn.ExecContext(ctx, eventsSchemaPatchSQL); err != nil {
			return err
		}
	}
	return seedEventsMasterData(ctx, conn)
}

// SeedEventsMasterDataOnly inserts default evt_* master rows when tables exist (no DDL).
func SeedEventsMasterDataOnly(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	return seedEventsMasterData(ctx, conn)
}

// RunEventsSchemaPatches applies idempotent evt_* DDL on tenants that already have evt_event
// (e.g. evt_person_therapy, is_pencatat) without re-running the full tenant patch.
func RunEventsSchemaPatches(ctx context.Context, schemaName string) error {
	if !schemaNameRe.MatchString(schemaName) {
		return fmt.Errorf("invalid schema name: %q", schemaName)
	}
	conn, err := TenantConn(ctx, schemaName)
	if err != nil {
		return err
	}
	defer conn.Close()
	return runEventsSchemaAndSeed(ctx, conn)
}

func seedEventsMasterData(ctx context.Context, conn *sql.Conn) error {
	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM evt_therapy WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for i, name := range []string{"Terapi 5 Elemen", "Terapi Shijie", "Terapi Energi Dewa"} {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO evt_therapy (therapy_name, description, display_order)
				VALUES ($1, '', $2)`, name, i+1); err != nil {
				return err
			}
		}
	}
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM evt_volunteer_role WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for i, name := range []string{"Relawan Depan", "Relawan Bakar Fu", "Relawan Pintu Keluar"} {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO evt_volunteer_role (role_name, display_order) VALUES ($1, $2)`, name, i+1); err != nil {
				return err
			}
		}
	}
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM evt_task WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		tasks := []struct {
			name, atype string
		}{
			{"Medang", "PER_HOUR"},
			{"Scan Barrier", "PER_SESSION"},
			{"Re-Scan", "PER_SESSION"},
			{"Koordinator Tengah", "FIXED"},
		}
		for i, t := range tasks {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO evt_task (task_name, assignment_type, display_order)
				VALUES ($1, $2, $3)`, t.name, t.atype, i+1); err != nil {
				return err
			}
		}
	}
	return nil
}
