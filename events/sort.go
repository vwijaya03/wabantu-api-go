package events

import (
	"fmt"
	"sort"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

func normalizeSortDir(dir string, defaultDesc bool) string {
	d := strings.ToLower(strings.TrimSpace(dir))
	if d == "desc" {
		return "DESC"
	}
	if d == "asc" {
		return "ASC"
	}
	if defaultDesc {
		return "DESC"
	}
	return "ASC"
}

func validateSortDirOnly(sortDir string) error {
	d := strings.ToLower(strings.TrimSpace(sortDir))
	if d != "" && d != "asc" && d != "desc" {
		return appErrs.BadRequest("arah sort tidak valid")
	}
	return nil
}

func validateSortField(sortBy string, allowed map[string]bool) error {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" {
		return nil
	}
	if !allowed[sb] {
		return appErrs.BadRequest("urutan sort tidak valid")
	}
	return nil
}

// sqlOrderExpr builds "col ASC NULLS LAST" (PostgreSQL requires direction before NULLS LAST).
func sqlOrderExpr(col, dir string, nullsLast bool) string {
	if nullsLast {
		return fmt.Sprintf("%s %s NULLS LAST", col, dir)
	}
	return fmt.Sprintf("%s %s", col, dir)
}

var allowedEventSortFields = map[string]bool{
	"startdate": true, "eventname": true, "createdat": true, "status": true,
}

func resolveEventOrderBy(sortBy, sortDir string) (string, error) {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" {
		return "ORDER BY start_date DESC, created_at DESC", nil
	}
	if err := validateSortField(sortBy, allowedEventSortFields); err != nil {
		return "", err
	}
	if err := validateSortDirOnly(sortDir); err != nil {
		return "", err
	}
	dir := normalizeSortDir(sortDir, sb == "startdate")
	switch sb {
	case "startdate":
		return fmt.Sprintf("ORDER BY start_date %s, created_at DESC", dir), nil
	case "eventname":
		return fmt.Sprintf("ORDER BY event_name %s, start_date DESC", dir), nil
	case "createdat":
		return fmt.Sprintf("ORDER BY created_at %s", dir), nil
	case "status":
		return fmt.Sprintf("ORDER BY status %s, start_date DESC", dir), nil
	default:
		return "ORDER BY start_date DESC, created_at DESC", nil
	}
}

var allowedPatientSortFields = map[string]bool{
	"name": true, "therapy": true, "slotdate": true, "slottime": true, "status": true, "createdat": true,
}

func patientSortNeedsInMemory(sortBy string) bool {
	return strings.ToLower(strings.TrimSpace(sortBy)) == "name"
}

func resolvePatientOrderBy(sortBy, sortDir string) (string, error) {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" || sb == "therapy" {
		if err := validateSortField(sortBy, allowedPatientSortFields); err != nil {
			return "", err
		}
		if err := validateSortDirOnly(sortDir); err != nil {
			return "", err
		}
		return patientOrderBy, nil
	}
	if err := validateSortField(sortBy, allowedPatientSortFields); err != nil {
		return "", err
	}
	if err := validateSortDirOnly(sortDir); err != nil {
		return "", err
	}
	if sb == "name" {
		return patientOrderBy, nil
	}
	dir := normalizeSortDir(sortDir, false)
	switch sb {
	case "slotdate":
		return fmt.Sprintf("ORDER BY %s, %s, %s",
			sqlOrderExpr("s.slot_date", dir, true),
			sqlOrderExpr("s.start_time", dir, true),
			sqlOrderExpr("pat.created_at", dir, false),
		), nil
	case "slottime":
		return fmt.Sprintf("ORDER BY %s, %s, %s",
			sqlOrderExpr("s.start_time", dir, true),
			sqlOrderExpr("s.slot_date", dir, true),
			sqlOrderExpr("pat.created_at", dir, false),
		), nil
	case "status":
		return fmt.Sprintf("ORDER BY pat.reservation_status %s, t.display_order ASC, pat.created_at %s", dir, dir), nil
	case "createdat":
		return fmt.Sprintf("ORDER BY pat.created_at %s", dir), nil
	default:
		return patientOrderBy, nil
	}
}

func sortPatientsInMemory(items []Patient, sortBy, sortDir string) {
	if strings.ToLower(strings.TrimSpace(sortBy)) != "name" {
		return
	}
	desc := normalizeSortDir(sortDir, false) == "DESC"
	sort.Slice(items, func(i, j int) bool {
		a := normalizePatientName(items[i].FullName)
		b := normalizePatientName(items[j].FullName)
		if desc {
			return a > b
		}
		return a < b
	})
}

var allowedPeopleSortFields = map[string]bool{"name": true, "persontype": true, "createdat": true}

func peopleSortNeedsInMemory(sortBy string) bool {
	return strings.ToLower(strings.TrimSpace(sortBy)) == "name"
}

func resolvePeopleOrderBy(sortBy, sortDir string) (string, error) {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" || sb == "persontype" {
		if err := validateSortField(sortBy, allowedPeopleSortFields); err != nil {
			return "", err
		}
		if err := validateSortDirOnly(sortDir); err != nil {
			return "", err
		}
		dir := normalizeSortDir(sortDir, false)
		if sb == "" {
			return "ORDER BY p.person_type ASC, p.created_at ASC", nil
		}
		return fmt.Sprintf("ORDER BY p.person_type %s, p.created_at %s", dir, dir), nil
	}
	if err := validateSortField(sortBy, allowedPeopleSortFields); err != nil {
		return "", err
	}
	if err := validateSortDirOnly(sortDir); err != nil {
		return "", err
	}
	if sb == "name" {
		return "ORDER BY p.person_type ASC, p.created_at ASC", nil
	}
	dir := normalizeSortDir(sortDir, false)
	return fmt.Sprintf("ORDER BY p.created_at %s", dir), nil
}

func sortEventPeopleInMemory(items []EventPerson, sortBy, sortDir string) {
	if strings.ToLower(strings.TrimSpace(sortBy)) != "name" {
		return
	}
	desc := normalizeSortDir(sortDir, false) == "DESC"
	sort.Slice(items, func(i, j int) bool {
		a := strings.ToLower(strings.TrimSpace(items[i].FullName))
		b := strings.ToLower(strings.TrimSpace(items[j].FullName))
		if desc {
			return a > b
		}
		return a < b
	})
}

var allowedAssignmentSortFields = map[string]bool{
	"taskname": true, "personname": true, "starttime": true, "createdat": true,
}

func assignmentSortNeedsInMemory(sortBy string) bool {
	return strings.ToLower(strings.TrimSpace(sortBy)) == "personname"
}

func resolveAssignmentOrderBy(sortBy, sortDir string) (string, error) {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" || sb == "starttime" {
		if err := validateSortField(sortBy, allowedAssignmentSortFields); err != nil {
			return "", err
		}
		if err := validateSortDirOnly(sortDir); err != nil {
			return "", err
		}
		if sb == "" {
			return "ORDER BY tk.display_order, a.start_time NULLS LAST, p.created_at", nil
		}
		dir := normalizeSortDir(sortDir, false)
		return fmt.Sprintf("ORDER BY %s, tk.display_order ASC, %s",
			sqlOrderExpr("a.start_time", dir, true),
			sqlOrderExpr("p.created_at", dir, false),
		), nil
	}
	if err := validateSortField(sortBy, allowedAssignmentSortFields); err != nil {
		return "", err
	}
	if err := validateSortDirOnly(sortDir); err != nil {
		return "", err
	}
	if sb == "personname" {
		return "ORDER BY tk.display_order, a.start_time NULLS LAST, p.created_at", nil
	}
	dir := normalizeSortDir(sortDir, false)
	switch sb {
	case "taskname":
		return fmt.Sprintf("ORDER BY %s, %s",
			sqlOrderExpr("tk.task_name", dir, false),
			sqlOrderExpr("a.start_time", "ASC", true),
		), nil
	case "createdat":
		return fmt.Sprintf("ORDER BY p.created_at %s", dir), nil
	default:
		return "ORDER BY tk.display_order, a.start_time NULLS LAST, p.created_at", nil
	}
}

func sortAssignmentsInMemory(items []Assignment, sortBy, sortDir string) {
	if strings.ToLower(strings.TrimSpace(sortBy)) != "personname" {
		return
	}
	desc := normalizeSortDir(sortDir, false) == "DESC"
	sort.Slice(items, func(i, j int) bool {
		a := strings.ToLower(strings.TrimSpace(items[i].PersonName))
		b := strings.ToLower(strings.TrimSpace(items[j].PersonName))
		if desc {
			return a > b
		}
		return a < b
	})
}

var allowedStaffExportSortFields = map[string]bool{"name": true, "persontype": true}

func validateStaffExportSort(sortBy, sortDir string) error {
	if err := validateSortField(sortBy, allowedStaffExportSortFields); err != nil {
		return err
	}
	return validateSortDirOnly(sortDir)
}

func sortStaffListRowsInMemory(rows []staffListRow, sortBy, sortDir string) {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" || sb == "persontype" {
		dirDesc := normalizeSortDir(sortDir, false) == "DESC"
		sort.Slice(rows, func(i, j int) bool {
			aType, bType := rows[i].PersonType, rows[j].PersonType
			if aType != bType {
				if dirDesc {
					return aType > bType
				}
				return aType < bType
			}
			aName := strings.ToLower(rows[i].FullName)
			bName := strings.ToLower(rows[j].FullName)
			return aName < bName
		})
		return
	}
	if sb != "name" {
		return
	}
	desc := normalizeSortDir(sortDir, false) == "DESC"
	sort.Slice(rows, func(i, j int) bool {
		a := strings.ToLower(rows[i].FullName)
		b := strings.ToLower(rows[j].FullName)
		if desc {
			return a > b
		}
		return a < b
	})
}

func sortStaffSheetRowsInMemory(rows []staffSheetRow, sortBy, sortDir string) {
	sb := strings.ToLower(strings.TrimSpace(sortBy))
	if sb == "" {
		sb = "name"
	}
	desc := normalizeSortDir(sortDir, false) == "DESC"
	sort.Slice(rows, func(i, j int) bool {
		if sb == "persontype" {
			return false
		}
		a := strings.ToLower(rows[i].FullName)
		b := strings.ToLower(rows[j].FullName)
		if desc {
			return a > b
		}
		return a < b
	})
}
