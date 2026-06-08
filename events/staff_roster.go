package events

import (
	"context"
	"database/sql"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type StaffRosterEntry struct {
	ID              string   `json:"id"`
	FullName        string   `json:"fullName"`
	PersonType      string   `json:"personType"`
	Role            string   `json:"role,omitempty"`
	AttendanceStatus string  `json:"attendanceStatus,omitempty"`
	TherapyIDs      []string `json:"therapyIds,omitempty"`
	TherapyNames    []string `json:"therapyNames,omitempty"`
	VolunteerRoleID *string  `json:"volunteerRoleId,omitempty"`
	IsPencatat      bool     `json:"isPencatat"`
	Notes           string   `json:"notes,omitempty"`
}

type ListStaffRosterResponse struct {
	Items []StaffRosterEntry `json:"items"`
	Total int                `json:"total"`
}

type ImportStaffRosterResponse struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

type SyncStaffRosterFromEventResponse struct {
	Upserted int `json:"upserted"`
}

func normalizeRosterName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func isBadConnectionErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "bad connection")
}

//encore:api auth method=GET path=/api/v1/events/staff-roster
func ListStaffRoster(ctx context.Context) (*ListStaffRosterResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	items, err := listStaffRosterWithConn(ctx, u.TenantSchema)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "bad connection") {
		items, err = listStaffRosterWithConn(ctx, u.TenantSchema)
	}
	if err != nil {
		return nil, err
	}
	return &ListStaffRosterResponse{Items: items, Total: len(items)}, nil
}

func listStaffRosterWithConn(ctx context.Context, schema string) ([]StaffRosterEntry, error) {
	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	rows, err := conn.QueryContext(ctx, `
		SELECT id::text, full_name, person_type, COALESCE(notes,'')
		FROM evt_staff_roster
		WHERE deleted_at IS NULL
		ORDER BY person_type, full_name`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []StaffRosterEntry
	for rows.Next() {
		var e StaffRosterEntry
		if err := rows.Scan(&e.ID, &e.FullName, &e.PersonType, &e.Notes); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		e.Role = personTypeToRole(e.PersonType)
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := attachRosterExtrasBatch(ctx, conn, items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []StaffRosterEntry{}
	}
	return items, nil
}

func attachRosterExtrasBatch(ctx context.Context, conn *sql.Conn, items []StaffRosterEntry) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[string]*StaffRosterEntry, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT rt.roster_id::text, rt.therapy_id::text, t.therapy_name
		FROM evt_staff_roster_therapy rt
		JOIN evt_therapy t ON t.id = rt.therapy_id
		JOIN evt_staff_roster r ON r.id = rt.roster_id AND r.deleted_at IS NULL
		ORDER BY rt.roster_id, t.display_order`)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var rid, tid, tname string
		if err := rows.Scan(&rid, &tid, &tname); err != nil {
			return appErrs.Internal(err.Error())
		}
		if e := byID[rid]; e != nil {
			e.TherapyIDs = append(e.TherapyIDs, tid)
			e.TherapyNames = append(e.TherapyNames, tname)
		}
	}
	if err := rows.Err(); err != nil {
		return appErrs.Internal(err.Error())
	}

	vrows, err := conn.QueryContext(ctx, `
		SELECT rv.roster_id::text, rv.volunteer_role_id::text, rv.is_pencatat
		FROM evt_staff_roster_volunteer rv
		JOIN evt_staff_roster r ON r.id = rv.roster_id AND r.deleted_at IS NULL`)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer vrows.Close()
	for vrows.Next() {
		var rid string
		var volRole sql.NullString
		var isPencatat bool
		if err := vrows.Scan(&rid, &volRole, &isPencatat); err != nil {
			return appErrs.Internal(err.Error())
		}
		if e := byID[rid]; e != nil {
			if volRole.Valid {
				e.VolunteerRoleID = &volRole.String
			}
			e.IsPencatat = isPencatat
		}
	}
	return vrows.Err()
}

func loadRosterExtras(ctx context.Context, conn *sql.Conn, e *StaffRosterEntry) error {
	rows, err := conn.QueryContext(ctx, `
		SELECT rt.therapy_id::text, t.therapy_name
		FROM evt_staff_roster_therapy rt
		JOIN evt_therapy t ON t.id = rt.therapy_id
		WHERE rt.roster_id = $1::uuid
		ORDER BY t.display_order`, e.ID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var tid, tname string
		if err := rows.Scan(&tid, &tname); err != nil {
			return appErrs.Internal(err.Error())
		}
		e.TherapyIDs = append(e.TherapyIDs, tid)
		e.TherapyNames = append(e.TherapyNames, tname)
	}
	var volRole sql.NullString
	var isPencatat bool
	err = conn.QueryRowContext(ctx, `
		SELECT volunteer_role_id::text, is_pencatat
		FROM evt_staff_roster_volunteer WHERE roster_id=$1::uuid`, e.ID,
	).Scan(&volRole, &isPencatat)
	if err == nil {
		if volRole.Valid {
			e.VolunteerRoleID = &volRole.String
		}
		e.IsPencatat = isPencatat
	} else if err != sql.ErrNoRows {
		return appErrs.Internal(err.Error())
	}
	return rows.Err()
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/people/import-roster tag:owner
func ImportStaffRosterToEvent(ctx context.Context, eventId string) (*ImportStaffRosterResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	// Retry once on transient DB connection drops.
	run := func() (*ImportStaffRosterResponse, error) {
		conn, err := tenantConn(ctx, u.TenantSchema)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		defer conn.Close()
		if err := assertEventMutable(ctx, conn, eventId); err != nil {
			return nil, err
		}
		added, skipped, err := importAllRosterToEvent(ctx, conn, eventId)
		if err != nil {
			return nil, err
		}
		return &ImportStaffRosterResponse{Added: added, Skipped: skipped}, nil
	}
	resp, err := run()
	if isBadConnectionErr(err) {
		resp, err = run()
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

//encore:api auth method=POST path=/api/v1/events/staff-roster/sync-from-event/:eventId tag:owner
func SyncStaffRosterFromEvent(ctx context.Context, eventId string) (*SyncStaffRosterFromEventResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	run := func() (*SyncStaffRosterFromEventResponse, error) {
		conn, err := tenantConn(ctx, u.TenantSchema)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		defer conn.Close()
		if err := assertEventExists(ctx, conn, eventId); err != nil {
			return nil, err
		}
		upserted, err := syncAllEventPeopleToRoster(ctx, conn, eventId)
		if err != nil {
			return nil, err
		}
		return &SyncStaffRosterFromEventResponse{Upserted: upserted}, nil
	}
	resp, err := run()
	if isBadConnectionErr(err) {
		resp, err = run()
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

type eventPersonRosterSync struct {
	id               string
	fullName         string
	personType       string
	attendanceStatus string
	notes            string
	therapyIDs       []string
	volunteerRoleID  *string
	isPencatat       bool
}

func loadEventPeopleForRosterSync(ctx context.Context, conn *sql.Conn, eventID string) ([]eventPersonRosterSync, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT id::text, full_name, person_type, attendance_status, COALESCE(notes,'')
		FROM evt_event_person
		WHERE event_id=$1::uuid AND deleted_at IS NULL
		ORDER BY person_type, full_name`, eventID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	var people []eventPersonRosterSync
	byID := make(map[string]*eventPersonRosterSync)
	for rows.Next() {
		var p eventPersonRosterSync
		if err := rows.Scan(&p.id, &p.fullName, &p.personType, &p.attendanceStatus, &p.notes); err != nil {
			_ = rows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		people = append(people, p)
		byID[p.id] = &people[len(people)-1]
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := rows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if len(people) == 0 {
		return people, nil
	}

	trows, err := conn.QueryContext(ctx, `
		SELECT pt.person_id::text, pt.therapy_id::text
		FROM evt_person_therapy pt
		JOIN evt_event_person p ON p.id = pt.person_id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		ORDER BY pt.person_id`, eventID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for trows.Next() {
		var pid, tid string
		if err := trows.Scan(&pid, &tid); err != nil {
			_ = trows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		if p := byID[pid]; p != nil {
			p.therapyIDs = append(p.therapyIDs, tid)
		}
	}
	if err := trows.Err(); err != nil {
		_ = trows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := trows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	vrows, err := conn.QueryContext(ctx, `
		SELECT ev.person_id::text, ev.volunteer_role_id::text, ev.is_pencatat
		FROM evt_event_volunteer ev
		JOIN evt_event_person p ON p.id = ev.person_id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL`, eventID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for vrows.Next() {
		var pid string
		var volRole sql.NullString
		var isPencatat bool
		if err := vrows.Scan(&pid, &volRole, &isPencatat); err != nil {
			_ = vrows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		if p := byID[pid]; p != nil {
			if volRole.Valid {
				p.volunteerRoleID = &volRole.String
			}
			p.isPencatat = isPencatat
		}
	}
	if err := vrows.Err(); err != nil {
		_ = vrows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := vrows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return people, nil
}

func syncAllEventPeopleToRoster(ctx context.Context, conn *sql.Conn, eventID string) (int, error) {
	people, err := loadEventPeopleForRosterSync(ctx, conn, eventID)
	if err != nil {
		return 0, err
	}
	upserted := 0
	for _, person := range people {
		p := &UpsertPersonParams{
			FullName:         person.fullName,
			PersonType:       person.personType,
			Role:             personTypeToRole(person.personType),
			AttendanceStatus: person.attendanceStatus,
			Notes:            person.notes,
			TherapyIDs:       person.therapyIDs,
			VolunteerRoleID:  person.volunteerRoleID,
			IsPencatat:       person.isPencatat,
		}
		if _, err := upsertStaffRoster(ctx, conn, p); err == nil {
			upserted++
		}
	}
	return upserted, nil
}

func importAllRosterToEvent(ctx context.Context, conn *sql.Conn, eventID string) (added, skipped int, err error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT id::text FROM evt_staff_roster WHERE deleted_at IS NULL ORDER BY person_type, full_name`)
	if err != nil {
		return 0, 0, appErrs.Internal(err.Error())
	}
	var rosterIDs []string
	for rows.Next() {
		var rosterID string
		if err := rows.Scan(&rosterID); err != nil {
			return added, skipped, appErrs.Internal(err.Error())
		}
		rosterIDs = append(rosterIDs, rosterID)
	}
	if err := rows.Err(); err != nil {
		return added, skipped, appErrs.Internal(err.Error())
	}
	if err := rows.Close(); err != nil {
		return added, skipped, appErrs.Internal(err.Error())
	}
	for _, rosterID := range rosterIDs {
		ok, err := addRosterMemberToEvent(ctx, conn, eventID, rosterID)
		if err != nil {
			return added, skipped, err
		}
		if ok {
			added++
		} else {
			skipped++
		}
	}
	return added, skipped, nil
}

func addRosterMemberToEvent(ctx context.Context, conn *sql.Conn, eventID, rosterID string) (bool, error) {
	var fullName, pt string
	if err := conn.QueryRowContext(ctx, `
		SELECT full_name, person_type FROM evt_staff_roster
		WHERE id=$1::uuid AND deleted_at IS NULL`, rosterID,
	).Scan(&fullName, &pt); err == sql.ErrNoRows {
		return false, appErrs.NotFound("anggota roster tidak ditemukan")
	} else if err != nil {
		return false, appErrs.Internal(err.Error())
	}
	norm := normalizeRosterName(fullName)
	var exists bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM evt_event_person
		  WHERE event_id=$1::uuid AND deleted_at IS NULL
		    AND person_type=$2
		    AND lower(regexp_replace(trim(full_name), '\s+', ' ', 'g')) = $3
		)`, eventID, pt, norm).Scan(&exists); err != nil {
		return false, appErrs.Internal(err.Error())
	}
	if exists {
		return false, nil
	}
	var e StaffRosterEntry
	e.ID = rosterID
	e.FullName = fullName
	e.PersonType = pt
	if err := loadRosterExtras(ctx, conn, &e); err != nil {
		return false, err
	}
	p := &UpsertPersonParams{
		FullName:         fullName,
		Role:             personTypeToRole(pt),
		PersonType:       pt,
		AttendanceStatus: "PRESENT",
		TherapyIDs:       e.TherapyIDs,
		VolunteerRoleID:  e.VolunteerRoleID,
		IsPencatat:       e.IsPencatat,
		Notes:            e.Notes,
	}
	if err := createPersonInEvent(ctx, conn, eventID, p); err != nil {
		return false, err
	}
	return true, nil
}

func createPersonInEvent(ctx context.Context, conn *sql.Conn, eventID string, p *UpsertPersonParams) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer tx.Rollback()
	var personID string
	pt := resolvePersonType(p)
	att := attendanceForDB(p.AttendanceStatus)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO evt_event_person (event_id, full_name, person_type, attendance_status, arrival_time, departure_time, notes)
		VALUES ($1::uuid,$2,$3,$4,$5::time,$6::time,$7) RETURNING id::text`,
		eventID, strings.TrimSpace(p.FullName), pt, att,
		nullTimeStrPtr(p.ArrivalTime), nullTimeStrPtr(p.DepartureTime), nullStr(p.Notes),
	).Scan(&personID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	therapyIDs := personTherapyIDs(p)
	if isTherapyStaffPersonType(pt) {
		if err := syncPersonTherapies(ctx, tx, personID, therapyIDs, p.AvailableFrom, p.AvailableUntil); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	if pt == "VOLUNTEER" {
		if err := syncPersonVolunteer(ctx, tx, personID, p.VolunteerRoleID, p.IsPencatat); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return tx.Commit()
}

func upsertStaffRoster(ctx context.Context, conn *sql.Conn, p *UpsertPersonParams) (string, error) {
	if p == nil || strings.TrimSpace(p.FullName) == "" {
		return "", nil
	}
	pt := resolvePersonType(p)
	norm := normalizeRosterName(p.FullName)
	var rosterID string
	err := conn.QueryRowContext(ctx, `
		SELECT id::text FROM evt_staff_roster
		WHERE normalized_name=$1 AND person_type=$2 AND deleted_at IS NULL`, norm, pt,
	).Scan(&rosterID)
	if err == sql.ErrNoRows {
		err = conn.QueryRowContext(ctx, `
			INSERT INTO evt_staff_roster (full_name, normalized_name, person_type, notes)
			VALUES ($1,$2,$3,$4) RETURNING id::text`,
			strings.TrimSpace(p.FullName), norm, pt, nullStr(p.Notes),
		).Scan(&rosterID)
	} else if err == nil {
		_, err = conn.ExecContext(ctx, `
			UPDATE evt_staff_roster SET full_name=$1, notes=$2, updated_at=now()
			WHERE id=$3::uuid`, strings.TrimSpace(p.FullName), nullStr(p.Notes), rosterID)
	}
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	_, _ = conn.ExecContext(ctx, `DELETE FROM evt_staff_roster_therapy WHERE roster_id=$1::uuid`, rosterID)
	_, _ = conn.ExecContext(ctx, `DELETE FROM evt_staff_roster_volunteer WHERE roster_id=$1::uuid`, rosterID)
	therapyIDs := personTherapyIDs(p)
	if isTherapyStaffPersonType(pt) {
		for _, tid := range therapyIDs {
			_, _ = conn.ExecContext(ctx, `
				INSERT INTO evt_staff_roster_therapy (roster_id, therapy_id)
				VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, rosterID, tid)
		}
	}
	if pt == "VOLUNTEER" {
		var volID interface{}
		if p.VolunteerRoleID != nil && strings.TrimSpace(*p.VolunteerRoleID) != "" {
			volID = *p.VolunteerRoleID
		}
		_, _ = conn.ExecContext(ctx, `
			INSERT INTO evt_staff_roster_volunteer (roster_id, volunteer_role_id, is_pencatat)
			VALUES ($1::uuid,$2::uuid,$3)
			ON CONFLICT (roster_id) DO UPDATE SET
			  volunteer_role_id=EXCLUDED.volunteer_role_id,
			  is_pencatat=EXCLUDED.is_pencatat`, rosterID, volID, p.IsPencatat)
	}
	return rosterID, nil
}

func rosterEntryToPersonParams(ctx context.Context, conn *sql.Conn, rosterID string) (*UpsertPersonParams, error) {
	var e StaffRosterEntry
	if err := conn.QueryRowContext(ctx, `
		SELECT id::text, full_name, person_type, COALESCE(notes,'')
		FROM evt_staff_roster WHERE id=$1::uuid AND deleted_at IS NULL`, rosterID,
	).Scan(&e.ID, &e.FullName, &e.PersonType, &e.Notes); err == sql.ErrNoRows {
		return nil, appErrs.NotFound("roster tidak ditemukan")
	} else if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := loadRosterExtras(ctx, conn, &e); err != nil {
		return nil, err
	}
	return &UpsertPersonParams{
		FullName:         e.FullName,
		Role:             personTypeToRole(e.PersonType),
		PersonType:       e.PersonType,
		AttendanceStatus: "PRESENT",
		TherapyIDs:       e.TherapyIDs,
		VolunteerRoleID:  e.VolunteerRoleID,
		IsPencatat:       e.IsPencatat,
		Notes:            e.Notes,
	}, nil
}

func shouldSaveToRoster(p *UpsertPersonParams) bool {
	if p == nil || p.SaveToRoster != nil && !*p.SaveToRoster {
		return false
	}
	return true
}
