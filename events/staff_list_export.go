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

func loadStaffListRows(ctx context.Context, conn *sql.Conn, eventID string) ([]staffListRow, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT p.full_name, p.person_type, p.attendance_status, COALESCE(p.notes,''),
		       COALESCE(string_agg(t.therapy_name, ', ' ORDER BY t.therapy_name), ''),
		       vr.role_name, COALESCE(ev.is_pencatat, false)
		FROM evt_event_person p
		LEFT JOIN evt_person_therapy pt ON pt.person_id = p.id
		LEFT JOIN evt_therapy t ON t.id = pt.therapy_id
		LEFT JOIN evt_event_volunteer ev ON ev.person_id = p.id
		LEFT JOIN evt_volunteer_role vr ON vr.id = ev.volunteer_role_id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		GROUP BY p.id, p.full_name, p.person_type, p.attendance_status, p.notes, vr.role_name, ev.is_pencatat
		ORDER BY p.person_type, p.full_name`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []staffListRow
	for rows.Next() {
		var r staffListRow
		var volRole sql.NullString
		if err := rows.Scan(&r.FullName, &r.PersonType, &r.AttendanceStatus, &r.Notes,
			&r.TherapyNames, &volRole, &r.IsPencatat); err != nil {
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

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#6D28D9"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})

	_ = f.SetCellValue(sheet, "A1", "Acara: "+eventName)
	headers := []string{"No", "Nama", "Peran", "Kehadiran", "Terapi", "Peran relawan", "Pencatat", "Catatan"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	for i, row := range rows {
		r := i + 4
		pencatat := "Tidak"
		if row.IsPencatat {
			pencatat = "Ya"
		}
		vals := []any{
			i + 1,
			row.FullName,
			personTypeExportLabel(row.PersonType),
			row.AttendanceStatus,
			row.TherapyNames,
			row.VolunteerRole,
			pencatat,
			row.Notes,
		}
		for c, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(c+1, r)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}

	_ = f.SetColWidth(sheet, "A", "A", 6)
	_ = f.SetColWidth(sheet, "B", "B", 28)
	_ = f.SetColWidth(sheet, "C", "H", 20)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
