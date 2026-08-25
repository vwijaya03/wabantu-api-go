package events

import (
	"bytes"
	"context"
	"database/sql"
	"strings"

	"github.com/xuri/excelize/v2"
)

type staffListRow struct {
	FullName         string
	PersonType       string
	RoleLabel        string
	AttendanceStatus string
	TherapyNames     string
	VolunteerRole    string
	IsPencatat       bool
	Notes            string
}

func loadStaffListRows(ctx context.Context, ts tenantScope, eventID string) ([]staffListRow, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT `+personNameEncLegacyColsP+`, p.person_type, p.attendance_status, COALESCE(p.notes,''),
		       COALESCE(string_agg(t.therapy_name, ', ' ORDER BY t.therapy_name), ''),
		       vr.role_name, COALESCE(ev.is_pencatat, false)
		FROM evt_event_person p
		LEFT JOIN evt_person_therapy pt ON pt.person_id = p.id
		LEFT JOIN evt_therapy t ON t.id = pt.therapy_id
		LEFT JOIN evt_event_volunteer ev ON ev.person_id = p.id
		LEFT JOIN evt_volunteer_role vr ON vr.id = ev.volunteer_role_id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		GROUP BY p.id, p.full_name_enc, p.full_name, p.person_type, p.attendance_status, p.notes, vr.role_name, ev.is_pencatat
		ORDER BY p.person_type, p.created_at`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []staffListRow
	for rows.Next() {
		var r staffListRow
		var volRole sql.NullString
		var nameEnc, nameLegacy string
		if err := rows.Scan(&nameEnc, &nameLegacy, &r.PersonType, &r.AttendanceStatus, &r.Notes,
			&r.TherapyNames, &volRole, &r.IsPencatat); err != nil {
			return nil, err
		}
		r.FullName, err = scanPersonNameFromRow(nameEnc, nameLegacy)
		if err != nil {
			return nil, err
		}
		r.RoleLabel = personTypeToRole(r.PersonType)
		if volRole.Valid {
			r.VolunteerRole = volRole.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func attendanceStatusExportLabel(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PRESENT":
		return "Bisa"
	case "PARTIAL":
		return "Sebagian"
	case "ABSENT":
		return "Tidak bisa"
	case "UNKNOWN", "":
		return "Belum diisi"
	default:
		return status
	}
}

func personTypeExportLabel(pt string) string {
	switch strings.ToUpper(pt) {
	case "THERAPIST":
		return "Terapis"
	case "VOLUNTEER":
		return "Relawan"
	case "SHIJIE":
		return "Shijie"
	case "DAOSHI":
		return "Daoshi"
	case "FASHI":
		return "Fashi"
	default:
		return pt
	}
}

func buildStaffListXLSX(eventName string, rows []staffListRow) ([]byte, error) {
	f := excelize.NewFile()
	sheet := "Staf"
	_ = f.SetSheetName("Sheet1", sheet)

	styles, err := newExportXLSXStyles(f, staffExportTheme)
	if err != nil {
		return nil, err
	}

	const colCount = 8
	headerRow := 4
	_ = writeExportTitleBlock(f, sheet, "Daftar Staf", exportSubtitleLines(eventName, "", ""), colCount, styles)

	headers := []string{"No", "Nama", "Peran", "Kehadiran", "Terapi", "Peran relawan", "Pencatat", "Catatan"}
	_ = writeExportTableHeader(f, sheet, headerRow, headers, styles.header)
	applyStaffListColumnWidths(f, sheet)

	for i, row := range rows {
		r := headerRow + 1 + i
		pencatat := "Tidak"
		if row.IsPencatat {
			pencatat = "Ya"
		}
		vals := []any{
			i + 1,
			row.FullName,
			personTypeExportLabel(row.PersonType),
			attendanceStatusExportLabel(row.AttendanceStatus),
			row.TherapyNames,
			row.VolunteerRole,
			pencatat,
			row.Notes,
		}
		_ = writeExportDataRow(f, sheet, r, vals, styles.body, styles.bodyAlt, i%2 == 1)
	}

	freezeExportHeader(f, sheet, headerRow)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
