package events

import (
	"context"
	"database/sql"
	"sort"
	"strings"
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

// sortPublicPatientScheduleByPreferredTimeASC sorts by preferredTime ascending.
// Empty preferred times are placed last; ties break by fullName then slotLabel.
func sortPublicPatientScheduleByPreferredTimeASC(rows []PublicPatientScheduleRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ti := normalizePreferredTime(rows[i].PreferredTime)
		tj := normalizePreferredTime(rows[j].PreferredTime)
		ei, ej := ti == "", tj == ""
		if ei != ej {
			return !ei // non-empty before empty
		}
		if ti != tj {
			return ti < tj
		}
		ni := strings.ToLower(strings.TrimSpace(rows[i].FullName))
		nj := strings.ToLower(strings.TrimSpace(rows[j].FullName))
		if ni != nj {
			return ni < nj
		}
		return strings.TrimSpace(rows[i].SlotLabel) < strings.TrimSpace(rows[j].SlotLabel)
	})
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
	ts, err := openTenant(ctx, schema)
	if err != nil {
		return nil, err
	}

	var eventID, eventName, status string
	err = ts.QueryRowContext(ctx, `
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

	patients, _, err := queryPatients(ctx, ts, eventID, patientFilterInput{
		Status:  "CONFIRMED",
		HasSlot: "true",
	}, maxPatientExportRows, 0)
	if err != nil {
		return nil, err
	}

	rows := toPublicPatientScheduleRows(patients)
	sortPublicPatientScheduleByPreferredTimeASC(rows)

	return &PublicPatientScheduleResponse{
		EventName: eventName,
		Patients:  rows,
	}, nil
}
