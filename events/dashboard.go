package events

import (
	"context"
	"database/sql"

	appErrs "encore.app/wabantu/shared/errs"
)

type EventDashboard struct {
	EventID              string                    `json:"eventId"`
	PatientsRegistered   int                       `json:"patientsRegistered"`
	PatientsCompleted    int                       `json:"patientsCompleted"`
	PatientsCancelled    int                       `json:"patientsCancelled"`
	TherapyCapacity      []TherapyCapacityRow      `json:"therapyCapacity"`
	PeopleByType         map[string]int            `json:"peopleByType"`
	UniquePeopleCount    int                       `json:"uniquePeopleCount"`
	MealConsumptionCount int                       `json:"mealConsumptionCount"`
}

type TherapyCapacityRow struct {
	TherapyID   string `json:"therapyId"`
	TherapyName string `json:"therapyName"`
	Current     int    `json:"current"`
	Max         int    `json:"max"`
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/dashboard
func GetEventDashboard(ctx context.Context, eventId string) (*EventDashboard, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	run := func() (*EventDashboard, error) {
		return loadEventDashboard(ctx, u.TenantSchema, eventId)
	}
	resp, err := run()
	if isBadConnectionErr(err) {
		resp, err = run()
	}
	return resp, err
}

type therapyCapScratch struct {
	row     TherapyCapacityRow
	capMode string
	maxCap  sql.NullInt64
}

func loadEventDashboard(ctx context.Context, tenantSchema, eventId string) (*EventDashboard, error) {
	conn, err := tenantConn(ctx, tenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventExists(ctx, conn, eventId); err != nil {
		return nil, err
	}

	d := EventDashboard{EventID: eventId, PeopleByType: map[string]int{}}

	_ = conn.QueryRowContext(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE reservation_status IN ('CONFIRMED','COMPLETED')),
		  COUNT(*) FILTER (WHERE reservation_status='COMPLETED'),
		  COUNT(*) FILTER (WHERE reservation_status='CANCELLED')
		FROM evt_patient WHERE event_id=$1::uuid AND deleted_at IS NULL`, eventId,
	).Scan(&d.PatientsRegistered, &d.PatientsCompleted, &d.PatientsCancelled)

	rows, err := conn.QueryContext(ctx, `
		SELECT et.therapy_id::text, t.therapy_name, et.capacity_mode, et.max_capacity,
		  (SELECT COUNT(*) FROM evt_patient p WHERE p.event_id=$1::uuid AND p.therapy_id=et.therapy_id
		     AND p.deleted_at IS NULL AND p.reservation_status IN ('CONFIRMED','COMPLETED'))
		FROM evt_event_therapy et
		JOIN evt_therapy t ON t.id = et.therapy_id
		WHERE et.event_id=$1::uuid
		ORDER BY t.display_order`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	var scratch []therapyCapScratch
	for rows.Next() {
		var s therapyCapScratch
		if err := rows.Scan(&s.row.TherapyID, &s.row.TherapyName, &s.capMode, &s.maxCap, &s.row.Current); err != nil {
			_ = rows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		scratch = append(scratch, s)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := rows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	// therapyMaxCapacity runs additional queries — must not run while rows cursor is open.
	for _, s := range scratch {
		max, err := therapyMaxCapacity(ctx, conn, eventId, s.row.TherapyID, s.capMode, s.maxCap)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		s.row.Max = max
		d.TherapyCapacity = append(d.TherapyCapacity, s.row)
	}

	typeRows, err := conn.QueryContext(ctx, `
		SELECT person_type, COUNT(*) FROM evt_event_person
		WHERE event_id=$1::uuid AND deleted_at IS NULL
		GROUP BY person_type`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for typeRows.Next() {
		var pt string
		var n int
		if err := typeRows.Scan(&pt, &n); err != nil {
			_ = typeRows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		d.PeopleByType[pt] = n
	}
	if err := typeRows.Err(); err != nil {
		_ = typeRows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := typeRows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	_ = conn.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT person_id) FROM (
		  SELECT id AS person_id FROM evt_event_person WHERE event_id=$1::uuid AND deleted_at IS NULL
		  UNION
		  SELECT person_id FROM evt_event_assignment WHERE event_id=$1::uuid AND deleted_at IS NULL
		) u`, eventId).Scan(&d.UniquePeopleCount)

	_ = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evt_event_person
		WHERE event_id=$1::uuid AND deleted_at IS NULL AND counts_toward_meals = true`, eventId,
	).Scan(&d.MealConsumptionCount)
	return &d, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/schedule
func GetEventSchedule(ctx context.Context, eventId string, p *ListSlotsParams) (*EventScheduleResponse, error) {
	slotsResp, err := ListTimeSlots(ctx, eventId, p)
	if err != nil {
		return nil, err
	}
	patParams := &ListPatientsParams{Page: 1, PageSize: 2000}
	if p != nil {
		patParams.TherapyID = p.TherapyID
		patParams.SlotDate = p.Date
	}
	patResp, err := ListPatients(ctx, eventId, patParams)
	if err != nil {
		return nil, err
	}
	return &EventScheduleResponse{Slots: slotsResp.Items, Patients: patResp.Items}, nil
}
