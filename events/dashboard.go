package events

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	encoreerrs "encore.dev/beta/errs"

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

func resolveTherapyMaxFromMaps(
	therapyID, capMode string,
	maxCap sql.NullInt64,
	slotSums, therapistCounts map[string]int,
	shijieCount int,
) int {
	if sum := slotSums[therapyID]; sum > 0 {
		return sum
	}
	switch strings.ToUpper(capMode) {
	case "SHIJIE_COUNT":
		return shijieCount
	case "FIXED":
		if maxCap.Valid && maxCap.Int64 > 0 {
			return int(maxCap.Int64)
		}
		return 1
	default: // THERAPIST_COUNT
		n := therapistCounts[therapyID]
		if n > 0 {
			return n
		}
		if maxCap.Valid && maxCap.Int64 > 0 {
			return int(maxCap.Int64)
		}
		return 1
	}
}

func loadEventDashboard(ctx context.Context, tenantSchema, eventId string) (*EventDashboard, error) {
	ts, err := openTenant(ctx, tenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventExists(ctx, ts, eventId); err != nil {
		return nil, err
	}

	d := EventDashboard{EventID: eventId, PeopleByType: map[string]int{}}

	_ = ts.QueryRowContext(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE reservation_status IN ('CONFIRMED','COMPLETED')),
		  COUNT(*) FILTER (WHERE reservation_status='COMPLETED'),
		  COUNT(*) FILTER (WHERE reservation_status='CANCELLED')
		FROM evt_patient WHERE event_id=$1::uuid AND deleted_at IS NULL`, eventId,
	).Scan(&d.PatientsRegistered, &d.PatientsCompleted, &d.PatientsCancelled)

	patientCounts := map[string]int{}
	pcountRows, err := ts.QueryContext(ctx, `
		SELECT therapy_id::text, COUNT(*)
		FROM evt_patient
		WHERE event_id=$1::uuid AND deleted_at IS NULL
		  AND reservation_status IN ('CONFIRMED','COMPLETED')
		GROUP BY therapy_id`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for pcountRows.Next() {
		var tid string
		var n int
		if err := pcountRows.Scan(&tid, &n); err != nil {
			_ = pcountRows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		patientCounts[tid] = n
	}
	if err := pcountRows.Err(); err != nil {
		_ = pcountRows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := pcountRows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	slotSums := map[string]int{}
	slotSumRows, err := ts.QueryContext(ctx, `
		SELECT therapy_id::text, COALESCE(SUM(capacity), 0)::int
		FROM evt_time_slot
		WHERE event_id=$1::uuid
		GROUP BY therapy_id`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for slotSumRows.Next() {
		var tid string
		var n int
		if err := slotSumRows.Scan(&tid, &n); err != nil {
			_ = slotSumRows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		slotSums[tid] = n
	}
	if err := slotSumRows.Err(); err != nil {
		_ = slotSumRows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := slotSumRows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	therapistCounts := map[string]int{}
	therapistRows, err := ts.QueryContext(ctx, `
		SELECT pt.therapy_id::text, COUNT(DISTINCT p.id)::int
		FROM evt_event_person p
		JOIN evt_person_therapy pt ON pt.person_id = p.id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		  AND p.person_type IN ('THERAPIST','SHIJIE','DAOSHI','FASHI')
		  AND p.attendance_status IN ('PRESENT','PARTIAL')
		GROUP BY pt.therapy_id`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for therapistRows.Next() {
		var tid string
		var n int
		if err := therapistRows.Scan(&tid, &n); err != nil {
			_ = therapistRows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		therapistCounts[tid] = n
	}
	if err := therapistRows.Err(); err != nil {
		_ = therapistRows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := therapistRows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var shijieCount int
	_ = ts.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evt_event_person
		WHERE event_id=$1::uuid AND person_type='SHIJIE' AND deleted_at IS NULL
		  AND attendance_status IN ('PRESENT','PARTIAL')`, eventId,
	).Scan(&shijieCount)

	rows, err := ts.QueryContext(ctx, `
		SELECT et.therapy_id::text, t.therapy_name, et.capacity_mode, et.max_capacity
		FROM evt_event_therapy et
		JOIN evt_therapy t ON t.id = et.therapy_id
		WHERE et.event_id=$1::uuid
		ORDER BY t.display_order`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	for rows.Next() {
		var s therapyCapScratch
		if err := rows.Scan(&s.row.TherapyID, &s.row.TherapyName, &s.capMode, &s.maxCap); err != nil {
			_ = rows.Close()
			return nil, appErrs.Internal(err.Error())
		}
		s.row.Current = patientCounts[s.row.TherapyID]
		s.row.Max = resolveTherapyMaxFromMaps(s.row.TherapyID, s.capMode, s.maxCap, slotSums, therapistCounts, shijieCount)
		d.TherapyCapacity = append(d.TherapyCapacity, s.row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, appErrs.Internal(err.Error())
	}
	if err := rows.Close(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	typeRows, err := ts.QueryContext(ctx, `
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

	_ = ts.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT person_id) FROM (
		  SELECT id AS person_id FROM evt_event_person WHERE event_id=$1::uuid AND deleted_at IS NULL
		  UNION
		  SELECT person_id FROM evt_event_assignment WHERE event_id=$1::uuid AND deleted_at IS NULL
		) u`, eventId).Scan(&d.UniquePeopleCount)

	_ = ts.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evt_event_person
		WHERE event_id=$1::uuid AND deleted_at IS NULL AND counts_toward_meals = true`, eventId,
	).Scan(&d.MealConsumptionCount)
	return &d, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/schedule
func GetEventSchedule(ctx context.Context, eventId string, p *ListSlotsParams) (*EventScheduleResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventExists(ctx, ts, eventId); err != nil {
		return nil, err
	}

	slotsResp, err := ListTimeSlots(ctx, eventId, p)
	if err != nil {
		return nil, err
	}

	filters := patientFilterInput{HasSlot: "true"}
	if p != nil {
		filters.TherapyID = p.TherapyID
		filters.SlotDate = p.Date
	}
	patients, _, err := queryPatients(ctx, ts, eventId, filters, maxPatientExportRows, 0)
	if err != nil {
		var encErr *encoreerrs.Error
		if errors.As(err, &encErr) {
			return nil, err
		}
		return nil, appErrs.Internal("gagal memuat jadwal pasien")
	}
	return &EventScheduleResponse{Slots: slotsResp.Items, Patients: patients}, nil
}
