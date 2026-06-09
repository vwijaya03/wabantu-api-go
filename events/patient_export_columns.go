package events

import (
	"fmt"
	"strings"
)

const (
	patientColBirthDate      = "birthDate"
	patientColTherapy        = "therapy"
	patientColComplaint      = "complaint"
	patientColPreferredTime  = "preferredTime"
	patientColStatus         = "status"
	patientColSlot           = "slot"
)

var patientExportColumnKeys = map[string]struct{}{
	patientColBirthDate:     {},
	patientColTherapy:       {},
	patientColComplaint:     {},
	patientColPreferredTime: {},
	patientColStatus:        {},
	patientColSlot:          {},
}

type patientExportCol struct {
	Key    string
	Header string
	Width  float64
	Align  string
	Value  func(row Patient, no int) string
}

func parsePatientHiddenColumns(cols []string) map[string]bool {
	hidden := map[string]bool{}
	for _, c := range cols {
		c = strings.TrimSpace(c)
		if _, ok := patientExportColumnKeys[c]; ok {
			hidden[c] = true
		}
	}
	return hidden
}

func patientExportColumns(hidden map[string]bool) []patientExportCol {
	all := []patientExportCol{
		{Key: "no", Header: "No", Width: 8, Align: "C", Value: func(row Patient, no int) string {
			_ = row
			return fmt.Sprintf("%d", no)
		}},
		{Key: "name", Header: "Nama", Width: 38, Align: "L", Value: func(row Patient, _ int) string {
			return row.FullName
		}},
		{Key: patientColBirthDate, Header: "Tgl lahir", Width: 22, Align: "L", Value: func(row Patient, _ int) string {
			return formatEventDateID(row.BirthDate)
		}},
		{Key: patientColTherapy, Header: "Terapi", Width: 28, Align: "L", Value: func(row Patient, _ int) string {
			return row.TherapyName
		}},
		{Key: patientColComplaint, Header: "Keluhan", Width: 42, Align: "L", Value: func(row Patient, _ int) string {
			return truncateText(row.Complaint, 120)
		}},
		{Key: patientColPreferredTime, Header: "Preferensi", Width: 22, Align: "L", Value: func(row Patient, _ int) string {
			return row.PreferredTime
		}},
		{Key: patientColStatus, Header: "Status", Width: 20, Align: "L", Value: func(row Patient, _ int) string {
			return row.ReservationStatus
		}},
		{Key: patientColSlot, Header: "Slot/Jadwal", Width: 30, Align: "L", Value: func(row Patient, _ int) string {
			if row.SlotLabel != "" {
				return row.SlotLabel
			}
			if row.PreferredTime != "" {
				return "Preferensi: " + row.PreferredTime
			}
			return ""
		}},
	}
	if len(hidden) == 0 {
		return all
	}
	out := make([]patientExportCol, 0, len(all))
	for _, c := range all {
		if c.Key == "no" || c.Key == "name" || !hidden[c.Key] {
			out = append(out, c)
		}
	}
	return out
}

func hiddenColumnList(hidden map[string]bool) []string {
	if len(hidden) == 0 {
		return nil
	}
	out := make([]string, 0, len(hidden))
	for k := range hidden {
		out = append(out, k)
	}
	return out
}

func scaleColumnWidths(cols []patientExportCol, pageWidth float64) []float64 {
	if len(cols) == 0 {
		return nil
	}
	total := 0.0
	for _, c := range cols {
		total += c.Width
	}
	if total <= 0 {
		total = 1
	}
	scale := pageWidth / total
	widths := make([]float64, len(cols))
	for i, c := range cols {
		widths[i] = c.Width * scale
	}
	return widths
}
