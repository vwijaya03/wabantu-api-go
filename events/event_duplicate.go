package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type DuplicateEventParams struct {
	EventName *string `json:"eventName,omitempty"`
	StartDate *string `json:"startDate,omitempty"`
	EndDate   *string `json:"endDate,omitempty"`
}

type DuplicateEventResponse struct {
	Event                 Event `json:"event"`
	PeopleCopied          int   `json:"peopleCopied"`
	PatientsCopied        int   `json:"patientsCopied"`
	TherapySettingsCopied int   `json:"therapySettingsCopied"`
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/duplicate tag:owner
func DuplicateEvent(ctx context.Context, eventId string, p *DuplicateEventParams) (*DuplicateEventResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil {
		p = &DuplicateEventParams{}
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	src, err := loadEventRow(ctx, conn, eventId)
	if err != nil {
		return nil, err
	}

	newName := strings.TrimSpace(src.EventName) + " (Salinan)"
	if p.EventName != nil && strings.TrimSpace(*p.EventName) != "" {
		newName = strings.TrimSpace(*p.EventName)
	}
	startDate := src.StartDate
	endDate := src.EndDate
	if p.StartDate != nil && strings.TrimSpace(*p.StartDate) != "" {
		startDate = strings.TrimSpace(*p.StartDate)
	}
	if p.EndDate != nil && strings.TrimSpace(*p.EndDate) != "" {
		endDate = strings.TrimSpace(*p.EndDate)
	}

	slug, err := uniqueSlug(ctx, conn, slugify(newName), "")
	if err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	var newEventID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO evt_event (
		  event_name, event_slug, event_description, location,
		  start_date, end_date, start_time, end_time,
		  break_start_time, break_end_time,
		  registration_open_at, registration_close_at, status, created_by
		)
		SELECT $1,$2,event_description,location,
		  $3::date,$4::date,start_time,end_time,
		  break_start_time,break_end_time,
		  registration_open_at,registration_close_at,'DRAFT',$5::uuid
		FROM evt_event WHERE id=$6::uuid AND deleted_at IS NULL
		RETURNING id::text`,
		newName, slug, startDate, endDate, u.AccountID, eventId,
	).Scan(&newEventID)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	therapySettingsCopied, err := copyEventTherapySettings(ctx, tx, eventId, newEventID)
	if err != nil {
		return nil, err
	}
	peopleCopied, err := copyEventPeople(ctx, tx, eventId, newEventID)
	if err != nil {
		return nil, err
	}
	patientsCopied, err := copyEventPatients(ctx, tx, eventId, newEventID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	auditEvent(ctx, conn, u, "event", newEventID, "duplicate", map[string]any{"sourceEventId": eventId}, map[string]any{
		"peopleCopied": peopleCopied, "patientsCopied": patientsCopied,
	})

	out, err := GetEvent(ctx, newEventID)
	if err != nil {
		return nil, err
	}
	return &DuplicateEventResponse{
		Event:                 *out,
		PeopleCopied:          peopleCopied,
		PatientsCopied:        patientsCopied,
		TherapySettingsCopied: therapySettingsCopied,
	}, nil
}

type eventRow struct {
	EventName string
	StartDate string
	EndDate   string
}

func loadEventRow(ctx context.Context, conn *sql.Conn, eventID string) (*eventRow, error) {
	var r eventRow
	err := conn.QueryRowContext(ctx, `
		SELECT event_name, start_date::text, end_date::text
		FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventID,
	).Scan(&r.EventName, &r.StartDate, &r.EndDate)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &r, nil
}

func copyEventTherapySettings(ctx context.Context, tx *sql.Tx, srcID, dstID string) (int, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO evt_event_therapy (
		  event_id, therapy_id, slot_duration_minutes, max_capacity, capacity_mode,
		  schedule_mode, schedule_start_time, schedule_end_time
		)
		SELECT $1::uuid, therapy_id, slot_duration_minutes, max_capacity, capacity_mode,
		  COALESCE(schedule_mode,'AUTO'), schedule_start_time, schedule_end_time
		FROM evt_event_therapy WHERE event_id=$2::uuid
		ON CONFLICT (event_id, therapy_id) DO NOTHING`,
		dstID, srcID)
	if err != nil {
		return 0, appErrs.Internal(err.Error())
	}
	n, _ := res.RowsAffected()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO evt_event_therapy_slot_template (event_therapy_id, start_time, end_time, sort_order)
		SELECT new_et.id, st.start_time, st.end_time, st.sort_order
		FROM evt_event_therapy_slot_template st
		JOIN evt_event_therapy old_et ON old_et.id = st.event_therapy_id AND old_et.event_id = $2::uuid
		JOIN evt_event_therapy new_et ON new_et.event_id = $1::uuid AND new_et.therapy_id = old_et.therapy_id
		ON CONFLICT (event_therapy_id, start_time) DO NOTHING`,
		dstID, srcID)
	if err != nil {
		return int(n), appErrs.Internal(err.Error())
	}
	return int(n), nil
}

func copyEventPeople(ctx context.Context, tx *sql.Tx, srcID, dstID string) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, full_name, person_type, attendance_status,
		       arrival_time::text, departure_time::text, COALESCE(notes,'')
		FROM evt_event_person
		WHERE event_id=$1::uuid AND deleted_at IS NULL
		ORDER BY person_type, full_name`, srcID)
	if err != nil {
		return 0, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	copied := 0
	for rows.Next() {
		var oldID, fullName, personType, att, notes string
		var arr, dep sql.NullString
		if err := rows.Scan(&oldID, &fullName, &personType, &att, &arr, &dep, &notes); err != nil {
			return copied, appErrs.Internal(err.Error())
		}
		var newID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO evt_event_person (
			  event_id, full_name, person_type, attendance_status,
			  arrival_time, departure_time, notes
			) VALUES ($1::uuid,$2,$3,$4,$5::time,$6::time,$7)
			RETURNING id::text`,
			dstID, fullName, personType, att,
			nullTimeStrPtr(nullStringPtr(arr)), nullTimeStrPtr(nullStringPtr(dep)), notes,
		).Scan(&newID)
		if err != nil {
			return copied, appErrs.Internal(err.Error())
		}

		if isTherapyStaffPersonType(personType) {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO evt_person_therapy (person_id, therapy_id, available_from, available_until)
				SELECT $1::uuid, therapy_id, available_from, available_until
				FROM evt_person_therapy WHERE person_id=$2::uuid`,
				newID, oldID)
			if err != nil {
				return copied, appErrs.Internal(err.Error())
			}
		}
		if personType == "VOLUNTEER" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO evt_event_volunteer (person_id, volunteer_role_id, is_pencatat)
				SELECT $1::uuid, volunteer_role_id, is_pencatat
				FROM evt_event_volunteer WHERE person_id=$2::uuid`,
				newID, oldID)
			if err != nil {
				return copied, appErrs.Internal(err.Error())
			}
		}
		copied++
	}
	return copied, rows.Err()
}

func nullStringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func copyEventPatients(ctx context.Context, tx *sql.Tx, srcID, dstID string) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT therapy_id::text, full_name_enc, birth_date_enc,
		       normalized_name, normalized_birthdate,
		       COALESCE(complaint,''), COALESCE(preferred_time,'')
		FROM evt_patient
		WHERE event_id=$1::uuid AND deleted_at IS NULL
		  AND reservation_status <> 'CANCELLED'`, srcID)
	if err != nil {
		return 0, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	copied := 0
	for rows.Next() {
		var therapyID, encName, encBirth, normName, normBirth, complaint, preferred string
		if err := rows.Scan(&therapyID, &encName, &encBirth, &normName, &normBirth, &complaint, &preferred); err != nil {
			return copied, appErrs.Internal(err.Error())
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO evt_patient (
			  event_id, therapy_id, full_name_enc, birth_date_enc,
			  normalized_name, normalized_birthdate, complaint, preferred_time,
			  reservation_status, slot_id
			) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,'CONFIRMED',NULL)`,
			dstID, therapyID, encName, encBirth, normName, normBirth, nullStr(complaint), nullStr(preferred))
		if err != nil {
			if isDuplicateKeyErr(err) {
				continue
			}
			return copied, appErrs.Internal(fmt.Sprintf("gagal menyalin pasien: %v", err))
		}
		copied++
	}
	return copied, rows.Err()
}
