package events

import (
	"context"
	"database/sql"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/system"
)

type PublicEventInfo struct {
	EventName        string              `json:"eventName"`
	EventDescription string              `json:"eventDescription,omitempty"`
	Location         string              `json:"location,omitempty"`
	StartDate        string              `json:"startDate"`
	EndDate          string              `json:"endDate"`
	Status           string              `json:"status"`
	RegistrationOpen bool                `json:"registrationOpen"`
	Message          string              `json:"message,omitempty"`
	Therapies        []Therapy           `json:"therapies"`
	Closed           bool                `json:"closed"`
	Cancelled        bool                `json:"cancelled"`
}

type PublicRegisterParams struct {
	TenantSlug    string `json:"tenantSlug"`
	FullName      string `json:"fullName"`
	BirthDate     string `json:"birthDate"`
	TherapyID     string `json:"therapyId"`
	Complaint     string `json:"complaint,omitempty"`
	PreferredTime string `json:"preferredTime,omitempty"`
}

type PublicRegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type PublicSlotsParams struct {
	TherapyID string `query:"therapyId"`
}

type PublicSlotsResponse struct {
	Items []PublicSlotOption `json:"items"`
}

func tenantSchemaBySlug(ctx context.Context, slug string) (string, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", appErrs.BadRequest("tenant tidak valid")
	}
	var schema string
	err := system.DB.QueryRow(ctx, `
		SELECT tc.schema_name FROM tenant_company tc
		JOIN tenant t ON t.id = tc.tenant_id
		WHERE t.slug = $1 AND t.deleted_at IS NULL`, slug).Scan(&schema)
	if err == sql.ErrNoRows {
		return "", appErrs.NotFound("toko tidak ditemukan")
	}
	if err != nil {
		return "", appErrs.Internal(err.Error())
	}
	return schema, nil
}

//encore:api public method=GET path=/api/v1/public/events/:tenantSlug/register/:eventSlug
func GetPublicRegistration(ctx context.Context, tenantSlug, eventSlug string) (*PublicEventInfo, error) {
	schema, err := tenantSchemaBySlug(ctx, tenantSlug)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var e PublicEventInfo
	var desc, loc sql.NullString
	var openAt, closeAt sql.NullTime
	err = conn.QueryRowContext(ctx, `
		SELECT event_name, event_description, location,
		       start_date::text, end_date::text, status,
		       registration_open_at, registration_close_at
		FROM evt_event
		WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&e.EventName, &desc, &loc, &e.StartDate, &e.EndDate, &e.Status, &openAt, &closeAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if desc.Valid {
		e.EventDescription = desc.String
	}
	if loc.Valid {
		e.Location = loc.String
	}

	switch e.Status {
	case "CANCELLED":
		e.Cancelled = true
		e.Message = "Acara dibatalkan."
		return &e, nil
	case "DRAFT", "ARCHIVED":
		return nil, appErrs.NotFound("acara tidak tersedia")
	case "CLOSED":
		e.Closed = true
		e.Message = "Pendaftaran telah ditutup."
	case "PUBLISHED":
		now := time.Now()
		e.RegistrationOpen = registrationOpen(now, openAt, closeAt)
		if !e.RegistrationOpen {
			e.Closed = true
			e.Message = "Pendaftaran telah ditutup."
		}
	default:
		e.Closed = true
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT t.id::text, t.therapy_name, COALESCE(t.description,''), t.is_active, t.display_order
		FROM evt_therapy t
		JOIN evt_event_therapy et ON et.therapy_id = t.id
		JOIN evt_event e ON e.id = et.event_id
		WHERE e.event_slug=$1 AND t.deleted_at IS NULL AND t.is_active = true
		ORDER BY t.display_order`, eventSlug)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var t Therapy
		if err := rows.Scan(&t.ID, &t.TherapyName, &t.Description, &t.Active, &t.DisplayOrder); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		e.Therapies = append(e.Therapies, t)
	}
	if e.Therapies == nil {
		e.Therapies = []Therapy{}
	}
	return &e, nil
}

//encore:api public method=GET path=/api/v1/public/events/:tenantSlug/register/:eventSlug/slots
func GetPublicRegistrationSlots(ctx context.Context, tenantSlug, eventSlug string, p *PublicSlotsParams) (*PublicSlotsResponse, error) {
	if p == nil || strings.TrimSpace(p.TherapyID) == "" {
		return nil, appErrs.BadRequest("terapi wajib dipilih")
	}
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
	var openAt, closeAt sql.NullTime
	err = conn.QueryRowContext(ctx, `
		SELECT id::text, status, registration_open_at, registration_close_at
		FROM evt_event WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&eventID, &status, &openAt, &closeAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if status != "PUBLISHED" || !registrationOpen(time.Now(), openAt, closeAt) {
		return &PublicSlotsResponse{Items: []PublicSlotOption{}}, nil
	}

	items, err := listPublicSlotOptions(ctx, conn, eventID, strings.TrimSpace(p.TherapyID))
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &PublicSlotsResponse{Items: items}, nil
}

//encore:api public method=POST path=/api/v1/public/events/:tenantSlug/register/:eventSlug
func PostPublicRegistration(ctx context.Context, tenantSlug, eventSlug string, p *PublicRegisterParams) (*PublicRegisterResponse, error) {
	if p == nil {
		return nil, appErrs.BadRequest("data tidak valid")
	}
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
	var openAt, closeAt sql.NullTime
	err = conn.QueryRowContext(ctx, `
		SELECT id::text, status, registration_open_at, registration_close_at
		FROM evt_event WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&eventID, &status, &openAt, &closeAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if status == "CANCELLED" {
		return nil, appErrs.BadRequest("Acara dibatalkan.")
	}
	if status != "PUBLISHED" {
		return nil, appErrs.BadRequest("Pendaftaran telah ditutup.")
	}
	if !registrationOpen(time.Now(), openAt, closeAt) {
		return nil, appErrs.BadRequest("Pendaftaran telah ditutup.")
	}
	if strings.TrimSpace(p.FullName) == "" || strings.TrimSpace(p.TherapyID) == "" {
		return nil, appErrs.BadRequest("nama dan terapi wajib diisi")
	}
	if len(strings.TrimSpace(p.FullName)) > maxPatientNameLen {
		return nil, appErrs.BadRequest("nama terlalu panjang")
	}
	if len(strings.TrimSpace(p.Complaint)) > maxComplaintLen {
		return nil, appErrs.BadRequest("keluhan terlalu panjang")
	}
	if strings.TrimSpace(p.PreferredTime) == "" {
		return nil, appErrs.BadRequest("pilih jam terapi terlebih dahulu")
	}

	_, err = registerPatient(ctx, schema, eventID, p.TherapyID, p.FullName, p.BirthDate, p.Complaint, p.PreferredTime)
	if err != nil {
		return nil, err
	}
	return &PublicRegisterResponse{
		Success: true,
		Message: "Pendaftaran berhasil. Terima kasih.",
	}, nil
}

type PublicStaffEventInfo struct {
	EventName        string          `json:"eventName"`
	EventDescription string          `json:"eventDescription,omitempty"`
	Location         string          `json:"location,omitempty"`
	StartDate        string          `json:"startDate"`
	EndDate          string          `json:"endDate"`
	Status           string          `json:"status"`
	RegistrationOpen bool            `json:"registrationOpen"`
	Message          string          `json:"message,omitempty"`
	Therapies        []Therapy       `json:"therapies"`
	VolunteerRoles   []VolunteerRole `json:"volunteerRoles"`
	Closed           bool            `json:"closed"`
	Cancelled        bool            `json:"cancelled"`
}

type PublicStaffRegisterBody struct {
	FullName        string   `json:"fullName"`
	Role            string   `json:"role"`
	TherapyIDs      []string `json:"therapyIds,omitempty"`
	VolunteerRoleID string   `json:"volunteerRoleId,omitempty"`
	Phone              string   `json:"phone,omitempty"`
	Notes              string   `json:"notes,omitempty"`
	CountsTowardMeals  *bool    `json:"countsTowardMeals,omitempty"`
}

//encore:api public method=GET path=/api/v1/public/events/:tenantSlug/register/:eventSlug/staff
func GetPublicStaffRegistration(ctx context.Context, tenantSlug, eventSlug string) (*PublicStaffEventInfo, error) {
	schema, err := tenantSchemaBySlug(ctx, tenantSlug)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	var e PublicStaffEventInfo
	var desc, loc sql.NullString
	var openAt, closeAt sql.NullTime
	err = conn.QueryRowContext(ctx, `
		SELECT event_name, event_description, location,
		       start_date::text, end_date::text, status,
		       registration_open_at, registration_close_at
		FROM evt_event
		WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&e.EventName, &desc, &loc, &e.StartDate, &e.EndDate, &e.Status, &openAt, &closeAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if desc.Valid {
		e.EventDescription = desc.String
	}
	if loc.Valid {
		e.Location = loc.String
	}

	switch e.Status {
	case "CANCELLED":
		e.Cancelled = true
		e.Message = "Acara dibatalkan."
		return &e, nil
	case "DRAFT", "ARCHIVED":
		return nil, appErrs.NotFound("acara tidak tersedia")
	case "CLOSED":
		e.Closed = true
		e.Message = "Pendaftaran telah ditutup."
	case "PUBLISHED":
		e.RegistrationOpen = registrationOpen(time.Now(), openAt, closeAt)
		if !e.RegistrationOpen {
			e.Closed = true
			e.Message = "Pendaftaran telah ditutup."
		}
	default:
		e.Closed = true
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT t.id::text, t.therapy_name, COALESCE(t.description,''), t.is_active, t.display_order
		FROM evt_therapy t
		JOIN evt_event_therapy et ON et.therapy_id = t.id
		JOIN evt_event e ON e.id = et.event_id
		WHERE e.event_slug=$1 AND t.deleted_at IS NULL AND t.is_active = true
		ORDER BY t.display_order`, eventSlug)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var t Therapy
		if err := rows.Scan(&t.ID, &t.TherapyName, &t.Description, &t.Active, &t.DisplayOrder); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		e.Therapies = append(e.Therapies, t)
	}
	if e.Therapies == nil {
		e.Therapies = []Therapy{}
	}

	vrows, err := conn.QueryContext(ctx, `
		SELECT id::text, role_name, is_active, display_order
		FROM evt_volunteer_role
		WHERE deleted_at IS NULL AND is_active = true
		ORDER BY display_order`)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer vrows.Close()
	for vrows.Next() {
		var r VolunteerRole
		if err := vrows.Scan(&r.ID, &r.RoleName, &r.Active, &r.DisplayOrder); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		e.VolunteerRoles = append(e.VolunteerRoles, r)
	}
	if e.VolunteerRoles == nil {
		e.VolunteerRoles = []VolunteerRole{}
	}
	return &e, nil
}

//encore:api public method=POST path=/api/v1/public/events/:tenantSlug/register/:eventSlug/staff
func PostPublicStaffRegistration(ctx context.Context, tenantSlug, eventSlug string, p *PublicStaffRegisterBody) (*PublicRegisterResponse, error) {
	if p == nil {
		return nil, appErrs.BadRequest("data tidak valid")
	}
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
	var openAt, closeAt sql.NullTime
	err = conn.QueryRowContext(ctx, `
		SELECT id::text, status, registration_open_at, registration_close_at
		FROM evt_event WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&eventID, &status, &openAt, &closeAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if status == "CANCELLED" {
		return nil, appErrs.BadRequest("Acara dibatalkan.")
	}
	if status != "PUBLISHED" {
		return nil, appErrs.BadRequest("Pendaftaran telah ditutup.")
	}
	if !registrationOpen(time.Now(), openAt, closeAt) {
		return nil, appErrs.BadRequest("Pendaftaran telah ditutup.")
	}

	notes := strings.TrimSpace(p.Notes)
	if phone := strings.TrimSpace(p.Phone); phone != "" {
		if notes != "" {
			notes += " · "
		}
		notes += "Telp: " + phone
	}
	if notes == "" {
		notes = "Pendaftaran online (staf/relawan)"
	} else {
		notes = "Pendaftaran online — " + notes
	}

	var volID *string
	if v := strings.TrimSpace(p.VolunteerRoleID); v != "" {
		volID = &v
	}
	saveFalse := false
	params := &UpsertPersonParams{
		FullName:         p.FullName,
		Role:             p.Role,
		TherapyIDs:       p.TherapyIDs,
		VolunteerRoleID:  volID,
		AttendanceStatus: "PRESENT",
		Notes:            notes,
		CountsTowardMeals: p.CountsTowardMeals,
		SaveToRoster:     &saveFalse,
	}
	if err := validatePerson(params); err != nil {
		return nil, err
	}
	if err := createPersonInEvent(ctx, conn, eventID, params); err != nil {
		return nil, err
	}
	return &PublicRegisterResponse{
		Success: true,
		Message: "Pendaftaran staf/relawan berhasil. Terima kasih.",
	}, nil
}
