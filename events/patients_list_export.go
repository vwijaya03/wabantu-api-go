package events

import (
	"bytes"

	"github.com/xuri/excelize/v2"
)

func buildPatientsXLSX(data patientPDFData) ([]byte, error) {
	f := excelize.NewFile()
	sheet := "Pasien"
	_ = f.SetSheetName("Sheet1", sheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#059669"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})

	headers := []string{"No", "Nama", "Tgl lahir", "Terapi", "Keluhan", "Jam preferensi", "Status", "Jadwal slot"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	for i, row := range data.Rows {
		r := i + 2
		birth := formatEventDateID(row.BirthDate)
		slot := row.SlotLabel
		if slot == "" && row.PreferredTime != "" {
			slot = "Preferensi: " + row.PreferredTime
		}
		vals := []any{i + 1, row.FullName, birth, row.TherapyName, row.Complaint, row.PreferredTime, row.ReservationStatus, slot}
		for c, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(c+1, r)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}

	_ = f.SetColWidth(sheet, "A", "A", 6)
	_ = f.SetColWidth(sheet, "B", "B", 28)
	_ = f.SetColWidth(sheet, "C", "C", 18)
	_ = f.SetColWidth(sheet, "D", "D", 22)
	_ = f.SetColWidth(sheet, "E", "E", 36)
	_ = f.SetColWidth(sheet, "F", "H", 18)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
