package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

type TimeSlot struct {
	ID          string `json:"id"`
	EventID     string `json:"eventId"`
	TherapyID   string `json:"therapyId"`
	TherapyName string `json:"therapyName,omitempty"`
	SlotDate    string `json:"slotDate"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Capacity    int    `json:"capacity"`
	BookedCount int    `json:"bookedCount"`
	Available   int    `json:"available"`
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/therapies/:therapyId/generate-slots tag:owner
func GenerateTimeSlots(ctx context.Context, eventId, therapyId string) (*GenerateTimeSlotsResponse, error) {
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

	var startDate, endDate string
	var evtStart, evtEnd string
	var breakStart, breakEnd sql.NullString
	if err := conn.QueryRowContext(ctx, `
		SELECT start_date::text, end_date::text, start_time::text, end_time::text,
		       break_start_time::text, break_end_time::text
		FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventId,
	).Scan(&startDate, &endDate, &evtStart, &evtEnd, &breakStart, &breakEnd); err != nil {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}

	var eventTherapyID string
	var dur int
	var schedStart, schedEnd sql.NullString
	var schedMode, capMode string
	var maxCap sql.NullInt64
	if err := conn.QueryRowContext(ctx, `
		SELECT id::text, slot_duration_minutes, schedule_start_time::text, schedule_end_time::text,
		       COALESCE(schedule_mode,'AUTO'), capacity_mode, max_capacity
		FROM evt_event_therapy WHERE event_id=$1::uuid AND therapy_id=$2::uuid`,
		eventId, therapyId,
	).Scan(&eventTherapyID, &dur, &schedStart, &schedEnd, &schedMode, &capMode, &maxCap); err != nil {
		return nil, appErrs.BadRequest("aturan terapi untuk acara ini belum dikonfigurasi")
	}
	if schedMode == "" {
		schedMode = "AUTO"
	}
	dayStart := evtStart
	dayEnd := evtEnd
	if schedStart.Valid {
		dayStart = schedStart.String
	}
	if schedEnd.Valid {
		dayEnd = schedEnd.String
	}
	if dur <= 0 {
		dur = 30
	}

	var templateSlots []slotRange
	if strings.ToUpper(schedMode) == "MANUAL" {
		var err error
		templateSlots, err = loadManualDaySlots(ctx, conn, eventTherapyID)
		if err != nil {
			return nil, err
		}
	}

	sd, _ := time.Parse("2006-01-02", startDate)
	ed, _ := time.Parse("2006-01-02", endDate)
	created := 0
	var warnings []string
	var breakStartPtr, breakEndPtr *string
	if strings.ToUpper(schedMode) == "AUTO" && breakStart.Valid && breakEnd.Valid {
		bs := strings.TrimSpace(breakStart.String)
		be := strings.TrimSpace(breakEnd.String)
		if bs != "" && be != "" {
			breakStartPtr = &bs
			breakEndPtr = &be
		}
	}
	for d := sd; !d.After(ed); d = d.AddDate(0, 0, 1) {
		capacity, err := computeTherapyCapacity(ctx, conn, eventId, therapyId, capMode, maxCap)
		if err != nil {
			return nil, err
		}
		var slots []slotRange
		if len(templateSlots) > 0 {
			slots = templateSlots
		} else {
			slots, err = buildDaySlotsWithBreak(dayStart, dayEnd, dur, breakStartPtr, breakEndPtr)
			if err != nil {
				return nil, err
			}
		}
		for _, sl := range slots {
			slotCapacity := capacity
			if strings.ToUpper(capMode) == "FIXED" && sl.capacity > 0 {
				slotCapacity = sl.capacity
			}
			_, err := conn.ExecContext(ctx, `
				INSERT INTO evt_time_slot (event_id, therapy_id, slot_date, start_time, end_time, capacity)
				VALUES ($1::uuid,$2::uuid,$3::date,$4::time,$5::time,$6)
				ON CONFLICT (event_id, therapy_id, slot_date, start_time) DO UPDATE SET
				  end_time=EXCLUDED.end_time, capacity=EXCLUDED.capacity`,
				eventId, therapyId, d.Format("2006-01-02"), sl.start, sl.end, slotCapacity)
			if err != nil {
				return nil, appErrs.Internal(err.Error())
			}
			created++
		}
	}
	if breakStartPtr != nil && breakEndPtr != nil {
		if warn, err := cleanupBreakWindowSlots(ctx, conn, eventId, therapyId, *breakStartPtr, *breakEndPtr); err != nil {
			return nil, err
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}
	auditEvent(ctx, conn, u, "time_slot", eventId, "generate", nil, map[string]any{"therapyId": therapyId, "created": created})
	resp := &GenerateTimeSlotsResponse{Created: created}
	if len(warnings) > 0 {
		resp.Warnings = warnings
	}
	return resp, nil
}

type slotRange struct {
	start    string
	end      string
	capacity int
}

func buildDaySlots(dayStart, dayEnd string, durMin int) ([]slotRange, error) {
	st, err := time.Parse("15:04:05", padTime(dayStart))
	if err != nil {
		st, err = time.Parse("15:04", dayStart)
	}
	en, err2 := time.Parse("15:04:05", padTime(dayEnd))
	if err != nil || err2 != nil {
		en, _ = time.Parse("15:04", dayEnd)
	}
	var out []slotRange
	cur := st
	for cur.Before(en) {
		nxt := cur.Add(time.Duration(durMin) * time.Minute)
		if nxt.After(en) {
			break
		}
		out = append(out, slotRange{start: cur.Format("15:04:05"), end: nxt.Format("15:04:05")})
		cur = nxt
	}
	return out, nil
}

func buildDaySlotsWithBreak(dayStart, dayEnd string, durMin int, breakStart, breakEnd *string) ([]slotRange, error) {
	segments, err := dayScheduleSegments(dayStart, dayEnd, breakStart, breakEnd)
	if err != nil {
		return nil, err
	}
	var out []slotRange
	for _, seg := range segments {
		slots, err := buildDaySlots(seg.start, seg.end, durMin)
		if err != nil {
			return nil, err
		}
		out = append(out, slots...)
	}
	return out, nil
}

type daySegment struct {
	start string
	end   string
}

func dayScheduleSegments(dayStart, dayEnd string, breakStart, breakEnd *string) ([]daySegment, error) {
	st, err := parseClockTime(dayStart)
	if err != nil {
		return nil, err
	}
	en, err := parseClockTime(dayEnd)
	if err != nil {
		return nil, err
	}
	if !st.Before(en) {
		return nil, appErrs.BadRequest("jam mulai jadwal harus sebelum jam selesai")
	}
	if breakStart == nil || breakEnd == nil {
		return []daySegment{{start: dayStart, end: dayEnd}}, nil
	}
	bsRaw := strings.TrimSpace(*breakStart)
	beRaw := strings.TrimSpace(*breakEnd)
	if bsRaw == "" || beRaw == "" {
		return []daySegment{{start: dayStart, end: dayEnd}}, nil
	}
	bs, err := parseClockTime(bsRaw)
	if err != nil {
		return nil, err
	}
	be, err := parseClockTime(beRaw)
	if err != nil {
		return nil, err
	}
	if !bs.Before(be) {
		return []daySegment{{start: dayStart, end: dayEnd}}, nil
	}
	if !bs.Before(en) || !be.After(st) {
		return []daySegment{{start: dayStart, end: dayEnd}}, nil
	}
	segments := []daySegment{}
	if bs.After(st) {
		segments = append(segments, daySegment{start: dayStart, end: bsRaw})
	}
	if be.Before(en) {
		segments = append(segments, daySegment{start: beRaw, end: dayEnd})
	}
	if len(segments) == 0 {
		return []daySegment{{start: dayStart, end: dayEnd}}, nil
	}
	return segments, nil
}

func cleanupBreakWindowSlots(ctx context.Context, conn *sql.Conn, eventID, therapyID, breakStart, breakEnd string) (string, error) {
	var blocked int
	if err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evt_time_slot
		WHERE event_id=$1::uuid AND therapy_id=$2::uuid
		  AND start_time >= $3::time AND start_time < $4::time
		  AND booked_count > 0`,
		eventID, therapyID, breakStart, breakEnd,
	).Scan(&blocked); err != nil {
		return "", appErrs.Internal(err.Error())
	}
	if blocked > 0 {
		return fmt.Sprintf("%d slot di jendela istirahat masih berisi pasien — pindahkan pasien lalu generate ulang", blocked), nil
	}
	_, err := conn.ExecContext(ctx, `
		DELETE FROM evt_time_slot
		WHERE event_id=$1::uuid AND therapy_id=$2::uuid
		  AND start_time >= $3::time AND start_time < $4::time
		  AND booked_count = 0`,
		eventID, therapyID, breakStart, breakEnd,
	)
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	return "", nil
}

func padTime(t string) string {
	if len(t) == 5 {
		return t + ":00"
	}
	return t
}

// therapyMaxCapacity is total bookable seats for a therapy: sum of generated slot capacities,
// or configured capacity when slots are not generated yet.
func therapyMaxCapacity(ctx context.Context, conn *sql.Conn, eventID, therapyID, capMode string, maxCap sql.NullInt64) (int, error) {
	var slotSum int
	err := conn.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(capacity), 0) FROM evt_time_slot
		WHERE event_id=$1::uuid AND therapy_id=$2::uuid`, eventID, therapyID).Scan(&slotSum)
	if err != nil {
		return 0, err
	}
	if slotSum > 0 {
		return slotSum, nil
	}
	return computeTherapyCapacity(ctx, conn, eventID, therapyID, capMode, maxCap)
}

func computeTherapyCapacity(ctx context.Context, conn *sql.Conn, eventID, therapyID, mode string, maxCap sql.NullInt64) (int, error) {
	switch strings.ToUpper(mode) {
	case "SHIJIE_COUNT":
		var n int
		err := conn.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM evt_event_person
			WHERE event_id=$1::uuid AND person_type='SHIJIE' AND deleted_at IS NULL
			  AND attendance_status IN ('PRESENT','PARTIAL')`, eventID).Scan(&n)
		return n, err
	case "FIXED":
		if maxCap.Valid && maxCap.Int64 > 0 {
			return int(maxCap.Int64), nil
		}
		return 1, nil
	default: // THERAPIST_COUNT
		var n int
		if err := conn.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT p.id) FROM evt_event_person p
			JOIN evt_person_therapy pt ON pt.person_id = p.id
			WHERE p.event_id=$1::uuid AND pt.therapy_id=$2::uuid AND p.deleted_at IS NULL
			  AND p.person_type IN ('THERAPIST','SHIJIE','DAOSHI','FASHI')
			  AND p.attendance_status IN ('PRESENT','PARTIAL')`, eventID, therapyID).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			return n, nil
		}
		if maxCap.Valid && maxCap.Int64 > 0 {
			return int(maxCap.Int64), nil
		}
		return 1, nil
	}
}

type ListSlotsParams struct {
	TherapyID string `query:"therapyId"`
	Date      string `query:"date"`
}

type DeleteSlotsParams struct {
	SlotIDs []string `json:"slotIds"`
}

type DeleteSlotsResponse struct {
	Deleted int      `json:"deleted"`
	Blocked int      `json:"blocked"`
	Errors  []string `json:"errors,omitempty"`
}

//encore:api auth method=DELETE path=/api/v1/events/detail/:eventId/slots/:slotId tag:owner
func DeleteTimeSlot(ctx context.Context, eventId, slotId string) error {
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

	var booked int
	err = conn.QueryRowContext(ctx, `
		SELECT booked_count
		FROM evt_time_slot
		WHERE id=$1::uuid AND event_id=$2::uuid`, slotId, eventId,
	).Scan(&booked)
	if err == sql.ErrNoRows {
		return appErrs.NotFound("slot tidak ditemukan")
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if booked > 0 {
		return appErrs.BadRequest("slot sudah terisi pasien, tidak bisa dihapus")
	}
	var linkedPatients int
	if err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evt_patient
		WHERE slot_id=$1::uuid AND deleted_at IS NULL`, slotId,
	).Scan(&linkedPatients); err != nil {
		return appErrs.Internal(err.Error())
	}
	if linkedPatients > 0 {
		return appErrs.BadRequest("slot masih dipakai data pasien, tidak bisa dihapus")
	}
	// Patient rows are soft-deleted; detach historical references first
	// so FK on evt_patient.slot_id doesn't block slot deletion.
	if _, err := conn.ExecContext(ctx, `
		UPDATE evt_patient
		SET slot_id = NULL, updated_at = now()
		WHERE slot_id=$1::uuid AND deleted_at IS NOT NULL`, slotId); err != nil {
		return appErrs.Internal(err.Error())
	}

	if _, err := conn.ExecContext(ctx, `
		DELETE FROM evt_time_slot
		WHERE id=$1::uuid AND event_id=$2::uuid`, slotId, eventId); err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, conn, u, "time_slot", slotId, "delete", map[string]any{"eventId": eventId}, nil)
	return nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/slots/delete-bulk tag:owner
func DeleteTimeSlotsBulk(ctx context.Context, eventId string, p *DeleteSlotsParams) (*DeleteSlotsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if p == nil || len(p.SlotIDs) == 0 {
		return nil, appErrs.BadRequest("pilih minimal satu slot")
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	if err := assertEventMutable(ctx, conn, eventId); err != nil {
		return nil, err
	}

	resp := &DeleteSlotsResponse{Errors: []string{}}
	seen := map[string]bool{}
	for _, rawID := range p.SlotIDs {
		slotID := strings.TrimSpace(rawID)
		if slotID == "" || seen[slotID] {
			continue
		}
		seen[slotID] = true

		var booked int
		err := conn.QueryRowContext(ctx, `
			SELECT booked_count
			FROM evt_time_slot
			WHERE id=$1::uuid AND event_id=$2::uuid`, slotID, eventId,
		).Scan(&booked)
		if err == sql.ErrNoRows {
			resp.Blocked++
			resp.Errors = append(resp.Errors, slotID+": slot tidak ditemukan")
			continue
		}
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if booked > 0 {
			resp.Blocked++
			resp.Errors = append(resp.Errors, slotID+": slot sudah terisi pasien")
			continue
		}
		var linkedPatients int
		if err := conn.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM evt_patient
			WHERE slot_id=$1::uuid AND deleted_at IS NULL`, slotID,
		).Scan(&linkedPatients); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if linkedPatients > 0 {
			resp.Blocked++
			resp.Errors = append(resp.Errors, slotID+": slot masih dipakai data pasien")
			continue
		}
		if _, err := conn.ExecContext(ctx, `
			UPDATE evt_patient
			SET slot_id = NULL, updated_at = now()
			WHERE slot_id=$1::uuid AND deleted_at IS NOT NULL`, slotID); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if _, err := conn.ExecContext(ctx, `
			DELETE FROM evt_time_slot
			WHERE id=$1::uuid AND event_id=$2::uuid`, slotID, eventId); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		resp.Deleted++
		auditEvent(ctx, conn, u, "time_slot", slotID, "delete_bulk_item", map[string]any{"eventId": eventId}, nil)
	}
	if len(resp.Errors) == 0 {
		resp.Errors = nil
	}
	return resp, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/slots
func ListTimeSlots(ctx context.Context, eventId string, p *ListSlotsParams) (*ListTimeSlotsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()
	conds := []string{"s.event_id = $1::uuid"}
	args := []any{eventId}
	i := 2
	if p != nil && p.TherapyID != "" {
		conds = append(conds, fmt.Sprintf("s.therapy_id = $%d::uuid", i))
		args = append(args, p.TherapyID)
		i++
	}
	if p != nil && p.Date != "" {
		conds = append(conds, fmt.Sprintf("s.slot_date = $%d::date", i))
		args = append(args, p.Date)
		i++
	}
	where := strings.Join(conds, " AND ")
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT s.id::text, s.event_id::text, s.therapy_id::text, t.therapy_name,
		       s.slot_date::text, s.start_time::text, s.end_time::text, s.capacity, s.booked_count
		FROM evt_time_slot s
		JOIN evt_therapy t ON t.id = s.therapy_id
		WHERE %s ORDER BY s.slot_date, s.start_time`, where), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []TimeSlot
	for rows.Next() {
		var s TimeSlot
		if err := rows.Scan(&s.ID, &s.EventID, &s.TherapyID, &s.TherapyName,
			&s.SlotDate, &s.StartTime, &s.EndTime, &s.Capacity, &s.BookedCount); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		s.Available = s.Capacity - s.BookedCount
		if s.Available < 0 {
			s.Available = 0
		}
		items = append(items, s)
	}
	if items == nil {
		items = []TimeSlot{}
	}
	return &ListTimeSlotsResponse{Items: items}, nil
}

func lockAndIncrementSlot(ctx context.Context, tx *sql.Tx, slotID string) error {
	var cap, booked int
	err := tx.QueryRowContext(ctx, `
		SELECT capacity, booked_count FROM evt_time_slot WHERE id=$1::uuid FOR UPDATE`, slotID,
	).Scan(&cap, &booked)
	if err != nil {
		return err
	}
	if booked >= cap {
		return appErrs.BadRequest("slot penuh")
	}
	_, err = tx.ExecContext(ctx, `UPDATE evt_time_slot SET booked_count = booked_count + 1 WHERE id=$1::uuid`, slotID)
	return err
}
