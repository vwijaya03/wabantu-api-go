package events

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

type staffSheetRow struct {
	Timestamp        time.Time
	FullName         string
	AttendanceLabel  string
	TherapyNames     string
}

type volunteerSlot struct {
	RoleLabel string
	Name      string
}

type hourlyAssignment struct {
	Label string
	Name  string
}

type sessionAssignment struct {
	TaskLabel string
	Sessions  map[string]string // session name -> person name
	FixedName string
}

type staffSheetExportData struct {
	EventName       string
	TherapyStaff    []staffSheetRow
	Volunteers      []volunteerSlot
	HourlyMedang    []hourlyAssignment
	SessionTasks    []sessionAssignment
}

func loadStaffSheetExportData(ctx context.Context, ts tenantScope, eventID string) (staffSheetExportData, error) {
	var data staffSheetExportData
	if err := ts.QueryRowContext(ctx, `
		SELECT event_name FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventID,
	).Scan(&data.EventName); err != nil {
		return staffSheetExportData{}, err
	}

	rows, err := ts.QueryContext(ctx, `
		SELECT p.created_at, `+personNameEncLegacyColsP+`, p.attendance_status,
		       COALESCE(p.notes,''), p.arrival_time::text, p.departure_time::text,
		       COALESCE(string_agg(th.therapy_name, ', ' ORDER BY th.display_order, th.therapy_name), '')
		FROM evt_event_person p
		LEFT JOIN evt_person_therapy pt ON pt.person_id = p.id
		LEFT JOIN evt_therapy th ON th.id = pt.therapy_id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL
		  AND p.person_type IN ('THERAPIST','SHIJIE','DAOSHI','FASHI')
		GROUP BY p.id, p.created_at, p.full_name_enc, p.full_name, p.attendance_status, p.notes, p.arrival_time, p.departure_time
		ORDER BY p.created_at, p.created_at`, eventID)
	if err != nil {
		return staffSheetExportData{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r staffSheetRow
		var att, notes string
		var arr, dep sql.NullString
		var nameEnc, nameLegacy string
		if err := rows.Scan(&r.Timestamp, &nameEnc, &nameLegacy, &att, &notes, &arr, &dep, &r.TherapyNames); err != nil {
			return staffSheetExportData{}, err
		}
		r.FullName, err = scanPersonNameFromRow(nameEnc, nameLegacy)
		if err != nil {
			return staffSheetExportData{}, err
		}
		r.AttendanceLabel = attendanceExportLabel(att, notes, arr, dep)
		data.TherapyStaff = append(data.TherapyStaff, r)
	}
	if err := rows.Err(); err != nil {
		return staffSheetExportData{}, err
	}

	volRows, err := ts.QueryContext(ctx, `
		SELECT LOWER(vr.role_name), `+personNameEncLegacyColsP+`
		FROM evt_event_person p
		JOIN evt_event_volunteer ev ON ev.person_id = p.id
		JOIN evt_volunteer_role vr ON vr.id = ev.volunteer_role_id
		WHERE p.event_id=$1::uuid AND p.deleted_at IS NULL AND p.person_type='VOLUNTEER'
		ORDER BY vr.display_order, p.created_at`, eventID)
	if err != nil {
		return staffSheetExportData{}, err
	}
	defer volRows.Close()
	roleNames := []struct{ key, label string }{
		{"relawan depan", "relawan depan"},
		{"relawan bakar fu", "relawan bakar fu"},
		{"relawan pintu keluar", "relawan pintu keluar, nandain name tag"},
	}
	byRole := map[string][]string{}
	for volRows.Next() {
		var roleKey, nameEnc, nameLegacy string
		if err := volRows.Scan(&roleKey, &nameEnc, &nameLegacy); err != nil {
			return staffSheetExportData{}, err
		}
		name, err := scanPersonNameFromRow(nameEnc, nameLegacy)
		if err != nil {
			return staffSheetExportData{}, err
		}
		byRole[normalizeVolunteerRoleKey(roleKey)] = append(byRole[normalizeVolunteerRoleKey(roleKey)], name)
	}
	for _, rn := range roleNames {
		names := byRole[rn.key]
		label := rn.label
		val := "-"
		if len(names) > 0 {
			val = strings.Join(names, ", ")
		}
		data.Volunteers = append(data.Volunteers, volunteerSlot{RoleLabel: label, Name: val})
	}

	assignRows, err := ts.QueryContext(ctx, `
		SELECT tk.task_name, tk.assignment_type, p.person_type, `+personNameEncLegacyColsP+`,
		       a.start_time::text, a.end_time::text, COALESCE(a.session_name,'')
		FROM evt_event_assignment a
		JOIN evt_task tk ON tk.id = a.task_id
		JOIN evt_event_person p ON p.id = a.person_id
		WHERE a.event_id=$1::uuid AND a.deleted_at IS NULL
		ORDER BY tk.display_order, a.start_time NULLS LAST, a.session_name, p.created_at`, eventID)
	if err != nil {
		return staffSheetExportData{}, err
	}
	defer assignRows.Close()

	perSession := map[string]map[string]string{}
	var fixedTasks []sessionAssignment

	for assignRows.Next() {
		var taskName, assignType, personType, sessionName string
		var nameEnc, nameLegacy string
		var startT, endT sql.NullString
		if err := assignRows.Scan(&taskName, &assignType, &personType, &nameEnc, &nameLegacy, &startT, &endT, &sessionName); err != nil {
			return staffSheetExportData{}, err
		}
		personName, err := scanPersonNameFromRow(nameEnc, nameLegacy)
		if err != nil {
			return staffSheetExportData{}, err
		}
		displayName := assignmentDisplayName(personType, personName)
		switch strings.ToUpper(assignType) {
		case "PER_HOUR":
			if !strings.EqualFold(strings.TrimSpace(taskName), "Medang") {
				continue
			}
			st, en := formatExportTime(exportTimeFromNull(startT)), formatExportTime(exportTimeFromNull(endT))
			label := fmt.Sprintf("petugas pedang jam %s - %s", st, en)
			data.HourlyMedang = append(data.HourlyMedang, hourlyAssignment{Label: label, Name: displayName})
		case "PER_SESSION":
			key := taskExportLabel(taskName)
			if perSession[key] == nil {
				perSession[key] = map[string]string{}
			}
			sn := strings.TrimSpace(sessionName)
			if sn == "" {
				sn = "Sesi"
			}
			perSession[key][sn] = displayName
		case "FIXED":
			fixedTasks = append(fixedTasks, sessionAssignment{
				TaskLabel: taskExportLabel(taskName),
				FixedName: displayName,
			})
		}
	}

	for key, sessions := range perSession {
		data.SessionTasks = append(data.SessionTasks, sessionAssignment{
			TaskLabel: key,
			Sessions:  sessions,
		})
	}
	sort.Slice(data.SessionTasks, func(i, j int) bool {
		return data.SessionTasks[i].TaskLabel < data.SessionTasks[j].TaskLabel
	})
	data.SessionTasks = append(data.SessionTasks, fixedTasks...)

	sort.Slice(data.HourlyMedang, func(i, j int) bool {
		return data.HourlyMedang[i].Label < data.HourlyMedang[j].Label
	})

	return data, nil
}

func attendanceExportLabel(status, notes string, arr, dep sql.NullString) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PRESENT":
		return "Bisa"
	case "NOT_PRESENT":
		return "Tidak bisa"
	case "PARTIAL":
		if n := strings.TrimSpace(notes); n != "" {
			return n
		}
		if arr.Valid && dep.Valid {
			return fmt.Sprintf("%s sampai %s", formatExportTime(arr.String), formatExportTime(dep.String))
		}
		if arr.Valid {
			return "Dari " + formatExportTime(arr.String)
		}
		if dep.Valid {
			return "Sampai " + formatExportTime(dep.String)
		}
		return "Sebagian"
	default:
		return "Bisa"
	}
}

func exportTimeFromNull(n sql.NullString) string {
	if n.Valid {
		return n.String
	}
	return ""
}

func assignmentDisplayName(personType, name string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	switch strings.ToUpper(strings.TrimSpace(personType)) {
	case "FASHI":
		if !strings.HasPrefix(lower, "fashi") {
			return "Fashi " + name
		}
	case "DAOSHI":
		if !strings.HasPrefix(lower, "daoshi") {
			return "Daoshi " + name
		}
	}
	return name
}

func formatExportTime(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

func normalizeVolunteerRoleKey(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	role = strings.ReplaceAll(role, "relawan ", "")
	if strings.Contains(role, "depan") {
		return "relawan depan"
	}
	if strings.Contains(role, "bakar") || strings.Contains(role, "fu") {
		return "relawan bakar fu"
	}
	if strings.Contains(role, "pintu") || strings.Contains(role, "keluar") {
		return "relawan pintu keluar"
	}
	return role
}

func taskExportLabel(taskName string) string {
	switch strings.TrimSpace(strings.ToLower(taskName)) {
	case "scan barrier":
		return "petugas scan & bikin barrier"
	case "re-scan":
		return "petugas re-scan"
	case "koordinator tengah":
		return "petugas koordinator tengah"
	default:
		return "petugas " + strings.ToLower(taskName)
	}
}

func buildStaffSheetXLSX(data staffSheetExportData) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Operasional"
	_ = f.SetSheetName("Sheet1", sheet)

	styles, err := newExportXLSXStyles(f, staffExportTheme)
	if err != nil {
		return nil, err
	}

	const colCount = 4
	applyStaffSheetColumnWidths(f, sheet)

	_ = writeExportTitleBlock(f, sheet, "Lembar Operasional Staf",
		exportSubtitleLines(data.EventName, "", ""), colCount, styles)

	row := 4
	headers := []string{"Timestamp", "Nama Terapis", "Apakah Bisa Datang?", "Terapi Yang Anda Pilih"}
	_ = writeExportTableHeader(f, sheet, row, headers, styles.header)
	row++

	for i, s := range data.TherapyStaff {
		vals := []any{
			s.Timestamp.Format("2/1/2006 15:04"),
			s.FullName,
			s.AttendanceLabel,
			s.TherapyNames,
		}
		_ = writeExportDataRow(f, sheet, row, vals, styles.body, styles.bodyAlt, i%2 == 1)
		row++
	}

	row++
	writeExportSectionTitle(f, sheet, "Para Fashi dan Daoshi saling bergantian menjadi sukarelawan", row, colCount, styles.section)
	row++
	_ = writeExportTableHeader(f, sheet, row, []string{"Peran relawan", "Nama", "", ""}, styles.header)
	row++
	for i, v := range data.Volunteers {
		vals := []any{v.RoleLabel, v.Name, "", ""}
		_ = writeExportDataRow(f, sheet, row, vals, styles.body, styles.bodyAlt, i%2 == 1)
		row++
	}

	row++
	writeExportSectionTitle(f, sheet, "Per 1 Jam Fashi Gonta Ganti Medang", row, colCount, styles.section)
	row++
	_ = writeExportTableHeader(f, sheet, row, []string{"Jam", "Petugas pedang", "", ""}, styles.header)
	row++
	for i, h := range data.HourlyMedang {
		vals := []any{h.Label, h.Name, "", ""}
		_ = writeExportDataRow(f, sheet, row, vals, styles.body, styles.bodyAlt, i%2 == 1)
		row++
	}

	row++
	writeExportSectionTitle(f, sheet, "Penugasan sesi", row, colCount, styles.section)
	row++
	_ = writeExportTableHeader(f, sheet, row, []string{"Tugas", "Penugasan", "", ""}, styles.header)
	row++
	for i, t := range data.SessionTasks {
		assignee := t.FixedName
		if assignee == "" {
			assignee = sessionAssignmentParts(t.Sessions)
		}
		vals := []any{t.TaskLabel, assignee, "", ""}
		_ = writeExportDataRow(f, sheet, row, vals, styles.body, styles.bodyAlt, i%2 == 1)
		row++
	}

	freezeExportHeader(f, sheet, 4)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sessionAssignmentParts(sessions map[string]string) string {
	if len(sessions) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(sessions))
	for k := range sessions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, sessions[k]))
	}
	return strings.Join(parts, " | ")
}

func pdfDataURL(pdf []byte) string {
	return "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(pdf)
}

func xlsxDataURL(xlsx []byte) string {
	return "data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64," +
		base64.StdEncoding.EncodeToString(xlsx)
}
