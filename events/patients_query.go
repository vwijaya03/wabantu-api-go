package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type patientFilterInput struct {
	Q         string
	TherapyID string
	Status    string
	SlotDate  string
	HasSlot   string
	SortBy    string
	SortDir   string
}

func buildPatientWhere(eventID string, f patientFilterInput) (string, []any) {
	conds := []string{"pat.deleted_at IS NULL", "pat.event_id = $1::uuid"}
	args := []any{eventID}
	i := 2
	if tid := strings.TrimSpace(f.TherapyID); tid != "" {
		conds = append(conds, fmt.Sprintf("pat.therapy_id = $%d::uuid", i))
		args = append(args, tid)
		i++
	}
	if st := strings.TrimSpace(f.Status); st != "" {
		conds = append(conds, fmt.Sprintf("pat.reservation_status = $%d", i))
		args = append(args, strings.ToUpper(st))
		i++
	}
	// Name search (q) is applied after decrypt — see filterPatientsByNameQuery.
	if sd := strings.TrimSpace(f.SlotDate); sd != "" {
		conds = append(conds, fmt.Sprintf("s.slot_date = $%d::date", i))
		args = append(args, sd)
		i++
	}
	switch strings.ToLower(strings.TrimSpace(f.HasSlot)) {
	case "true":
		conds = append(conds, "pat.slot_id IS NOT NULL")
	case "false":
		conds = append(conds, "pat.slot_id IS NULL")
	}
	return strings.Join(conds, " AND "), args
}

const patientFromJoin = `
	FROM evt_patient pat
	JOIN evt_therapy t ON t.id = pat.therapy_id
	LEFT JOIN evt_time_slot s ON s.id = pat.slot_id`

const patientOrderBy = `
	ORDER BY t.display_order, s.slot_date NULLS LAST, s.start_time NULLS LAST, pat.created_at`

func scanPatientRows(rows *sql.Rows) ([]Patient, error) {
	var items []Patient
	for rows.Next() {
		var pat Patient
		var encName, encBirth string
		var slotID sql.NullString
		var slotDate, slotStart, slotEnd sql.NullString
		if err := rows.Scan(&pat.ID, &pat.EventID, &pat.TherapyID, &pat.TherapyName,
			&encName, &encBirth, &pat.Complaint, &pat.PreferredTime, &pat.ReservationStatus,
			&slotID, &slotDate, &slotStart, &slotEnd); err != nil {
			return nil, err
		}
		name, err := decryptPatientField(encName)
		if err != nil {
			return nil, appErrs.Internal("gagal membaca data pasien")
		}
		birth, err := decryptPatientField(encBirth)
		if err != nil {
			return nil, appErrs.Internal("gagal membaca data pasien")
		}
		pat.FullName = name
		pat.BirthDate = birth
		if slotID.Valid {
			pat.SlotID = &slotID.String
			if slotDate.Valid && slotStart.Valid {
				end := ""
				if slotEnd.Valid {
					end = slotEnd.String
				}
				pat.SlotLabel = formatPatientSlotLabel(slotDate.String, slotStart.String, end)
			}
		}
		items = append(items, pat)
	}
	if items == nil {
		items = []Patient{}
	}
	return items, nil
}

// filterPatientsByNameQuery keeps patients whose decrypted name contains the query (case-insensitive).
func filterPatientsByNameQuery(items []Patient, q string) []Patient {
	q = normalizePatientName(q)
	if q == "" {
		return items
	}
	out := make([]Patient, 0, len(items))
	for _, p := range items {
		if strings.Contains(normalizePatientName(p.FullName), q) {
			out = append(out, p)
		}
	}
	return out
}

func paginatePatientSlice(items []Patient, limit, offset int) ([]Patient, int) {
	total := len(items)
	if offset >= total {
		return []Patient{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total
}

func queryPatients(ctx context.Context, conn *sql.Conn, eventID string, f patientFilterInput, limit, offset int) ([]Patient, int, error) {
	if err := validatePatientFilters(f); err != nil {
		return nil, 0, err
	}
	orderBy, err := resolvePatientOrderBy(f.SortBy, f.SortDir)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = maxPatientListPageSize
	}
	if limit > maxPatientExportRows {
		limit = maxPatientExportRows
	}
	if offset < 0 {
		offset = 0
	}
	where, args := buildPatientWhere(eventID, f)
	nameQ := strings.TrimSpace(f.Q)
	inMemorySort := patientSortNeedsInMemory(f.SortBy)

	if nameQ != "" || inMemorySort {
		rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT pat.id::text, pat.event_id::text, pat.therapy_id::text, t.therapy_name,
		       pat.full_name_enc, pat.birth_date_enc, COALESCE(pat.complaint,''),
		       COALESCE(pat.preferred_time,''), pat.reservation_status, pat.slot_id::text,
		       s.slot_date::text, s.start_time::text, s.end_time::text
		%s WHERE %s %s LIMIT $%d`,
			patientFromJoin, where, orderBy, len(args)+1),
			append(append([]any{}, args...), maxPatientExportRows)...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		allItems, err := scanPatientRows(rows)
		if err != nil {
			return nil, 0, err
		}
		if nameQ != "" {
			allItems = filterPatientsByNameQuery(allItems, nameQ)
		}
		if inMemorySort {
			sortPatientsInMemory(allItems, f.SortBy, f.SortDir)
		}
		page, total := paginatePatientSlice(allItems, limit, offset)
		return page, total, nil
	}

	var total int
	countQ := `SELECT COUNT(*) ` + patientFromJoin + ` WHERE ` + where
	if err := conn.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append([]any{}, args...)
	qArgs = append(qArgs, limit, offset)
	limIdx := len(args) + 1
	offIdx := len(args) + 2
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT pat.id::text, pat.event_id::text, pat.therapy_id::text, t.therapy_name,
		       pat.full_name_enc, pat.birth_date_enc, COALESCE(pat.complaint,''),
		       COALESCE(pat.preferred_time,''), pat.reservation_status, pat.slot_id::text,
		       s.slot_date::text, s.start_time::text, s.end_time::text
		%s WHERE %s %s LIMIT $%d OFFSET $%d`,
		patientFromJoin, where, orderBy, limIdx, offIdx), qArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanPatientRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
