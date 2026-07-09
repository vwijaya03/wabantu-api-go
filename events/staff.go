package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	encoreerrs "encore.dev/beta/errs"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pii"
)

type EventPerson struct {
	ID               string    `json:"id"`
	EventID          string    `json:"eventId"`
	FullName         string    `json:"fullName"`
	PersonType       string    `json:"personType"`
	AttendanceStatus string    `json:"attendanceStatus"`
	ArrivalTime      *string   `json:"arrivalTime,omitempty"`
	DepartureTime    *string   `json:"departureTime,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	TherapyID        *string   `json:"therapyId,omitempty"` // first therapy (compat)
	TherapyIDs       []string  `json:"therapyIds,omitempty"`
	TherapyNames     []string  `json:"therapyNames,omitempty"`
	VolunteerRoleID  *string   `json:"volunteerRoleId,omitempty"`
	IsPencatat         bool      `json:"isPencatat"`
	CountsTowardMeals  bool      `json:"countsTowardMeals"`
	AvailableFrom      *string   `json:"availableFrom,omitempty"`
	AvailableUntil     *string   `json:"availableUntil,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

type UpsertPersonParams struct {
	FullName         string   `json:"fullName"`
	Role             string   `json:"role"` // terapis|relawan|shijie|daoshi|fashi
	PersonType       string   `json:"personType,omitempty"`
	RosterID         string   `json:"rosterId,omitempty"`
	SaveToRoster     *bool    `json:"saveToRoster,omitempty"`
	AttendanceStatus string   `json:"attendanceStatus"`
	ArrivalTime      *string  `json:"arrivalTime,omitempty"`
	DepartureTime    *string  `json:"departureTime,omitempty"`
	Notes            string   `json:"notes,omitempty"`
	TherapyID        *string  `json:"therapyId,omitempty"`
	TherapyIDs       []string `json:"therapyIds,omitempty"`
	VolunteerRoleID  *string  `json:"volunteerRoleId,omitempty"`
	IsPencatat        bool     `json:"isPencatat"`
	CountsTowardMeals *bool    `json:"countsTowardMeals,omitempty"`
	AvailableFrom     *string  `json:"availableFrom,omitempty"`
	AvailableUntil    *string  `json:"availableUntil,omitempty"`
}

type ListPeopleParams struct {
	Q          string `query:"q"`
	PersonType string `query:"personType"`
	SortBy     string `query:"sortBy"`
	SortDir    string `query:"sortDir"`
	Page       int    `query:"page"`
	PageSize   int    `query:"pageSize"`
}

type ListPeopleResponse struct {
	Items []EventPerson `json:"items"`
	Total int           `json:"total"`
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/people
func ListEventPeople(ctx context.Context, eventId string, p *ListPeopleParams) (*ListPeopleResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if p == nil {
		p = &ListPeopleParams{}
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	orderBy, err := resolvePeopleOrderBy(p.SortBy, p.SortDir)
	if err != nil {
		return nil, err
	}
	conds := []string{"p.deleted_at IS NULL", "p.event_id = $1::uuid"}
	args := []any{eventId}
	i := 2
	if q := strings.TrimSpace(p.Q); q != "" {
		conds = append(conds, fmt.Sprintf("p.normalized_name = $%d", i))
		args = append(args, pii.BlindIndex(pii.NormalizeName(q), strings.TrimSpace(secrets.DataEncryptionKey)))
		i++
	}
	if pt := strings.TrimSpace(p.PersonType); pt != "" {
		conds = append(conds, fmt.Sprintf("p.person_type = $%d", i))
		args = append(args, pt)
		i++
	}
	where := strings.Join(conds, " AND ")
	inMemorySort := peopleSortNeedsInMemory(p.SortBy)

	scanPeopleRows := func(rows *sql.Rows) ([]EventPerson, error) {
		var items []EventPerson
		for rows.Next() {
			var person EventPerson
			var arr, dep, notes sql.NullString
			var nameEnc, nameLegacy sql.NullString
			if err := rows.Scan(&person.ID, &person.EventID, &nameEnc, &nameLegacy, &person.PersonType,
				&person.AttendanceStatus, &arr, &dep, &notes, &person.CountsTowardMeals, &person.CreatedAt); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			person.FullName, err = decryptPersonName(nameEnc.String, nameLegacy.String)
			if err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			if arr.Valid {
				person.ArrivalTime = &arr.String
			}
			if dep.Valid {
				person.DepartureTime = &dep.String
			}
			person.Notes = notes.String
			items = append(items, person)
		}
		if err := rows.Err(); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		for idx := range items {
			if err := loadPersonExtras(ctx, conn, items[idx].ID, &items[idx]); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			if len(items[idx].TherapyIDs) > 0 {
				items[idx].TherapyID = &items[idx].TherapyIDs[0]
			}
		}
		if items == nil {
			items = []EventPerson{}
		}
		return items, nil
	}

	if inMemorySort {
		rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT p.id::text, p.event_id::text,
		       COALESCE(p.full_name_enc,''), COALESCE(p.full_name,''),
		       p.person_type, p.attendance_status,
		       p.arrival_time::text, p.departure_time::text, COALESCE(p.notes,''),
		       p.counts_toward_meals, p.created_at
		FROM evt_event_person p
		WHERE %s %s`, where, orderBy), args...)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		defer rows.Close()
		allItems, err := scanPeopleRows(rows)
		if err != nil {
			return nil, err
		}
		sortEventPeopleInMemory(allItems, p.SortBy, p.SortDir)
		total := len(allItems)
		if off >= total {
			return &ListPeopleResponse{Items: []EventPerson{}, Total: total}, nil
		}
		end := off + lim
		if end > total {
			end = total
		}
		return &ListPeopleResponse{Items: allItems[off:end], Total: total}, nil
	}

	var total int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM evt_event_person p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, lim, off)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT p.id::text, p.event_id::text,
		       COALESCE(p.full_name_enc,''), COALESCE(p.full_name,''),
		       p.person_type, p.attendance_status,
		       p.arrival_time::text, p.departure_time::text, COALESCE(p.notes,''),
		       p.counts_toward_meals, p.created_at
		FROM evt_event_person p
		WHERE %s %s LIMIT $%d OFFSET $%d`, where, orderBy, i, i+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	items, err := scanPeopleRows(rows)
	if err != nil {
		return nil, err
	}
	return &ListPeopleResponse{Items: items, Total: total}, nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/people tag:owner
func CreateEventPerson(ctx context.Context, eventId string, p *UpsertPersonParams) (*EventPerson, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if err := validatePerson(p); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return nil, err
	}

	if rid := strings.TrimSpace(p.RosterID); rid != "" {
		rp, err := rosterEntryToPersonParams(ctx, conn, rid)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.FullName) == "" {
			p.FullName = rp.FullName
		}
		if p.Role == "" && rp.Role != "" {
			p.Role = rp.Role
		}
		if len(p.TherapyIDs) == 0 {
			p.TherapyIDs = rp.TherapyIDs
		}
		if p.VolunteerRoleID == nil {
			p.VolunteerRoleID = rp.VolunteerRoleID
		}
		if !p.IsPencatat {
			p.IsPencatat = rp.IsPencatat
		}
		if p.Notes == "" {
			p.Notes = rp.Notes
		}
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()

	var personID string
	pt := resolvePersonType(p)
	att := attendanceForDB(p.AttendanceStatus)
	nameEnc, nameIdx, encErr := encryptPersonName(p.FullName)
	if encErr != nil {
		return nil, appErrs.Internal(encErr.Error())
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO evt_event_person (event_id, full_name, full_name_enc, normalized_name, person_type, attendance_status, arrival_time, departure_time, notes, counts_toward_meals)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7::time,$8::time,$9,$10) RETURNING id::text`,
		eventId, piiPlaceholder(nameEnc), nameEnc, nameIdx, pt, att,
		nullTimeStrPtr(p.ArrivalTime), nullTimeStrPtr(p.DepartureTime), nullStr(p.Notes),
		countsTowardMealsValue(p),
	).Scan(&personID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	therapyIDs := personTherapyIDs(p)
	if isTherapyStaffPersonType(pt) {
		if err := syncPersonTherapies(ctx, tx, personID, therapyIDs, p.AvailableFrom, p.AvailableUntil); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if pt == "VOLUNTEER" {
		if err := syncPersonVolunteer(ctx, tx, personID, p.VolunteerRoleID, p.IsPencatat); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if shouldSaveToRoster(p) {
		_, _ = upsertStaffRoster(ctx, conn, p)
	}
	auditEvent(ctx, conn, u, "event_person", personID, "create", nil, p)
	out := &EventPerson{
		ID: personID, EventID: eventId, FullName: p.FullName,
		PersonType: pt, AttendanceStatus: att,
		TherapyIDs: therapyIDs, VolunteerRoleID: p.VolunteerRoleID, IsPencatat: p.IsPencatat,
	}
	if len(therapyIDs) > 0 {
		out.TherapyID = &therapyIDs[0]
	}
	return out, nil
}

func personTherapyIDs(p *UpsertPersonParams) []string {
	if p == nil {
		return nil
	}
	if len(p.TherapyIDs) > 0 {
		return p.TherapyIDs
	}
	if p.TherapyID != nil && strings.TrimSpace(*p.TherapyID) != "" {
		return []string{strings.TrimSpace(*p.TherapyID)}
	}
	return nil
}

//encore:api auth method=PUT path=/api/v1/events/detail/:eventId/people/:personId tag:owner
func UpdateEventPerson(ctx context.Context, eventId, personId string, p *UpsertPersonParams) (*EventPerson, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if err := validatePerson(p); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return nil, err
	}
	pt := resolvePersonType(p)
	att := attendanceForDB(p.AttendanceStatus)
	nameEnc, nameIdx, encErr := encryptPersonName(p.FullName)
	if encErr != nil {
		return nil, appErrs.Internal(encErr.Error())
	}
	_, err = conn.ExecContext(ctx, `
		UPDATE evt_event_person SET full_name=$1, full_name_enc=$2, normalized_name=$3,
		  person_type=$4, attendance_status=$5,
		  arrival_time=$6::time, departure_time=$7::time, notes=$8,
		  counts_toward_meals=$9, updated_at=now()
		WHERE id=$10::uuid AND event_id=$11::uuid AND deleted_at IS NULL`,
		piiPlaceholder(nameEnc), nameEnc, nameIdx, pt, att,
		nullTimeStrPtr(p.ArrivalTime), nullTimeStrPtr(p.DepartureTime), nullStr(p.Notes),
		countsTowardMealsValue(p), personId, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	therapyIDs := personTherapyIDs(p)
	if isTherapyStaffPersonType(pt) {
		if err := syncPersonTherapies(ctx, conn, personId, therapyIDs, p.AvailableFrom, p.AvailableUntil); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		_, _ = conn.ExecContext(ctx, `DELETE FROM evt_event_volunteer WHERE person_id=$1::uuid`, personId)
	}
	if pt == "VOLUNTEER" {
		if err := syncPersonVolunteer(ctx, conn, personId, p.VolunteerRoleID, p.IsPencatat); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		_, _ = conn.ExecContext(ctx, `DELETE FROM evt_person_therapy WHERE person_id=$1::uuid`, personId)
	}
	if shouldSaveToRoster(p) {
		_, _ = upsertStaffRoster(ctx, conn, p)
	}
	auditEvent(ctx, conn, u, "event_person", personId, "update", nil, p)
	resp, _ := ListEventPeople(ctx, eventId, &ListPeopleParams{Page: 1, PageSize: 1000})
	for _, it := range resp.Items {
		if it.ID == personId {
			return &it, nil
		}
	}
	return &EventPerson{ID: personId, EventID: eventId}, nil
}

//encore:api auth method=DELETE path=/api/v1/events/detail/:eventId/people/:personId tag:owner
func DeleteEventPerson(ctx context.Context, eventId, personId string) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `
		UPDATE evt_event_person SET deleted_at=now()
		WHERE id=$1::uuid AND event_id=$2::uuid AND deleted_at IS NULL`, personId, eventId)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, conn, u, "event_person", personId, "delete", nil, nil)
	return nil
}

type DeletePeopleParams struct {
	PersonIDs []string `json:"personIds"`
}

type DeletePeopleResponse struct {
	Deleted int      `json:"deleted"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

func deleteEventPersonInTx(ctx context.Context, tx *sql.Tx, eventId, personId string) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE evt_event_person SET deleted_at=now(), updated_at=now()
		WHERE id=$1::uuid AND event_id=$2::uuid AND deleted_at IS NULL`, personId, eventId)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return appErrs.NotFound("staf tidak ditemukan")
	}
	return nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/people/delete-bulk tag:owner
func DeleteEventPeopleBulk(ctx context.Context, eventId string, p *DeletePeopleParams) (*DeletePeopleResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil || len(p.PersonIDs) == 0 {
		return nil, appErrs.BadRequest("pilih minimal satu staf")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return nil, err
	}

	resp := &DeletePeopleResponse{Errors: []string{}}
	seen := map[string]bool{}
	for _, rawID := range p.PersonIDs {
		personID := strings.TrimSpace(rawID)
		if personID == "" || seen[personID] {
			continue
		}
		seen[personID] = true

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if err := deleteEventPersonInTx(ctx, tx, eventId, personID); err != nil {
			_ = tx.Rollback()
			resp.Failed++
			if encErr, ok := err.(*encoreerrs.Error); ok && encErr.Code == encoreerrs.NotFound {
				resp.Errors = append(resp.Errors, personID+": staf tidak ditemukan")
			} else {
				resp.Errors = append(resp.Errors, personID+": "+err.Error())
			}
			continue
		}
		if err := tx.Commit(); err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, personID+": gagal menghapus")
			continue
		}
		resp.Deleted++
		auditEvent(ctx, conn, u, "event_person", personID, "delete_bulk_item", map[string]any{"eventId": eventId}, nil)
	}
	if len(resp.Errors) == 0 {
		resp.Errors = nil
	}
	return resp, nil
}

func validatePerson(p *UpsertPersonParams) error {
	p.FullName = clampLen(p.FullName, maxPersonNameLen)
	p.Notes = clampLen(p.Notes, maxNotesLen)
	if strings.TrimSpace(p.FullName) == "" {
		return appErrs.BadRequest("nama wajib diisi")
	}
	pt := resolvePersonType(p)
	valid := map[string]bool{"THERAPIST": true, "SHIJIE": true, "VOLUNTEER": true, "DAOSHI": true, "FASHI": true}
	if !valid[pt] {
		return appErrs.BadRequest("peran tidak valid")
	}
	p.PersonType = pt
	if isTherapyStaffPersonType(pt) && len(personTherapyIDs(p)) == 0 {
		return appErrs.BadRequest("pilih minimal satu terapi untuk peran terapis/shijie/daoshi/fashi")
	}
	if pt == "VOLUNTEER" && (p.VolunteerRoleID == nil || strings.TrimSpace(*p.VolunteerRoleID) == "") {
		return appErrs.BadRequest("peran relawan wajib dipilih")
	}
	att := strings.ToUpper(strings.TrimSpace(p.AttendanceStatus))
	if att == "" {
		att = "PRESENT"
	}
	validAtt := map[string]bool{"PRESENT": true, "PARTIAL": true, "ABSENT": true, "NOT_PRESENT": true}
	if !validAtt[att] {
		return appErrs.BadRequest("status kehadiran tidak valid")
	}
	p.AttendanceStatus = att
	return nil
}

func nullTimeStrPtr(s *string) interface{} {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return strings.TrimSpace(*s)
}

type Assignment struct {
	ID          string  `json:"id"`
	EventID     string  `json:"eventId"`
	TaskID      string  `json:"taskId"`
	TaskName    string  `json:"taskName,omitempty"`
	PersonID    string  `json:"personId"`
	PersonName  string  `json:"personName,omitempty"`
	StartTime   *string `json:"startTime,omitempty"`
	EndTime     *string `json:"endTime,omitempty"`
	SessionName *string `json:"sessionName,omitempty"`
}

type UpsertAssignmentParams struct {
	TaskID      string  `json:"taskId"`
	PersonID    string  `json:"personId"`
	StartTime   *string `json:"startTime,omitempty"`
	EndTime     *string `json:"endTime,omitempty"`
	SessionName *string `json:"sessionName,omitempty"`
}

type ListAssignmentsParams struct {
	Q        string `query:"q"`
	SortBy   string `query:"sortBy"`
	SortDir  string `query:"sortDir"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/assignments
func ListAssignments(ctx context.Context, eventId string, p *ListAssignmentsParams) (*ListAssignmentsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if p == nil {
		p = &ListAssignmentsParams{}
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	orderBy, err := resolveAssignmentOrderBy(p.SortBy, p.SortDir)
	if err != nil {
		return nil, err
	}
	inMemorySort := assignmentSortNeedsInMemory(p.SortBy)
	conds := []string{"a.deleted_at IS NULL", "a.event_id = $1::uuid"}
	args := []any{eventId}
	i := 2
	if q := strings.TrimSpace(p.Q); q != "" {
		if nameCond, nameArg := personNameSearchCondition("p", i, q); nameArg != nil {
			conds = append(conds, fmt.Sprintf("(%s OR tk.task_name ILIKE $%d)", nameCond, i+1))
			args = append(args, nameArg, "%"+q+"%")
			i += 2
		} else {
			conds = append(conds, fmt.Sprintf("tk.task_name ILIKE $%d", i))
			args = append(args, "%"+q+"%")
			i++
		}
	}
	where := strings.Join(conds, " AND ")
	var total int
	countQ := fmt.Sprintf(`
		SELECT COUNT(*) FROM evt_event_assignment a
		JOIN evt_task tk ON tk.id = a.task_id
		JOIN evt_event_person p ON p.id = a.person_id
		WHERE %s`, where)
	if err := conn.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	scanAssignmentRows := func(rows *sql.Rows) ([]Assignment, error) {
		var items []Assignment
		for rows.Next() {
			var a Assignment
			var st, en, sn sql.NullString
			var nameEnc, nameLegacy string
			if err := rows.Scan(&a.ID, &a.EventID, &a.TaskID, &a.TaskName, &a.PersonID, &nameEnc, &nameLegacy, &st, &en, &sn); err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			if st.Valid {
				a.StartTime = &st.String
			}
			if en.Valid {
				a.EndTime = &en.String
			}
			if sn.Valid {
				a.SessionName = &sn.String
			}
			a.PersonName, err = scanPersonNameFromRow(nameEnc, nameLegacy)
			if err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			items = append(items, a)
		}
		if err := rows.Err(); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if items == nil {
			items = []Assignment{}
		}
		return items, nil
	}

	if inMemorySort {
		listQ := fmt.Sprintf(`
		SELECT a.id::text, a.event_id::text, a.task_id::text, tk.task_name,
		       a.person_id::text, `+personNameEncLegacyColsP+`,
		       a.start_time::text, a.end_time::text, a.session_name
		FROM evt_event_assignment a
		JOIN evt_task tk ON tk.id = a.task_id
		JOIN evt_event_person p ON p.id = a.person_id
		WHERE %s %s`, where, orderBy)
		rows, err := conn.QueryContext(ctx, listQ, args...)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		defer rows.Close()
		allItems, err := scanAssignmentRows(rows)
		if err != nil {
			return nil, err
		}
		sortAssignmentsInMemory(allItems, p.SortBy, p.SortDir)
		total = len(allItems)
		if off >= total {
			return &ListAssignmentsResponse{Items: []Assignment{}, Total: total}, nil
		}
		end := off + lim
		if end > total {
			end = total
		}
		return &ListAssignmentsResponse{Items: allItems[off:end], Total: total}, nil
	}

	args = append(args, lim, off)
	listQ := fmt.Sprintf(`
		SELECT a.id::text, a.event_id::text, a.task_id::text, tk.task_name,
		       a.person_id::text, `+personNameEncLegacyColsP+`,
		       a.start_time::text, a.end_time::text, a.session_name
		FROM evt_event_assignment a
		JOIN evt_task tk ON tk.id = a.task_id
		JOIN evt_event_person p ON p.id = a.person_id
		WHERE %s
		%s
		LIMIT $%d OFFSET $%d`, where, orderBy, i, i+1)
	rows, err := conn.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	items, err := scanAssignmentRows(rows)
	if err != nil {
		return nil, err
	}
	return &ListAssignmentsResponse{Items: items, Total: total}, nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/assignments tag:owner
func CreateAssignment(ctx context.Context, eventId string, p *UpsertAssignmentParams) (*Assignment, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.TaskID) == "" || strings.TrimSpace(p.PersonID) == "" {
		return nil, appErrs.BadRequest("tugas dan orang wajib dipilih")
	}
	var personOK, taskOK bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM evt_event_person WHERE id=$1::uuid AND event_id=$2::uuid AND deleted_at IS NULL)`,
		p.PersonID, eventId).Scan(&personOK); err != nil || !personOK {
		return nil, appErrs.BadRequest("orang tidak ditemukan di acara ini")
	}
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM evt_task WHERE id=$1::uuid AND deleted_at IS NULL)`,
		p.TaskID).Scan(&taskOK); err != nil || !taskOK {
		return nil, appErrs.BadRequest("tugas tidak valid")
	}
	var id string
	err = conn.QueryRowContext(ctx, `
		INSERT INTO evt_event_assignment (event_id, task_id, person_id, start_time, end_time, session_name)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4::time,$5::time,$6) RETURNING id::text`,
		eventId, p.TaskID, p.PersonID,
		nullTimeStrPtr(p.StartTime), nullTimeStrPtr(p.EndTime), nullStrPtr(p.SessionName),
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, conn, u, "assignment", id, "create", nil, p)
	return &Assignment{ID: id, EventID: eventId, TaskID: p.TaskID, PersonID: p.PersonID}, nil
}

//encore:api auth method=PUT path=/api/v1/events/detail/:eventId/assignments/:assignmentId tag:owner
func UpdateAssignment(ctx context.Context, eventId, assignmentId string, p *UpsertAssignmentParams) (*Assignment, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.TaskID) == "" || strings.TrimSpace(p.PersonID) == "" {
		return nil, appErrs.BadRequest("tugas dan orang wajib dipilih")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return nil, err
	}
	res, err := conn.ExecContext(ctx, `
		UPDATE evt_event_assignment SET
		  task_id=$1::uuid, person_id=$2::uuid,
		  start_time=$3::time, end_time=$4::time, session_name=$5,
		  updated_at=now()
		WHERE id=$6::uuid AND event_id=$7::uuid AND deleted_at IS NULL`,
		p.TaskID, p.PersonID,
		nullTimeStrPtr(p.StartTime), nullTimeStrPtr(p.EndTime), nullStrPtr(p.SessionName),
		assignmentId, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, appErrs.NotFound("penugasan tidak ditemukan")
	}
	auditEvent(ctx, conn, u, "assignment", assignmentId, "update", nil, p)
	var a Assignment
	var st, en, sn sql.NullString
	var nameEnc, nameLegacy string
	err = conn.QueryRowContext(ctx, `
		SELECT a.id::text, a.event_id::text, a.task_id::text, tk.task_name,
		       a.person_id::text, `+personNameEncLegacyColsP+`, a.start_time::text, a.end_time::text, a.session_name
		FROM evt_event_assignment a
		JOIN evt_task tk ON tk.id = a.task_id
		JOIN evt_event_person p ON p.id = a.person_id
		WHERE a.id=$1::uuid AND a.event_id=$2::uuid`, assignmentId, eventId,
	).Scan(&a.ID, &a.EventID, &a.TaskID, &a.TaskName, &a.PersonID, &nameEnc, &nameLegacy, &st, &en, &sn)
	if err == nil {
		a.PersonName, _ = scanPersonNameFromRow(nameEnc, nameLegacy)
	}
	if err != nil {
		return &Assignment{ID: assignmentId, EventID: eventId}, nil
	}
	if st.Valid {
		a.StartTime = &st.String
	}
	if en.Valid {
		a.EndTime = &en.String
	}
	if sn.Valid {
		a.SessionName = &sn.String
	}
	return &a, nil
}

//encore:api auth method=DELETE path=/api/v1/events/detail/:eventId/assignments/:assignmentId tag:owner
func DeleteAssignment(ctx context.Context, eventId, assignmentId string) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `
		UPDATE evt_event_assignment SET deleted_at=now()
		WHERE id=$1::uuid AND event_id=$2::uuid`, assignmentId, eventId)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, conn, u, "assignment", assignmentId, "delete", nil, nil)
	return nil
}

func nullStrPtr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return nullStr(*s)
}
