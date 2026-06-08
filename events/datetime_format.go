package events

import (
	"fmt"
	"strings"
	"time"
)

var idMonthNames = []string{
	"",
	"Januari",
	"Februari",
	"Maret",
	"April",
	"Mei",
	"Juni",
	"Juli",
	"Agustus",
	"September",
	"Oktober",
	"November",
	"Desember",
}

func formatEventDateID(dateStr string) string {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return ""
	}
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	m := int(d.Month())
	if m < 1 || m > 12 {
		return dateStr
	}
	return fmt.Sprintf("%d %s %d", d.Day(), idMonthNames[m], d.Year())
}

func formatEventTimeHM(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

// formatPatientSlotLabel builds e.g. "14 Juni 2026 09:00–09:30".
func formatPatientSlotLabel(slotDate, startTime, endTime string) string {
	slotDate = strings.TrimSpace(slotDate)
	if slotDate == "" {
		return ""
	}
	datePart := formatEventDateID(slotDate)
	start := formatEventTimeHM(startTime)
	if start == "" {
		return datePart
	}
	end := formatEventTimeHM(endTime)
	if end != "" && end != start {
		return datePart + " " + start + "–" + end
	}
	return datePart + " " + start
}
