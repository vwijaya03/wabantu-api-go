package events

import (
	"context"
	"database/sql"
)

type PublicPatientScheduleRow struct {
	FullName      string `json:"fullName"`
	TherapyName   string `json:"therapyName"`
	SlotLabel     string `json:"slotLabel"`
	PreferredTime string `json:"preferredTime"`
}

type PublicPatientScheduleResponse struct {
	EventName string                     `json:"eventName"`
	Patients  []PublicPatientScheduleRow `json:"patients"`
}

func toPublicPatientScheduleRows(patients []Patient) []PublicPatientScheduleRow {
	out := make([]PublicPatientScheduleRow, 0, len(patients))
	for _, p := range patients {
		out = append(out, PublicPatientScheduleRow{
			FullName:      p.FullName,
			TherapyName:   p.TherapyName,
			SlotLabel:     p.SlotLabel,
			PreferredTime: p.PreferredTime,
		})
	}
	return out
}

//encore:api public method=GET path=/api/v1/public/events/:tenantSlug/patient-schedule/:eventSlug
func GetPublicPatientSchedule(ctx context.Context, tenantSlug, eventSlug string) (*PublicPatientScheduleResponse, error) {
	return runPublicEvent(ctx, tenantSlug, eventSlug, func() (*PublicPatientScheduleResponse, error) {
		return loadPublicPatientSchedule(ctx, tenantSlug, eventSlug)
	})
}

func loadPublicPatientSchedule(ctx context.Context, tenantSlug, eventSlug string) (*PublicPatientScheduleResponse, error) {
	schema, err := tenantSchemaBySlug(ctx, tenantSlug)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var eventID, eventName, status string
	err = conn.QueryRowContext(ctx, `
		SELECT id::text, event_name, status
		FROM evt_event
		WHERE event_slug=$1 AND deleted_at IS NULL`, eventSlug,
	).Scan(&eventID, &eventName, &status)
	if err == sql.ErrNoRows {
		return nil, publicNotFound()
	}
	if err != nil {
		return nil, err
	}
	if status != "PUBLISHED" {
		return nil, publicNotFound()
	}

	patients, _, err := queryPatients(ctx, conn, eventID, patientFilterInput{
		Status:  "CONFIRMED",
		HasSlot: "true",
	}, maxPatientExportRows, 0)
	if err != nil {
		return nil, err
	}

	return &PublicPatientScheduleResponse{
		EventName: eventName,
		Patients:  toPublicPatientScheduleRows(patients),
	}, nil
}
