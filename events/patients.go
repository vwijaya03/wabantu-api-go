package events

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	encoreerrs "encore.dev/beta/errs"

	appErrs "encore.app/wabantu/shared/errs"
)

type Patient struct {
	ID                string  `json:"id"`
	EventID           string  `json:"eventId"`
	TherapyID         string  `json:"therapyId"`
	TherapyName       string  `json:"therapyName,omitempty"`
	FullName          string  `json:"fullName"`
	BirthDate         string  `json:"birthDate"`
	Complaint         string  `json:"complaint,omitempty"`
	PreferredTime     string  `json:"preferredTime,omitempty"`
	ReservationStatus string  `json:"reservationStatus"`
	SlotID            *string `json:"slotId,omitempty"`
	SlotLabel         string  `json:"slotLabel,omitempty"`
}

type ListPatientsParams struct {
	Q         string `query:"q"`
	TherapyID string `query:"therapyId"`
	Status    string `query:"status"`
	SlotDate  string `query:"slotDate"`
	HasSlot   string `query:"hasSlot"`
	SortBy    string `query:"sortBy"`
	SortDir   string `query:"sortDir"`
	Page      int    `query:"page"`
	PageSize  int    `query:"pageSize"`
}

type ListPatientsResponse struct {
	Items []Patient `json:"items"`
	Total int       `json:"total"`
}

type UpsertPatientStatusParams struct {
	ReservationStatus string `json:"reservationStatus"`
}

type CreatePatientParams struct {
	ContactID     string `json:"contactId,omitempty"`
	FullName      string `json:"fullName"`
	BirthDate     string `json:"birthDate"`
	TherapyID     string `json:"therapyId"`
	Complaint     string `json:"complaint,omitempty"`
	PreferredTime string `json:"preferredTime,omitempty"`
}

type UpdatePatientParams struct {
	FullName          string `json:"fullName"`
	BirthDate         string `json:"birthDate"`
	TherapyID         string `json:"therapyId"`
	Complaint         string `json:"complaint,omitempty"`
	PreferredTime     string `json:"preferredTime,omitempty"`
	ReservationStatus string `json:"reservationStatus,omitempty"`
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/patients tag:owner
func CreateEventPatient(ctx context.Context, eventId string, p *CreatePatientParams) (*Patient, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, appErrs.BadRequest("data pasien wajib diisi")
	}
	patientID, err := createPatientForEvent(ctx, u.TenantSchema, eventId, p, false, strings.TrimSpace(p.PreferredTime) != "")
	if err != nil {
		return nil, err
	}
	return &Patient{
		ID: patientID, EventID: eventId, TherapyID: p.TherapyID,
		FullName: strings.TrimSpace(p.FullName), BirthDate: p.BirthDate,
		Complaint: p.Complaint, PreferredTime: p.PreferredTime,
		ReservationStatus: "CONFIRMED",
	}, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/patients
func ListPatients(ctx context.Context, eventId string, p *ListPatientsParams) (*ListPatientsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventExists(ctx, u, ts, eventId); err != nil {
		return nil, err
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	filters := patientFilterInput{}
	if p != nil {
		filters = patientFilterInput{
			Q: p.Q, TherapyID: p.TherapyID, Status: p.Status,
			SlotDate: p.SlotDate, HasSlot: p.HasSlot,
			SortBy: p.SortBy, SortDir: p.SortDir,
		}
	}
	items, total, err := queryPatients(ctx, ts, eventId, filters, lim, off)
	if err != nil {
		var encErr *encoreerrs.Error
		if errors.As(err, &encErr) {
			return nil, err
		}
		return nil, appErrs.Internal("gagal memuat pasien")
	}
	return &ListPatientsResponse{Items: items, Total: total}, nil
}

//encore:api auth method=PATCH path=/api/v1/events/detail/:eventId/patients/:patientId tag:owner
func UpdatePatientStatus(ctx context.Context, eventId, patientId string, p *UpsertPatientStatusParams) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	st := strings.ToUpper(strings.TrimSpace(p.ReservationStatus))
	if st != "CONFIRMED" && st != "CANCELLED" && st != "COMPLETED" {
		return appErrs.BadRequest("status reservasi tidak valid")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return err
	}
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer tx.Rollback()
	txTS := ts.WithQ(tx)

	var oldStatus string
	var slotID sql.NullString
	err = txTS.QueryRowContext(ctx, `
		SELECT reservation_status, slot_id::text FROM evt_patient
		WHERE id=$1::uuid AND event_id=$2::uuid AND deleted_at IS NULL`,
		patientId, eventId).Scan(&oldStatus, &slotID)
	if err == sql.ErrNoRows {
		return appErrs.NotFound("pasien tidak ditemukan")
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}

	if st == "CANCELLED" && oldStatus == "CONFIRMED" && slotID.Valid {
		_, _ = txTS.ExecContext(ctx, `
			UPDATE evt_time_slot SET booked_count = GREATEST(0, booked_count - 1)
			WHERE id=$1::uuid`, slotID.String)
	}

	_, err = txTS.ExecContext(ctx, `
		UPDATE evt_patient SET reservation_status=$1, updated_at=now()
		WHERE id=$2::uuid AND event_id=$3::uuid`, st, patientId, eventId)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "patient", patientId, "status", oldStatus, st)
	return nil
}

//encore:api auth method=PUT path=/api/v1/events/detail/:eventId/patients/:patientId tag:owner
func UpdateEventPatient(ctx context.Context, eventId, patientId string, p *UpdatePatientParams) (*Patient, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, appErrs.BadRequest("data pasien wajib diisi")
	}
	fullName := clampLen(p.FullName, maxPatientNameLen)
	if strings.TrimSpace(fullName) == "" {
		return nil, appErrs.BadRequest("nama pasien wajib diisi")
	}
	if strings.TrimSpace(p.TherapyID) == "" {
		return nil, appErrs.BadRequest("terapi wajib dipilih")
	}
	normBirth, err := normalizeBirthDate(p.BirthDate)
	if err != nil {
		return nil, err
	}
	encName, err := encryptPatientField(strings.TrimSpace(fullName))
	if err != nil {
		return nil, err
	}
	encBirth, err := encryptPatientField(normBirth)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return nil, err
	}
	var therapyLinked bool
	if err := ts.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM evt_event_therapy WHERE event_id=$1::uuid AND therapy_id=$2::uuid)`,
		eventId, p.TherapyID).Scan(&therapyLinked); err != nil || !therapyLinked {
		return nil, appErrs.BadRequest("terapi tidak tersedia untuk acara ini")
	}
	st := strings.ToUpper(strings.TrimSpace(p.ReservationStatus))
	if st == "" {
		st = "CONFIRMED"
	}
	if st != "CONFIRMED" && st != "CANCELLED" && st != "COMPLETED" {
		return nil, appErrs.BadRequest("status reservasi tidak valid")
	}
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()
	txTS := ts.WithQ(tx)

	_, err = txTS.ExecContext(ctx, `
		UPDATE evt_patient SET
		  full_name_enc=$1, birth_date_enc=$2,
		  normalized_name=$3, normalized_birthdate=$4,
		  therapy_id=$5::uuid, complaint=$6, preferred_time=$7,
		  reservation_status=$8, updated_at=now()
		WHERE id=$9::uuid AND event_id=$10::uuid AND deleted_at IS NULL`,
		encName, encBirth, patientBlindName(fullName), normBirth,
		p.TherapyID, nullStr(p.Complaint), nullStr(p.PreferredTime), st, patientId, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	_ = tryAssignPatientSlot(ctx, txTS, eventId, patientId, p.TherapyID, p.PreferredTime, false)
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var therapyName string
	_ = ts.QueryRowContext(ctx, `SELECT therapy_name FROM evt_therapy WHERE id=$1::uuid`, p.TherapyID).Scan(&therapyName)

	var slotLabel string
	var slotID *string
	var slotIDNull, sd, stime, etime sql.NullString
	_ = ts.QueryRowContext(ctx, `
		SELECT pat.slot_id::text, s.slot_date::text, s.start_time::text, s.end_time::text
		FROM evt_patient pat
		LEFT JOIN evt_time_slot s ON s.id = pat.slot_id
		WHERE pat.id=$1::uuid`, patientId).Scan(&slotIDNull, &sd, &stime, &etime)
	if slotIDNull.Valid && slotIDNull.String != "" && sd.Valid && stime.Valid {
		sid := slotIDNull.String
		slotID = &sid
		end := ""
		if etime.Valid {
			end = etime.String
		}
		slotLabel = formatPatientSlotLabel(sd.String, stime.String, end)
	}

	return &Patient{
		ID: patientId, EventID: eventId, TherapyID: p.TherapyID, TherapyName: therapyName,
		FullName: fullName, BirthDate: normBirth,
		Complaint: p.Complaint, PreferredTime: p.PreferredTime,
		ReservationStatus: st, SlotID: slotID, SlotLabel: slotLabel,
	}, nil
}

type DeletePatientsParams struct {
	PatientIDs []string `json:"patientIds"`
}

type DeletePatientsResponse struct {
	Deleted int      `json:"deleted"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

func deleteEventPatientInTx(ctx context.Context, ts tenantScope, eventId, patientId string) error {
	var slotID sql.NullString
	var status string
	err := ts.QueryRowContext(ctx, `
		SELECT reservation_status, slot_id::text FROM evt_patient
		WHERE id=$1::uuid AND event_id=$2::uuid AND deleted_at IS NULL`, patientId, eventId).Scan(&status, &slotID)
	if err == sql.ErrNoRows {
		return appErrs.NotFound("pasien tidak ditemukan")
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if status == "CONFIRMED" && slotID.Valid {
		_, _ = ts.ExecContext(ctx, `
			UPDATE evt_time_slot SET booked_count = GREATEST(0, booked_count - 1)
			WHERE id=$1::uuid`, slotID.String)
	}
	_, err = ts.ExecContext(ctx, `
		UPDATE evt_patient SET deleted_at=now(), updated_at=now()
		WHERE id=$1::uuid AND event_id=$2::uuid`, patientId, eventId)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}

//encore:api auth method=DELETE path=/api/v1/events/detail/:eventId/patients/:patientId tag:owner
func DeleteEventPatient(ctx context.Context, eventId, patientId string) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return err
	}
	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer tx.Rollback()
	txTS := ts.WithQ(tx)
	if err := deleteEventPatientInTx(ctx, txTS, eventId, patientId); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "patient", patientId, "delete", nil, nil)
	return nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/patients/delete-bulk tag:owner
func DeletePatientsBulk(ctx context.Context, eventId string, p *DeletePatientsParams) (*DeletePatientsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil || len(p.PatientIDs) == 0 {
		return nil, appErrs.BadRequest("pilih minimal satu pasien")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return nil, err
	}

	resp := &DeletePatientsResponse{Errors: []string{}}
	seen := map[string]bool{}
	for _, rawID := range p.PatientIDs {
		patientID := strings.TrimSpace(rawID)
		if patientID == "" || seen[patientID] {
			continue
		}
		seen[patientID] = true

		tx, err := ts.BeginTx(ctx, nil)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		txTS := ts.WithQ(tx)
		if err := deleteEventPatientInTx(ctx, txTS, eventId, patientID); err != nil {
			_ = tx.Rollback()
			resp.Failed++
			if encErr, ok := err.(*encoreerrs.Error); ok && encErr.Code == encoreerrs.NotFound {
				resp.Errors = append(resp.Errors, patientID+": pasien tidak ditemukan")
			} else {
				resp.Errors = append(resp.Errors, patientID+": "+err.Error())
			}
			continue
		}
		if err := tx.Commit(); err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, patientID+": gagal menghapus")
			continue
		}
		resp.Deleted++
		auditEvent(ctx, ts, u, "patient", patientID, "delete_bulk_item", map[string]any{"eventId": eventId}, nil)
	}
	if len(resp.Errors) == 0 {
		resp.Errors = nil
	}
	return resp, nil
}

func registerPatient(ctx context.Context, tenantSchema string, eventID string, therapyID, fullName, birthDate, complaint, preferred string) (string, error) {
	return createPatientForEvent(ctx, tenantSchema, eventID, &CreatePatientParams{
		FullName: fullName, BirthDate: birthDate, TherapyID: therapyID,
		Complaint: complaint, PreferredTime: preferred,
	}, true, true)
}

func createPatientForEvent(
	ctx context.Context,
	tenantSchema string,
	eventID string,
	p *CreatePatientParams,
	requirePublished bool,
	assignSlot bool,
) (string, error) {
	ts, err := openTenant(ctx, tenantSchema)
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}

	contactFields, err := resolvePatientContact(ctx, ts, p)
	if err != nil {
		return "", err
	}

	fullName := clampLen(p.FullName, maxPatientNameLen)
	complaint := clampLen(p.Complaint, maxComplaintLen)
	preferred := clampLen(p.PreferredTime, maxPreferredTimeLen)
	therapyID := strings.TrimSpace(p.TherapyID)
	if strings.TrimSpace(fullName) == "" {
		return "", appErrs.BadRequest("nama pasien wajib diisi")
	}
	if therapyID == "" {
		return "", appErrs.BadRequest("terapi wajib dipilih")
	}
	normBirth, err := normalizeBirthDate(p.BirthDate)
	if err != nil {
		return "", err
	}
	encName, err := encryptPatientField(strings.TrimSpace(fullName))
	if err != nil {
		return "", err
	}
	encBirth, err := encryptPatientField(normBirth)
	if err != nil {
		return "", err
	}

	if requirePublished {
		if err := assertEventMutable(ctx, ts, eventID); err != nil {
			return "", err
		}
	} else if err := assertEventExists(ctx, nil, ts, eventID); err != nil {
		return "", err
	} else if err := assertEventMutable(ctx, ts, eventID); err != nil {
		return "", err
	}

	if requirePublished {
		var status string
		var openAt, closeAt sql.NullTime
		if err := ts.QueryRowContext(ctx, `
			SELECT status, registration_open_at, registration_close_at
			FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventID,
		).Scan(&status, &openAt, &closeAt); err == sql.ErrNoRows {
			return "", appErrs.NotFound("acara tidak ditemukan")
		} else if err != nil {
			return "", appErrs.Internal(err.Error())
		}
		if strings.ToUpper(status) != "PUBLISHED" {
			return "", appErrs.BadRequest("Pendaftaran telah ditutup.")
		}
		if !registrationOpen(time.Now(), openAt, closeAt) {
			return "", appErrs.BadRequest("Pendaftaran telah ditutup.")
		}
	}

	var therapyLinked bool
	if err := ts.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM evt_event_therapy WHERE event_id=$1::uuid AND therapy_id=$2::uuid
		)`, eventID, therapyID).Scan(&therapyLinked); err != nil {
		return "", appErrs.Internal(err.Error())
	}
	if !therapyLinked {
		return "", appErrs.BadRequest("terapi tidak tersedia untuk acara ini")
	}

	var dup bool
	if err := ts.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM evt_patient
		  WHERE event_id=$1::uuid AND normalized_name=$2 AND normalized_birthdate=$3
		    AND deleted_at IS NULL AND reservation_status <> 'CANCELLED'
		)`, eventID, patientBlindName(fullName), normBirth).Scan(&dup); err != nil {
		return "", appErrs.Internal(err.Error())
	}
	if dup {
		return "", appErrs.BadRequest("pasien sudah terdaftar untuk acara ini")
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	defer tx.Rollback()
	txTS := ts.WithQ(tx)

	var slotID interface{}
	if assignSlot {
		sid, err := pickSlotForRegistration(ctx, txTS, eventID, therapyID, strings.TrimSpace(preferred), requirePublished)
		if err != nil {
			return "", err
		}
		if err := lockAndIncrementSlot(ctx, txTS, sid); err != nil {
			return "", err
		}
		slotID = sid
	}

	var contactID interface{}
	if contactFields != nil && contactFields.ContactID != "" {
		contactID = contactFields.ContactID
	}

	var patientID string
	err = txTS.QueryRowContext(ctx, `
		INSERT INTO evt_patient (
		  event_id, therapy_id, contact_id, full_name_enc, birth_date_enc,
		  normalized_name, normalized_birthdate, complaint, preferred_time,
		  reservation_status, slot_id
		) VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,'CONFIRMED',$10)
		RETURNING id::text`,
		eventID, therapyID, contactID, encName, encBirth, patientBlindName(fullName), normBirth,
		nullStr(complaint), nullStr(preferred), slotID,
	).Scan(&patientID)
	if err != nil {
		if isDuplicateKeyErr(err) {
			return "", appErrs.BadRequest("pasien sudah terdaftar untuk acara ini")
		}
		return "", appErrs.Internal(err.Error())
	}
	if err := tx.Commit(); err != nil {
		return "", appErrs.Internal(err.Error())
	}
	action := "admin_create"
	if requirePublished {
		action = "public_register"
	}
	auditEvent(ctx, ts, nil, "patient", patientID, action, nil, map[string]any{"eventId": eventID})
	return patientID, nil
}
