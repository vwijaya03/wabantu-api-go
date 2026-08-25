package events

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

func loadTherapySlotTemplates(ctx context.Context, ts tenantScope, eventID string) (map[string][]TherapySlotTemplate, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT ets.id::text, st.id::text, st.start_time::text, st.end_time::text, st.capacity, st.sort_order
		FROM evt_event_therapy_slot_template st
		JOIN evt_event_therapy ets ON ets.id = st.event_therapy_id
		WHERE ets.event_id = $1::uuid
		ORDER BY st.sort_order, st.start_time`, eventID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	out := map[string][]TherapySlotTemplate{}
	for rows.Next() {
		var etID, id, start, end string
		var ord, capacity int
		if err := rows.Scan(&etID, &id, &start, &end, &capacity, &ord); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if capacity <= 0 {
			capacity = 1
		}
		out[etID] = append(out[etID], TherapySlotTemplate{
			ID: id, StartTime: trimTime(start), EndTime: trimTime(end), Capacity: capacity, SortOrder: ord,
		})
	}
	return out, rows.Err()
}

func trimTime(t string) string {
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

func normalizeSlotTemplates(in []TherapySlotTemplate, mode string) ([]TherapySlotTemplate, error) {
	if mode != "MANUAL" {
		return nil, nil
	}
	if len(in) == 0 {
		return nil, appErrs.BadRequest("mode manual membutuhkan minimal satu slot waktu")
	}
	out := make([]TherapySlotTemplate, 0, len(in))
	for i, t := range in {
		start := strings.TrimSpace(t.StartTime)
		end := strings.TrimSpace(t.EndTime)
		if start == "" || end == "" {
			return nil, appErrs.BadRequest(fmt.Sprintf("slot %d: jam mulai dan selesai wajib diisi", i+1))
		}
		st, err := parseClock(start)
		if err != nil {
			return nil, appErrs.BadRequest(fmt.Sprintf("slot %d: jam mulai tidak valid", i+1))
		}
		en, err := parseClock(end)
		if err != nil {
			return nil, appErrs.BadRequest(fmt.Sprintf("slot %d: jam selesai tidak valid", i+1))
		}
		if !st.Before(en) {
			return nil, appErrs.BadRequest(fmt.Sprintf("slot %d: jam selesai harus setelah jam mulai", i+1))
		}
		ord := t.SortOrder
		if ord <= 0 {
			ord = i
		}
		capacity := t.Capacity
		if capacity <= 0 {
			capacity = 1
		}
		out = append(out, TherapySlotTemplate{
			StartTime: st.Format("15:04"),
			EndTime:   en.Format("15:04"),
			Capacity:  capacity,
			SortOrder: ord,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].StartTime < out[j].StartTime
	})
	return out, nil
}

func parseClock(s string) (time.Time, error) {
	s = padTime(strings.TrimSpace(s))
	t, err := time.Parse("15:04:05", s)
	if err != nil {
		return time.Parse("15:04", strings.TrimSpace(s))
	}
	return t, nil
}

func replaceTherapySlotTemplates(ctx context.Context, ts tenantScope, eventTherapyID, mode string, templates []TherapySlotTemplate) error {
	if _, err := ts.ExecContext(ctx, `
		DELETE FROM evt_event_therapy_slot_template WHERE event_therapy_id=$1::uuid`, eventTherapyID); err != nil {
		return appErrs.Internal(err.Error())
	}
	if mode != "MANUAL" || len(templates) == 0 {
		return nil
	}
	for i, t := range templates {
		ord := t.SortOrder
		if ord <= 0 {
			ord = i
		}
		capacity := t.Capacity
		if capacity <= 0 {
			capacity = 1
		}
		if _, err := ts.ExecContext(ctx, `
			INSERT INTO evt_event_therapy_slot_template (event_therapy_id, start_time, end_time, capacity, sort_order)
			VALUES ($1::uuid, $2::time, $3::time, $4, $5)`,
			eventTherapyID, t.StartTime, t.EndTime, capacity, ord); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return nil
}

func loadManualDaySlots(ctx context.Context, ts tenantScope, eventTherapyID string) ([]slotRange, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT start_time::text, end_time::text, capacity
		FROM evt_event_therapy_slot_template
		WHERE event_therapy_id=$1::uuid
		ORDER BY sort_order, start_time`, eventTherapyID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var out []slotRange
	for rows.Next() {
		var start, end string
		var capacity int
		if err := rows.Scan(&start, &end, &capacity); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		if capacity <= 0 {
			capacity = 1
		}
		out = append(out, slotRange{start: padTime(start), end: padTime(end), capacity: capacity})
	}
	if len(out) == 0 {
		return nil, appErrs.BadRequest("belum ada daftar slot manual — simpan pengaturan terapi dulu")
	}
	return out, rows.Err()
}
