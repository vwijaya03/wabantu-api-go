package pii

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const backfillBatch = 500

type dbTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// BackfillTenant encrypts legacy plaintext PII rows (idempotent). Requires PII columns and valid key.
func BackfillTenant(ctx context.Context, db dbTX, encKey string) error {
	if err := ValidateKey(encKey); err != nil {
		return nil
	}
	steps := []func(context.Context, dbTX, string) error{
		backfillContacts,
		backfillLeads,
		backfillEventPersons,
		backfillStaffRoster,
		backfillChecklistTitles,
		backfillRecurringTitles,
		backfillBroadcastRecipients,
	}
	for _, fn := range steps {
		if err := fn(ctx, db, encKey); err != nil {
			return err
		}
	}
	return nil
}

func finishRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}

func backfillContacts(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, phone_number, display_name, birth_date::text
		FROM contact
		WHERE deleted_at IS NULL
		  AND (phone_number_enc IS NULL OR phone_number_enc = '')
		  AND NULLIF(TRIM(phone_number), '') IS NOT NULL
		  AND phone_number <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		return fmt.Errorf("backfill contacts: %w", err)
	}
	type row struct {
		id, phone, display, birth string
	}
	var batch []row
	for rows.Next() {
		var r row
		var displayName, birthDate sql.NullString
		if err := rows.Scan(&r.id, &r.phone, &displayName, &birthDate); err != nil {
			_ = rows.Close()
			return err
		}
		r.display = displayName.String
		r.birth = birthDate.String
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		if err := writeContactRow(ctx, db, key, r.id, r.phone, r.display, r.birth); err != nil {
			return err
		}
	}
	return nil
}

func writeContactRow(ctx context.Context, db dbTX, key, id, phone, displayName, birthDate string) error {
	phoneEnc, err := Encrypt(phone, key)
	if err != nil {
		return err
	}
	phoneIdx := BlindIndex(NormalizePhone(phone), key)
	var dupID string
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM contact
		WHERE phone_number_idx = $1 AND id <> $2 AND deleted_at IS NULL
		LIMIT 1`, phoneIdx, id).Scan(&dupID); err == nil {
		_, _ = db.ExecContext(ctx, `
			UPDATE contact SET deleted_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL`, id)
		return nil
	}
	var displayEnc, displayIdx, birthEnc string
	if v := strings.TrimSpace(displayName); v != "" {
		displayEnc, err = Encrypt(v, key)
		if err != nil {
			return err
		}
		displayIdx = BlindIndex(NormalizeName(v), key)
	}
	if bd := strings.TrimSpace(birthDate); bd != "" {
		if len(bd) > 10 {
			bd = bd[:10]
		}
		birthEnc, err = Encrypt(bd, key)
		if err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `
		UPDATE contact SET
		  phone_number_enc = $1,
		  phone_number_idx = $2,
		  display_name_enc = NULLIF($3, ''),
		  display_name_idx = NULLIF($4, ''),
		  birth_date_enc = NULLIF($5, ''),
		  phone_number = $6,
		  display_name = CASE WHEN $3 <> '' THEN $6 ELSE display_name END,
		  birth_date = NULL
		WHERE id = $7`,
		phoneEnc, phoneIdx, displayEnc, nullIfEmptyStr(displayIdx), birthEnc, Placeholder, id)
	return err
}

func backfillLeads(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, phone_number, name
		FROM lead
		WHERE deleted_at IS NULL
		  AND (phone_number_enc IS NULL OR phone_number_enc = '')
		  AND NULLIF(TRIM(phone_number), '') IS NOT NULL
		  AND phone_number <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		return fmt.Errorf("backfill leads: %w", err)
	}
	type row struct {
		id, phone, name string
	}
	var batch []row
	for rows.Next() {
		var r row
		var name sql.NullString
		if err := rows.Scan(&r.id, &r.phone, &name); err != nil {
			_ = rows.Close()
			return err
		}
		r.name = name.String
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		phoneEnc, err := Encrypt(r.phone, key)
		if err != nil {
			return err
		}
		phoneIdx := BlindIndex(NormalizePhone(r.phone), key)
		var nameEnc, nameIdx string
		if n := strings.TrimSpace(r.name); n != "" {
			nameEnc, err = Encrypt(n, key)
			if err != nil {
				return err
			}
			nameIdx = BlindIndex(NormalizeName(n), key)
		}
		_, err = db.ExecContext(ctx, `
			UPDATE lead SET
			  phone_number_enc = $1, phone_number_idx = $2, phone_number = $3,
			  name_enc = NULLIF($4, ''), name_idx = NULLIF($5, ''),
			  name = CASE WHEN $4 <> '' THEN $3 ELSE name END
			WHERE id = $6`,
			phoneEnc, phoneIdx, Placeholder, nameEnc, nullIfEmptyStr(nameIdx), r.id)
		if err != nil {
			return err
		}
	}
	return nil
}

func backfillEventPersons(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, full_name
		FROM evt_event_person
		WHERE deleted_at IS NULL
		  AND (full_name_enc IS NULL OR full_name_enc = '')
		  AND NULLIF(TRIM(full_name), '') IS NOT NULL
		  AND full_name <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		if isMissingColumn(err) {
			return nil
		}
		return fmt.Errorf("backfill evt_event_person: %w", err)
	}
	type row struct{ id, name string }
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		enc, err := Encrypt(r.name, key)
		if err != nil {
			return err
		}
		norm := BlindIndex(NormalizeName(r.name), key)
		_, err = db.ExecContext(ctx, `
			UPDATE evt_event_person SET
			  full_name_enc = $1, normalized_name = $2, full_name = $3
			WHERE id = $4`, enc, norm, Placeholder, r.id)
		if err != nil {
			return err
		}
	}
	return nil
}

func backfillStaffRoster(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, full_name, normalized_name
		FROM evt_staff_roster
		WHERE deleted_at IS NULL
		  AND (full_name_enc IS NULL OR full_name_enc = '')
		  AND NULLIF(TRIM(full_name), '') IS NOT NULL
		  AND full_name <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		if isMissingColumn(err) {
			return nil
		}
		return fmt.Errorf("backfill evt_staff_roster: %w", err)
	}
	type row struct{ id, name string }
	var batch []row
	for rows.Next() {
		var r row
		var normLegacy string
		if err := rows.Scan(&r.id, &r.name, &normLegacy); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		enc, err := Encrypt(r.name, key)
		if err != nil {
			return err
		}
		idx := BlindIndex(NormalizeName(r.name), key)
		_, err = db.ExecContext(ctx, `
			UPDATE evt_staff_roster SET
			  full_name_enc = $1, normalized_name_idx = $2, full_name = $3
			WHERE id = $4`, enc, idx, Placeholder, r.id)
		if err != nil {
			return err
		}
	}
	return nil
}

func backfillChecklistTitles(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, title FROM fin_checklist_template
		WHERE deleted_at IS NULL
		  AND (title_enc IS NULL OR title_enc = '')
		  AND NULLIF(TRIM(title), '') IS NOT NULL
		  AND title <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		if isMissingColumn(err) {
			return nil
		}
		return fmt.Errorf("backfill fin_checklist_template: %w", err)
	}
	type row struct{ id, title string }
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		enc, err := Encrypt(r.title, key)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `
			UPDATE fin_checklist_template SET title_enc = $1, title = $2 WHERE id = $3`,
			enc, Placeholder, r.id)
		if err != nil {
			return err
		}
	}
	return nil
}

func backfillRecurringTitles(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, title FROM fin_recurring
		WHERE deleted_at IS NULL
		  AND (title_enc IS NULL OR title_enc = '')
		  AND NULLIF(TRIM(title), '') IS NOT NULL
		  AND title <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		if isMissingColumn(err) {
			return nil
		}
		return fmt.Errorf("backfill fin_recurring: %w", err)
	}
	type row struct{ id, title string }
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		enc, err := Encrypt(r.title, key)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `
			UPDATE fin_recurring SET title_enc = $1, title = $2 WHERE id = $3`,
			enc, Placeholder, r.id)
		if err != nil {
			return err
		}
	}
	return nil
}

func backfillBroadcastRecipients(ctx context.Context, db dbTX, key string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, phone_number FROM broadcast_recipient
		WHERE (phone_number_enc IS NULL OR phone_number_enc = '')
		  AND NULLIF(TRIM(phone_number), '') IS NOT NULL
		  AND phone_number <> $1
		LIMIT $2`, Placeholder, backfillBatch)
	if err != nil {
		if isMissingColumn(err) {
			return nil
		}
		return fmt.Errorf("backfill broadcast_recipient: %w", err)
	}
	type row struct{ id, phone string }
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.phone); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	if err := finishRows(rows); err != nil {
		return err
	}
	for _, r := range batch {
		enc, err := Encrypt(r.phone, key)
		if err != nil {
			return err
		}
		idx := BlindIndex(NormalizePhone(r.phone), key)
		_, err = db.ExecContext(ctx, `
			UPDATE broadcast_recipient SET
			  phone_number_enc = $1, phone_number_idx = $2, phone_number = $3
			WHERE id = $4`, enc, idx, Placeholder, r.id)
		if err != nil {
			return err
		}
	}
	return nil
}

func nullIfEmptyStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

func isMissingColumn(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") && strings.Contains(msg, "column")
}
