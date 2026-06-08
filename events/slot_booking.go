package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

// normalizePreferredTime converts "09.00", "9:00", "09:00:00" → "09:00" for slot matching.
func normalizePreferredTime(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", ":")
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return s
	}
	h, m := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if len(h) == 1 {
		h = "0" + h
	}
	if len(m) == 1 {
		m = "0" + m
	}
	if len(m) > 2 {
		m = m[:2]
	}
	return h + ":" + m
}

func slotStartMatches(startTime, preferred string) bool {
	pref := normalizePreferredTime(preferred)
	if pref == "" {
		return false
	}
	start := normalizePreferredTime(startTime)
	if len(start) >= 5 {
		start = start[:5]
	}
	return pref == start
}

func formatSlotTime(t string) string {
	t = strings.TrimSpace(t)
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

type PublicSlotOption struct {
	SlotID    string `json:"slotId"`
	SlotDate  string `json:"slotDate"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Label     string `json:"label"`
	Available int    `json:"available"`
}

func countTherapySlots(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, eventID, therapyID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM evt_time_slot
		WHERE event_id=$1::uuid AND therapy_id=$2::uuid`, eventID, therapyID,
	).Scan(&n)
	return n, err
}

func listPublicSlotOptions(ctx context.Context, conn *sql.Conn, eventID, therapyID string) ([]PublicSlotOption, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT id::text, slot_date::text, start_time::text, end_time::text, capacity, booked_count
		FROM evt_time_slot
		WHERE event_id=$1::uuid AND therapy_id=$2::uuid AND booked_count < capacity
		ORDER BY slot_date, start_time`, eventID, therapyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicSlotOption
	for rows.Next() {
		var o PublicSlotOption
		var cap, booked int
		if err := rows.Scan(&o.SlotID, &o.SlotDate, &o.StartTime, &o.EndTime, &cap, &booked); err != nil {
			return nil, err
		}
		o.StartTime = formatSlotTime(o.StartTime)
		o.EndTime = formatSlotTime(o.EndTime)
		o.Available = cap - booked
		o.Label = fmt.Sprintf("%s (%d tersisa)", formatPatientSlotLabel(o.SlotDate, o.StartTime, o.EndTime), o.Available)
		out = append(out, o)
	}
	if out == nil {
		out = []PublicSlotOption{}
	}
	return out, rows.Err()
}

// pickSlotForRegistration assigns a slot. strictPreferred=true (public): must match preferred time exactly.
func pickSlotForRegistration(ctx context.Context, tx *sql.Tx, eventID, therapyID, preferred string, strictPreferred bool) (string, error) {
	preferred = normalizePreferredTime(preferred)

	total, err := countTherapySlots(ctx, tx, eventID, therapyID)
	if err != nil {
		return "", err
	}
	if total == 0 {
		return "", appErrs.BadRequest("jadwal slot belum dibuat untuk terapi ini — owner perlu generate slot di tab Jadwal")
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, start_time::text, capacity, booked_count
		FROM evt_time_slot
		WHERE event_id=$1::uuid AND therapy_id=$2::uuid AND booked_count < capacity
		ORDER BY slot_date, start_time FOR UPDATE`, eventID, therapyID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var fallback string
	for rows.Next() {
		var id, start string
		var cap, booked int
		if err := rows.Scan(&id, &start, &cap, &booked); err != nil {
			return "", err
		}
		if strictPreferred {
			if preferred == "" {
				continue
			}
			if slotStartMatches(start, preferred) {
				return id, nil
			}
			continue
		}
		if fallback == "" {
			fallback = id
		}
		if preferred != "" && slotStartMatches(start, preferred) {
			return id, nil
		}
	}

	if strictPreferred {
		if preferred == "" {
			return "", appErrs.BadRequest("pilih jam terapi terlebih dahulu")
		}
		return "", appErrs.BadRequest(fmt.Sprintf(
			"slot pukul %s sudah penuh atau tidak tersedia — pilih jam lain", preferred,
		))
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", appErrs.BadRequest("tidak ada slot tersedia untuk terapi ini")
}

// assignPatientSlotBestEffort links a patient to a slot when possible; import must not fail if slots are full/missing.
func assignPatientSlotBestEffort(ctx context.Context, tenantSchema, eventID, patientID, therapyID, preferred string) error {
	conn, err := tenantConn(ctx, tenantSchema)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tryAssignPatientSlot(ctx, tx, eventID, patientID, therapyID, preferred, false); err != nil {
		return err
	}
	return tx.Commit()
}

func tryAssignPatientSlot(ctx context.Context, tx *sql.Tx, eventID, patientID, therapyID, preferred string, strict bool) error {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return nil
	}
	var existing sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT slot_id::text FROM evt_patient WHERE id=$1::uuid AND deleted_at IS NULL`,
		patientID).Scan(&existing); err != nil {
		return err
	}
	if existing.Valid {
		return nil
	}
	sid, err := pickSlotForRegistration(ctx, tx, eventID, therapyID, preferred, strict)
	if err != nil {
		return err
	}
	if err := lockAndIncrementSlot(ctx, tx, sid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE evt_patient SET slot_id=$1::uuid, updated_at=now() WHERE id=$2::uuid`, sid, patientID)
	return err
}
