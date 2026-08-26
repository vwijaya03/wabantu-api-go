package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

type Event struct {
	ID                  string  `json:"id"`
	EventName           string  `json:"eventName"`
	EventSlug           string  `json:"eventSlug"`
	EventDescription    string  `json:"eventDescription,omitempty"`
	CateringOrderNotes  string  `json:"cateringOrderNotes,omitempty"`
	Location            string  `json:"location,omitempty"`
	StartDate           string  `json:"startDate"`
	EndDate             string  `json:"endDate"`
	StartTime           string  `json:"startTime"`
	EndTime             string  `json:"endTime"`
	RegistrationOpenAt  *string `json:"registrationOpenAt,omitempty"`
	RegistrationCloseAt *string `json:"registrationCloseAt,omitempty"`
	Status              string  `json:"status"`
	BreakStartTime      *string `json:"breakStartTime,omitempty"`
	BreakEndTime        *string `json:"breakEndTime,omitempty"`
}

type TherapySlotTemplate struct {
	ID        string `json:"id,omitempty"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Capacity  int    `json:"capacity,omitempty"`
	SortOrder int    `json:"sortOrder"`
}

type EventTherapySetting struct {
	ID                  string                `json:"id"`
	EventID             string                `json:"eventId"`
	TherapyID           string                `json:"therapyId"`
	TherapyName         string                `json:"therapyName,omitempty"`
	SlotDurationMinutes int                   `json:"slotDurationMinutes"`
	MaxCapacity         *int                  `json:"maxCapacity,omitempty"`
	CapacityMode        string                `json:"capacityMode"`
	ScheduleMode        string                `json:"scheduleMode"`
	ScheduleStartTime   *string               `json:"scheduleStartTime,omitempty"`
	ScheduleEndTime     *string               `json:"scheduleEndTime,omitempty"`
	SlotTemplates       []TherapySlotTemplate `json:"slotTemplates,omitempty"`
}

type ListEventsParams struct {
	Q        string `query:"q"`
	Status   string `query:"status"`
	SortBy   string `query:"sortBy"`
	SortDir  string `query:"sortDir"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListEventsResponse struct {
	Items []Event `json:"items"`
	Total int     `json:"total"`
}

type UpsertEventParams struct {
	EventName             string  `json:"eventName"`
	EventSlug             string  `json:"eventSlug,omitempty"`
	EventDescription      string  `json:"eventDescription,omitempty"`
	CateringOrderNotes    string  `json:"cateringOrderNotes,omitempty"`
	Location              string  `json:"location,omitempty"`
	StartDate             string  `json:"startDate"`
	EndDate               string  `json:"endDate"`
	StartTime             string  `json:"startTime"`
	EndTime               string  `json:"endTime"`
	RegistrationOpenAt    *string `json:"registrationOpenAt,omitempty"`
	RegistrationCloseAt   *string `json:"registrationCloseAt,omitempty"`
	Status                string  `json:"status"`
	ImportStaffFromRoster *bool   `json:"importStaffFromRoster,omitempty"`
	BreakStartTime        *string `json:"breakStartTime,omitempty"`
	BreakEndTime          *string `json:"breakEndTime,omitempty"`
}

func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var e Event
	var desc, cateringNotes, loc sql.NullString
	var openAt, closeAt sql.NullTime
	var breakStart, breakEnd sql.NullString
	err := row.Scan(
		&e.ID, &e.EventName, &e.EventSlug, &desc, &cateringNotes, &loc,
		&e.StartDate, &e.EndDate, &e.StartTime, &e.EndTime,
		&breakStart, &breakEnd,
		&openAt, &closeAt, &e.Status,
	)
	if err != nil {
		return e, err
	}
	if desc.Valid {
		e.EventDescription = desc.String
	}
	if cateringNotes.Valid {
		e.CateringOrderNotes = cateringNotes.String
	}
	if loc.Valid {
		e.Location = loc.String
	}
	if openAt.Valid {
		s := openAt.Time.Format(time.RFC3339)
		e.RegistrationOpenAt = &s
	}
	if closeAt.Valid {
		s := closeAt.Time.Format(time.RFC3339)
		e.RegistrationCloseAt = &s
	}
	if breakStart.Valid {
		s := breakStart.String
		e.BreakStartTime = &s
	}
	if breakEnd.Valid {
		s := breakEnd.String
		e.BreakEndTime = &s
	}
	return e, nil
}

//encore:api auth method=GET path=/api/v1/events
func ListEvents(ctx context.Context, p *ListEventsParams) (*ListEventsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	conds := []string{"deleted_at IS NULL"}
	args := []any{}
	i := 1
	if q := strings.TrimSpace(p.Q); q != "" {
		conds = append(conds, fmt.Sprintf("(event_name ILIKE $%d OR event_slug ILIKE $%d)", i, i))
		args = append(args, "%"+q+"%")
		i++
	}
	if st := strings.TrimSpace(p.Status); st != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", i))
		args = append(args, st)
		i++
	}
	where := strings.Join(conds, " AND ")
	orderBy, err := resolveEventOrderBy("", "")
	if p != nil {
		orderBy, err = resolveEventOrderBy(p.SortBy, p.SortDir)
	}
	if err != nil {
		return nil, err
	}
	var total int
	if err := ts.QueryRowContext(ctx, `SELECT COUNT(*) FROM evt_event WHERE `+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, lim, off)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, event_name, event_slug, event_description, catering_order_notes, location,
		       start_date::text, end_date::text, start_time::text, end_time::text,
		       break_start_time::text, break_end_time::text,
		       registration_open_at, registration_close_at, status
		FROM evt_event WHERE %s %s LIMIT $%d OFFSET $%d`,
		where, orderBy, i, i+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		items = append(items, e)
	}
	if items == nil {
		items = []Event{}
	}
	return &ListEventsResponse{Items: items, Total: total}, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId
func GetEvent(ctx context.Context, eventId string) (*Event, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	run := func() (*Event, error) {
		return loadEvent(ctx, u, u.TenantSchema, eventId)
	}
	resp, err := run()
	if isBadConnectionErr(err) {
		resp, err = run()
	}
	return resp, err
}

func loadEvent(ctx context.Context, u *types.AuthUser, tenantSchema, eventId string) (*Event, error) {
	ts, err := openTenant(ctx, tenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	row := ts.QueryRowContext(ctx, `
		SELECT id::text, event_name, event_slug, event_description, catering_order_notes, location,
		       start_date::text, end_date::text, start_time::text, end_time::text,
		       break_start_time::text, break_end_time::text,
		       registration_open_at, registration_close_at, status
		FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventId)
	e, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, eventAccessErr(ctx, u, eventId, tenantSchema)
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &e, nil
}

//encore:api auth method=POST path=/api/v1/events tag:owner
func CreateEvent(ctx context.Context, p *UpsertEventParams) (*Event, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if err := validateEventParams(p); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	slug := strings.TrimSpace(p.EventSlug)
	if slug == "" {
		slug = slugify(p.EventName)
	}
	slug, err = uniqueSlug(ctx, ts, slug, "")
	if err != nil {
		return nil, err
	}
	st := strings.ToUpper(strings.TrimSpace(p.Status))
	if st == "" {
		st = "DRAFT"
	}
	var id string
	err = ts.QueryRowContext(ctx, `
		INSERT INTO evt_event (
		  event_name, event_slug, event_description, catering_order_notes, location,
		  start_date, end_date, start_time, end_time,
		  break_start_time, break_end_time,
		  registration_open_at, registration_close_at, status, created_by
		) VALUES ($1,$2,$3,$4,$5,$6::date,$7::date,$8::time,$9::time,$10::time,$11::time,$12,$13,$14,$15::uuid)
		RETURNING id::text`,
		p.EventName, slug, nullStr(p.EventDescription), nullStr(p.CateringOrderNotes), nullStr(p.Location),
		p.StartDate, p.EndDate, p.StartTime, p.EndTime,
		nullTimeStrPtr(p.BreakStartTime), nullTimeStrPtr(p.BreakEndTime),
		parseTimePtr(p.RegistrationOpenAt), parseTimePtr(p.RegistrationCloseAt),
		st, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	_, _ = ts.ExecContext(ctx, `
		INSERT INTO evt_event_therapy (event_id, therapy_id, slot_duration_minutes, capacity_mode)
		SELECT $1::uuid, t.id, 30, CASE
		  WHEN t.therapy_name ILIKE '%shijie%' THEN 'SHIJIE_COUNT'
		  ELSE 'THERAPIST_COUNT'
		END
		FROM evt_therapy t
		WHERE t.deleted_at IS NULL AND t.is_active = true
		ON CONFLICT (event_id, therapy_id) DO NOTHING`, id)

	importRoster := true
	if p.ImportStaffFromRoster != nil {
		importRoster = *p.ImportStaffFromRoster
	}
	if importRoster {
		_, _, _ = importAllRosterToEvent(ctx, ts, id)
	}

	auditEvent(ctx, ts, u, "event", id, "create", nil, p)
	return GetEvent(ctx, id)
}

//encore:api auth method=PUT path=/api/v1/events/detail/:eventId tag:owner
func UpdateEvent(ctx context.Context, eventId string, p *UpsertEventParams) (*Event, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	if err := validateEventParams(p); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return nil, err
	}
	slug := strings.TrimSpace(p.EventSlug)
	if slug == "" {
		slug = slugify(p.EventName)
	}
	slug, err = uniqueSlug(ctx, ts, slug, eventId)
	if err != nil {
		return nil, err
	}
	st := strings.ToUpper(strings.TrimSpace(p.Status))
	_, err = ts.ExecContext(ctx, `
		UPDATE evt_event SET
		  event_name=$1, event_slug=$2, event_description=$3, catering_order_notes=$4, location=$5,
		  start_date=$6::date, end_date=$7::date, start_time=$8::time, end_time=$9::time,
		  break_start_time=$10::time, break_end_time=$11::time,
		  registration_open_at=$12, registration_close_at=$13, status=$14, updated_at=now()
		WHERE id=$15::uuid AND deleted_at IS NULL`,
		p.EventName, slug, nullStr(p.EventDescription), nullStr(p.CateringOrderNotes), nullStr(p.Location),
		p.StartDate, p.EndDate, p.StartTime, p.EndTime,
		nullTimeStrPtr(p.BreakStartTime), nullTimeStrPtr(p.BreakEndTime),
		parseTimePtr(p.RegistrationOpenAt), parseTimePtr(p.RegistrationCloseAt), st, eventId,
	)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "event", eventId, "update", nil, p)
	return GetEvent(ctx, eventId)
}

//encore:api auth method=DELETE path=/api/v1/events/detail/:eventId tag:owner
func DeleteEvent(ctx context.Context, eventId string) error {
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
	if err := assertEventExists(ctx, u, ts, eventId); err != nil {
		return err
	}
	_, err = ts.ExecContext(ctx, `UPDATE evt_event SET deleted_at=now() WHERE id=$1::uuid AND deleted_at IS NULL`, eventId)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "event", eventId, "delete", nil, nil)
	return nil
}

func validateEventParams(p *UpsertEventParams) error {
	if strings.TrimSpace(p.EventName) == "" {
		return appErrs.BadRequest("nama acara wajib diisi")
	}
	if p.StartDate == "" || p.EndDate == "" {
		return appErrs.BadRequest("tanggal acara wajib diisi")
	}
	sd, err := time.Parse("2006-01-02", p.StartDate)
	if err != nil {
		return appErrs.BadRequest("tanggal mulai tidak valid")
	}
	ed, err := time.Parse("2006-01-02", p.EndDate)
	if err != nil {
		return appErrs.BadRequest("tanggal selesai tidak valid")
	}
	if ed.Before(sd) {
		return appErrs.BadRequest("tanggal selesai tidak boleh sebelum tanggal mulai")
	}
	if strings.TrimSpace(p.StartTime) == "" {
		p.StartTime = "09:00"
	}
	if strings.TrimSpace(p.EndTime) == "" {
		p.EndTime = "17:00"
	}
	if _, err := time.Parse("15:04", p.StartTime); err != nil {
		if _, err2 := time.Parse("15:04:05", p.StartTime); err2 != nil {
			return appErrs.BadRequest("jam mulai tidak valid")
		}
	}
	if _, err := time.Parse("15:04", p.EndTime); err != nil {
		if _, err2 := time.Parse("15:04:05", p.EndTime); err2 != nil {
			return appErrs.BadRequest("jam selesai tidak valid")
		}
	}
	p.EventName = clampLen(p.EventName, 200)
	p.EventDescription = clampLen(p.EventDescription, 2000)
	p.CateringOrderNotes = clampLen(p.CateringOrderNotes, 2000)
	p.Location = clampLen(p.Location, 300)
	st := strings.ToUpper(strings.TrimSpace(p.Status))
	valid := map[string]bool{"DRAFT": true, "PUBLISHED": true, "CLOSED": true, "CANCELLED": true, "ARCHIVED": true}
	if st != "" && !valid[st] {
		return appErrs.BadRequest("status acara tidak valid")
	}
	return validateEventBreakTimes(p.StartTime, p.EndTime, p.BreakStartTime, p.BreakEndTime)
}

func validateEventBreakTimes(startTime, endTime string, breakStart, breakEnd *string) error {
	bs := ""
	be := ""
	if breakStart != nil {
		bs = strings.TrimSpace(*breakStart)
	}
	if breakEnd != nil {
		be = strings.TrimSpace(*breakEnd)
	}
	if bs == "" && be == "" {
		return nil
	}
	if bs == "" || be == "" {
		return appErrs.BadRequest("jam mulai dan selesai jeda istirahat wajib diisi bersamaan")
	}
	bst, err := parseClockTime(bs)
	if err != nil {
		return appErrs.BadRequest("jam mulai jeda tidak valid")
	}
	bet, err := parseClockTime(be)
	if err != nil {
		return appErrs.BadRequest("jam selesai jeda tidak valid")
	}
	if !bst.Before(bet) {
		return appErrs.BadRequest("jam mulai jeda harus sebelum jam lanjut kegiatan")
	}
	dayStart, err := parseClockTime(startTime)
	if err != nil {
		return err
	}
	dayEnd, err := parseClockTime(endTime)
	if err != nil {
		return err
	}
	if bst.Before(dayStart) || bet.After(dayEnd) {
		return appErrs.BadRequest("jeda istirahat harus berada dalam jam acara")
	}
	return nil
}

func parseClockTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, appErrs.BadRequest("jam tidak valid")
	}
	if t, err := time.Parse("15:04:05", padTime(raw)); err == nil {
		return t, nil
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return time.Time{}, appErrs.BadRequest("jam tidak valid")
	}
	return t, nil
}

func nullStr(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func parseTimePtr(s *string) interface{} {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", strings.TrimSpace(*s))
		if err != nil {
			return nil
		}
	}
	return t
}

type UpsertEventTherapyParams struct {
	TherapyID           string                `json:"therapyId"`
	SlotDurationMinutes int                   `json:"slotDurationMinutes"`
	MaxCapacity         *int                  `json:"maxCapacity,omitempty"`
	CapacityMode        string                `json:"capacityMode"`
	ScheduleMode        string                `json:"scheduleMode,omitempty"`
	ScheduleStartTime   string                `json:"scheduleStartTime,omitempty"`
	ScheduleEndTime     string                `json:"scheduleEndTime,omitempty"`
	SlotTemplates       []TherapySlotTemplate `json:"slotTemplates,omitempty"`
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/therapy-settings
func ListEventTherapySettings(ctx context.Context, eventId string) (*ListEventTherapySettingsResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	rows, err := ts.QueryContext(ctx, `
		SELECT ets.id::text, ets.event_id::text, ets.therapy_id::text, t.therapy_name,
		       ets.slot_duration_minutes, ets.max_capacity, ets.capacity_mode,
		       ets.schedule_mode,
		       ets.schedule_start_time::text, ets.schedule_end_time::text
		FROM evt_event_therapy ets
		JOIN evt_therapy t ON t.id = ets.therapy_id
		WHERE ets.event_id = $1::uuid
		ORDER BY t.display_order`, eventId)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []EventTherapySetting
	for rows.Next() {
		var s EventTherapySetting
		var maxCap sql.NullInt64
		var st, en sql.NullString
		if err := rows.Scan(&s.ID, &s.EventID, &s.TherapyID, &s.TherapyName,
			&s.SlotDurationMinutes, &maxCap, &s.CapacityMode, &s.ScheduleMode, &st, &en); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if s.ScheduleMode == "" {
			s.ScheduleMode = "AUTO"
		}
		if maxCap.Valid {
			v := int(maxCap.Int64)
			s.MaxCapacity = &v
		}
		if st.Valid {
			s.ScheduleStartTime = &st.String
		}
		if en.Valid {
			s.ScheduleEndTime = &en.String
		}
		items = append(items, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	tplMap, err := loadTherapySlotTemplates(ctx, ts, eventId)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if tpls, ok := tplMap[items[i].ID]; ok {
			items[i].SlotTemplates = tpls
		} else {
			items[i].SlotTemplates = []TherapySlotTemplate{}
		}
	}
	if items == nil {
		items = []EventTherapySetting{}
	}
	return &ListEventTherapySettingsResponse{Items: items}, nil
}

//encore:api auth method=PUT path=/api/v1/events/detail/:eventId/therapy-settings tag:owner
func UpsertEventTherapySetting(ctx context.Context, eventId string, p *UpsertEventTherapyParams) (*EventTherapySetting, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	mode := strings.ToUpper(strings.TrimSpace(p.CapacityMode))
	if mode == "" {
		mode = "THERAPIST_COUNT"
	}
	validMode := map[string]bool{"THERAPIST_COUNT": true, "SHIJIE_COUNT": true, "FIXED": true}
	if !validMode[mode] {
		return nil, appErrs.BadRequest("mode kapasitas tidak valid")
	}
	if dur := p.SlotDurationMinutes; dur > 0 && (dur < 5 || dur > 480) {
		return nil, appErrs.BadRequest("durasi slot harus antara 5–480 menit")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return nil, err
	}
	schedMode := strings.ToUpper(strings.TrimSpace(p.ScheduleMode))
	if schedMode == "" {
		schedMode = "AUTO"
	}
	if schedMode != "AUTO" && schedMode != "MANUAL" {
		return nil, appErrs.BadRequest("mode jadwal harus AUTO atau MANUAL")
	}
	dur := p.SlotDurationMinutes
	if dur <= 0 {
		dur = 30
	}
	templates, err := normalizeSlotTemplates(p.SlotTemplates, schedMode)
	if err != nil {
		return nil, err
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer tx.Rollback()
	txTS := ts.WithQ(tx)

	var id string
	err = txTS.QueryRowContext(ctx, `
		INSERT INTO evt_event_therapy (
		  event_id, therapy_id, slot_duration_minutes, max_capacity, capacity_mode,
		  schedule_mode, schedule_start_time, schedule_end_time
		) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7::time,$8::time)
		ON CONFLICT (event_id, therapy_id) DO UPDATE SET
		  slot_duration_minutes=EXCLUDED.slot_duration_minutes,
		  max_capacity=EXCLUDED.max_capacity,
		  capacity_mode=EXCLUDED.capacity_mode,
		  schedule_mode=EXCLUDED.schedule_mode,
		  schedule_start_time=EXCLUDED.schedule_start_time,
		  schedule_end_time=EXCLUDED.schedule_end_time,
		  updated_at=now()
		RETURNING id::text`,
		eventId, p.TherapyID, dur, p.MaxCapacity, mode, schedMode,
		nullTimeStr(p.ScheduleStartTime), nullTimeStr(p.ScheduleEndTime),
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := replaceTherapySlotTemplates(ctx, txTS, id, schedMode, templates); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "event_therapy", id, "upsert", nil, p)
	resp, _ := ListEventTherapySettings(ctx, eventId)
	for _, it := range resp.Items {
		if it.ID == id {
			return &it, nil
		}
	}
	return &EventTherapySetting{ID: id, EventID: eventId, TherapyID: p.TherapyID, SlotDurationMinutes: dur, CapacityMode: mode}, nil
}

func nullTimeStr(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}
