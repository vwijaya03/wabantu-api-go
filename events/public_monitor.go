package events

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type PublicStaffMonitorPerson struct {
	FullName     string   `json:"fullName"`
	RoleLabel    string   `json:"roleLabel"`
	TherapyNames []string `json:"therapyNames"`
	IsPencatat   bool     `json:"isPencatat"`
	Notes        string   `json:"notes,omitempty"`
}

type PublicStaffMonitorResponse struct {
	EventName            string                     `json:"eventName"`
	EventDescription     string                     `json:"eventDescription,omitempty"`
	Location             string                     `json:"location,omitempty"`
	StartDate            string                     `json:"startDate"`
	EndDate              string                     `json:"endDate"`
	TherapyCapacity      []TherapyCapacityRow       `json:"therapyCapacity"`
	MealConsumptionCount int                        `json:"mealConsumptionCount"`
	Staff                []PublicStaffMonitorPerson `json:"staff"`
}

//encore:api public method=GET path=/api/v1/public/events/:tenantSlug/monitor/:eventSlug
func GetPublicStaffMonitor(ctx context.Context, tenantSlug, eventSlug string) (*PublicStaffMonitorResponse, error) {
	schema, err := tenantSchemaBySlug(ctx, tenantSlug)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var eventID, status string
	var resp PublicStaffMonitorResponse
	var desc, loc sql.NullString
	err = conn.QueryRowContext(ctx, `
		SELECT id::text, event_name, event_description, location,
		       start_date::text, end_date::text, status
		FROM evt_event
		WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&eventID, &resp.EventName, &desc, &loc, &resp.StartDate, &resp.EndDate, &status)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if status != "PUBLISHED" {
		return nil, appErrs.NotFound("acara tidak tersedia")
	}
	if desc.Valid {
		resp.EventDescription = desc.String
	}
	if loc.Valid {
		resp.Location = loc.String
	}

	dash, err := loadEventDashboard(ctx, schema, eventID)
	if err != nil {
		return nil, err
	}
	resp.TherapyCapacity = dash.TherapyCapacity
	resp.MealConsumptionCount = dash.MealConsumptionCount

	staff, err := loadPublicStaffMonitorPeople(ctx, conn, eventID)
	if err != nil {
		return nil, err
	}
	resp.Staff = staff
	return &resp, nil
}

func loadPublicStaffMonitorPeople(ctx context.Context, conn *sql.Conn, eventID string) ([]PublicStaffMonitorPerson, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT p.id::text,
		       COALESCE(p.full_name_enc,''), COALESCE(p.full_name,''),
		       p.person_type, COALESCE(p.notes,'')
		FROM evt_event_person p
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		ORDER BY p.person_type, p.created_at`, eventID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var out []PublicStaffMonitorPerson
	for rows.Next() {
		var personID, nameEnc, nameLegacy, personType, notes string
		if err := rows.Scan(&personID, &nameEnc, &nameLegacy, &personType, &notes); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		fullName, err := decryptPersonName(nameEnc, nameLegacy)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		var extras EventPerson
		if err := loadPersonExtras(ctx, conn, personID, &extras); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		out = append(out, PublicStaffMonitorPerson{
			FullName:     fullName,
			RoleLabel:    personTypeLabel(personType),
			TherapyNames: extras.TherapyNames,
			IsPencatat:   extras.IsPencatat,
			Notes:        publicDisplayNotes(notes),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName)
	})
	if out == nil {
		out = []PublicStaffMonitorPerson{}
	}
	return out, nil
}
