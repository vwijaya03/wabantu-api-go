package events

import (
	"context"
	"database/sql"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

// isTherapyStaffPersonType — terapis, shijie, daoshi, fashi (bukan relawan).
func isTherapyStaffPersonType(pt string) bool {
	switch strings.ToUpper(strings.TrimSpace(pt)) {
	case "THERAPIST", "SHIJIE", "DAOSHI", "FASHI":
		return true
	default:
		return false
	}
}

// roleUsesTherapies maps UI/API role slug to therapy assignment.
func roleUsesTherapies(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "terapis", "therapist", "shijie", "daoshi", "fashi":
		return true
	default:
		return false
	}
}

func personTypeToRole(pt string) string {
	switch strings.ToUpper(strings.TrimSpace(pt)) {
	case "THERAPIST":
		return "terapis"
	case "VOLUNTEER":
		return "relawan"
	case "SHIJIE":
		return "shijie"
	case "DAOSHI":
		return "daoshi"
	case "FASHI":
		return "fashi"
	default:
		return strings.ToLower(pt)
	}
}

func resolvePersonType(p *UpsertPersonParams) string {
	if p == nil {
		return ""
	}
	if r := strings.TrimSpace(p.Role); r != "" {
		switch strings.ToLower(r) {
		case "terapis", "therapist":
			return "THERAPIST"
		case "relawan", "volunteer":
			return "VOLUNTEER"
		case "shijie":
			return "SHIJIE"
		case "daoshi":
			return "DAOSHI"
		case "fashi":
			return "FASHI"
		}
	}
	return strings.ToUpper(strings.TrimSpace(p.PersonType))
}

func attendanceForDB(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "PRESENT"
	}
	if s == "ABSENT" {
		return "NOT_PRESENT"
	}
	return s
}

func syncPersonTherapies(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, personID string, therapyIDs []string, availableFrom, availableUntil *string) error {
	_, err := exec.ExecContext(ctx, `DELETE FROM evt_person_therapy WHERE person_id=$1::uuid`, personID)
	if err != nil {
		return err
	}
	for _, tid := range therapyIDs {
		tid = strings.TrimSpace(tid)
		if tid == "" {
			continue
		}
		_, err := exec.ExecContext(ctx, `
			INSERT INTO evt_person_therapy (person_id, therapy_id, available_from, available_until)
			VALUES ($1::uuid,$2::uuid,$3::time,$4::time)
			ON CONFLICT (person_id, therapy_id) DO UPDATE SET
			  available_from=EXCLUDED.available_from, available_until=EXCLUDED.available_until`,
			personID, tid, nullTimeStrPtr(availableFrom), nullTimeStrPtr(availableUntil))
		if err != nil {
			return err
		}
	}
	return nil
}

func syncPersonVolunteer(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, personID string, roleID *string, isPencatat bool) error {
	_, err := exec.ExecContext(ctx, `DELETE FROM evt_event_volunteer WHERE person_id=$1::uuid`, personID)
	if err != nil {
		return err
	}
	if roleID == nil || strings.TrimSpace(*roleID) == "" {
		return nil
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO evt_event_volunteer (person_id, volunteer_role_id, is_pencatat)
		VALUES ($1::uuid,$2::uuid,$3)`,
		personID, strings.TrimSpace(*roleID), isPencatat)
	return err
}

func loadPersonExtras(ctx context.Context, conn *sql.Conn, personID string, person *EventPerson) error {
	rows, err := conn.QueryContext(ctx, `
		SELECT pt.therapy_id::text, t.therapy_name
		FROM evt_person_therapy pt
		JOIN evt_therapy t ON t.id = pt.therapy_id
		WHERE pt.person_id=$1::uuid
		ORDER BY t.display_order`, personID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			_ = rows.Close()
			return err
		}
		person.TherapyIDs = append(person.TherapyIDs, id)
		person.TherapyNames = append(person.TherapyNames, name)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var rid sql.NullString
	var pencatat bool
	err = conn.QueryRowContext(ctx, `
		SELECT volunteer_role_id::text, is_pencatat FROM evt_event_volunteer WHERE person_id=$1::uuid`, personID,
	).Scan(&rid, &pencatat)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if rid.Valid {
		person.VolunteerRoleID = &rid.String
	}
	person.IsPencatat = pencatat
	af, au, err := loadPersonPartialTimes(ctx, conn, personID)
	if err != nil {
		return err
	}
	if af != nil {
		person.AvailableFrom = af
	}
	if au != nil {
		person.AvailableUntil = au
	}
	return nil
}

func loadPersonPartialTimes(ctx context.Context, conn *sql.Conn, personID string) (*string, *string, error) {
	var af, au sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT available_from::text, available_until::text
		FROM evt_person_therapy WHERE person_id=$1::uuid LIMIT 1`, personID,
	).Scan(&af, &au)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var from, until *string
	if af.Valid {
		from = &af.String
	}
	if au.Valid {
		until = &au.String
	}
	return from, until, nil
}

func therapyLookupCandidates(name string) []string {
	name = strings.TrimSpace(name)
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add(name)
	if i := strings.Index(name, "("); i > 0 {
		add(name[:i])
	}
	return out
}

// resolveTherapyIDByName maps OCR/vision labels (often with extra capacity text) to evt_therapy.id.
func resolveTherapyIDByName(ctx context.Context, conn *sql.Conn, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", appErrs.BadRequest("terapi wajib")
	}
	for _, cand := range therapyLookupCandidates(raw) {
		var id string
		err := conn.QueryRowContext(ctx, `
			SELECT id::text FROM evt_therapy
			WHERE deleted_at IS NULL AND therapy_name ILIKE $1
			ORDER BY display_order LIMIT 1`, cand).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return "", appErrs.Internal(err.Error())
		}
		// e.g. "Terapi 5 Elemen (maksimal 9 orang…)" → DB "Terapi 5 Elemen"
		err = conn.QueryRowContext(ctx, `
			SELECT id::text FROM evt_therapy
			WHERE deleted_at IS NULL AND $1 ILIKE therapy_name || '%'
			ORDER BY length(therapy_name) DESC, display_order LIMIT 1`, cand).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return "", appErrs.Internal(err.Error())
		}
	}
	return "", appErrs.NotFound("terapi tidak dikenali: " + raw)
}

func resolveTherapyIDsByNames(ctx context.Context, conn *sql.Conn, names []string) ([]string, error) {
	var ids []string
	for _, name := range names {
		id, err := resolveTherapyIDByName(ctx, conn, name)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func countsTowardMealsValue(p *UpsertPersonParams) bool {
	if p == nil || p.CountsTowardMeals == nil {
		return true
	}
	return *p.CountsTowardMeals
}

func personTypeLabel(pt string) string {
	switch strings.ToUpper(strings.TrimSpace(pt)) {
	case "THERAPIST":
		return "Terapis"
	case "VOLUNTEER":
		return "Relawan"
	case "SHIJIE":
		return "Shijie"
	case "DAOSHI":
		return "Daoshi"
	case "FASHI":
		return "Fashi"
	default:
		return pt
	}
}

// publicDisplayNotes strips phone suffix added during online staff registration.
func publicDisplayNotes(notes string) string {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return ""
	}
	if idx := strings.Index(notes, " · Telp:"); idx >= 0 {
		return strings.TrimSpace(notes[:idx])
	}
	if strings.HasPrefix(notes, "Telp:") {
		return ""
	}
	return notes
}

func resolveVolunteerRoleIDByName(ctx context.Context, conn *sql.Conn, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", appErrs.BadRequest("peran relawan wajib untuk relawan")
	}
	var id string
	err := conn.QueryRowContext(ctx, `
		SELECT id::text FROM evt_volunteer_role
		WHERE deleted_at IS NULL AND role_name ILIKE $1
		ORDER BY display_order LIMIT 1`, name).Scan(&id)
	if err == sql.ErrNoRows {
		return "", appErrs.BadRequest("peran relawan tidak dikenali: " + name)
	}
	return id, err
}
