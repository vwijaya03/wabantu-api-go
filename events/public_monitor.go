package events

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

type PublicStaffMonitorPerson struct {
	FullName          string   `json:"fullName"`
	RoleLabel         string   `json:"roleLabel"`
	TherapyNames      []string `json:"therapyNames"`
	VolunteerRoleName string   `json:"volunteerRoleName,omitempty"`
	IsPencatat        bool     `json:"isPencatat"`
	CountsTowardMeals bool     `json:"countsTowardMeals"`
	Notes             string   `json:"notes,omitempty"`
}

type PublicStaffMonitorResponse struct {
	EventName            string                     `json:"eventName"`
	EventDescription     string                     `json:"eventDescription,omitempty"`
	CateringOrderNotes   string                     `json:"cateringOrderNotes,omitempty"`
	Location             string                     `json:"location,omitempty"`
	StartDate            string                     `json:"startDate"`
	EndDate              string                     `json:"endDate"`
	StartTime            string                     `json:"startTime"`
	EndTime              string                     `json:"endTime"`
	TherapyCapacity      []TherapyCapacityRow       `json:"therapyCapacity"`
	MealConsumptionCount int                        `json:"mealConsumptionCount"`
	Staff                []PublicStaffMonitorPerson `json:"staff"`
}

//encore:api public method=GET path=/api/v1/public/events/:tenantSlug/monitor/:eventSlug
func GetPublicStaffMonitor(ctx context.Context, tenantSlug, eventSlug string) (*PublicStaffMonitorResponse, error) {
	return runPublicEvent(ctx, tenantSlug, eventSlug, func() (*PublicStaffMonitorResponse, error) {
		return loadPublicStaffMonitor(ctx, tenantSlug, eventSlug)
	})
}

func loadPublicStaffMonitor(ctx context.Context, tenantSlug, eventSlug string) (*PublicStaffMonitorResponse, error) {
	schema, err := tenantSchemaBySlug(ctx, tenantSlug)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, schema)
	if err != nil {
		return nil, err
	}

	var eventID, status string
	var resp PublicStaffMonitorResponse
	var desc, catering, loc sql.NullString
	err = ts.QueryRowContext(ctx, `
		SELECT id::text, event_name, event_description, catering_order_notes, location,
		       start_date::text, end_date::text, start_time::text, end_time::text, status
		FROM evt_event
		WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&eventID, &resp.EventName, &desc, &catering, &loc, &resp.StartDate, &resp.EndDate, &resp.StartTime, &resp.EndTime, &status)
	if err == sql.ErrNoRows {
		return nil, publicNotFound()
	}
	if err != nil {
		return nil, err
	}
	if status != "PUBLISHED" {
		return nil, publicNotFound()
	}
	if desc.Valid {
		resp.EventDescription = desc.String
	}
	if catering.Valid {
		resp.CateringOrderNotes = catering.String
	}
	if loc.Valid {
		resp.Location = loc.String
	}

	dash, err := loadEventDashboard(ctx, nil, schema, eventID)
	if err != nil {
		return nil, err
	}
	resp.TherapyCapacity = dash.TherapyCapacity
	resp.MealConsumptionCount = dash.MealConsumptionCount

	staff, err := loadPublicStaffMonitorPeople(ctx, ts, eventID)
	if err != nil {
		return nil, err
	}
	resp.Staff = staff
	return &resp, nil
}

func loadPublicStaffMonitorPeople(ctx context.Context, ts tenantScope, eventID string) ([]PublicStaffMonitorPerson, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT p.id::text,
		       COALESCE(p.full_name_enc,''), COALESCE(p.full_name,''),
		       p.person_type, COALESCE(p.notes,''), p.counts_toward_meals
		FROM evt_event_person p
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		ORDER BY p.person_type, p.created_at`, eventID)
	if err != nil {
		return nil, err
	}
	type staffScratch struct {
		personID            string
		nameEnc, nameLegacy string
		personType, notes   string
		countsTowardMeals   bool
	}
	var scratch []staffScratch
	for rows.Next() {
		var s staffScratch
		if err := rows.Scan(&s.personID, &s.nameEnc, &s.nameLegacy, &s.personType, &s.notes, &s.countsTowardMeals); err != nil {
			_ = rows.Close()
			return nil, err
		}
		scratch = append(scratch, s)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	extrasByID := make([]EventPerson, len(scratch))
	for i, s := range scratch {
		extrasByID[i].ID = s.personID
	}
	if err := attachPersonExtrasBatch(ctx, ts, extrasByID); err != nil {
		return nil, err
	}

	var out []PublicStaffMonitorPerson
	for i, s := range scratch {
		fullName, err := decryptPersonName(s.nameEnc, s.nameLegacy)
		if err != nil {
			return nil, err
		}
		out = append(out, PublicStaffMonitorPerson{
			FullName:          fullName,
			RoleLabel:         personTypeLabel(s.personType),
			TherapyNames:      extrasByID[i].TherapyNames,
			VolunteerRoleName: extrasByID[i].VolunteerRoleName,
			IsPencatat:        extrasByID[i].IsPencatat,
			CountsTowardMeals: s.countsTowardMeals,
			Notes:             publicDisplayNotes(s.notes),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName)
	})
	if out == nil {
		out = []PublicStaffMonitorPerson{}
	}
	return out, nil
}
