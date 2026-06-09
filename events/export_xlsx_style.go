package events

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

type exportXLSXTheme struct {
	HeaderFill string
	HeaderFont string
	TitleFill  string
	TitleFont  string
	ZebraFill  string
}

var (
	patientExportTheme = exportXLSXTheme{
		HeaderFill: "#047857",
		HeaderFont: "#FFFFFF",
		TitleFill:  "#ECFDF5",
		TitleFont:  "#065F46",
		ZebraFill:  "#F9FAFB",
	}
	staffExportTheme = exportXLSXTheme{
		HeaderFill: "#6D28D9",
		HeaderFont: "#FFFFFF",
		TitleFill:  "#F5F3FF",
		TitleFont:  "#5B21B6",
		ZebraFill:  "#F9FAFB",
	}
)

type exportXLSXStyles struct {
	title      int
	subtitle   int
	header     int
	body       int
	bodyAlt    int
	section    int
	bodyCenter int
}

func newExportXLSXStyles(f *excelize.File, theme exportXLSXTheme) (exportXLSXStyles, error) {
	border := []excelize.Border{
		{Type: "left", Color: "#E5E7EB", Style: 1},
		{Type: "right", Color: "#E5E7EB", Style: 1},
		{Type: "top", Color: "#E5E7EB", Style: 1},
		{Type: "bottom", Color: "#E5E7EB", Style: 1},
	}
	title, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14, Color: theme.TitleFont},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{theme.TitleFill}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	subtitle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#4B5563"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{theme.TitleFill}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	header, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: theme.HeaderFont},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{theme.HeaderFill}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    border,
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	body, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#111827"},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true, Indent: 1},
		Border:    border,
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	bodyAlt, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#111827"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{theme.ZebraFill}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true, Indent: 1},
		Border:    border,
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	section, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: theme.TitleFont},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{theme.TitleFill}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true, Indent: 1},
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	bodyCenter, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#111827"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    border,
	})
	if err != nil {
		return exportXLSXStyles{}, err
	}
	return exportXLSXStyles{
		title: title, subtitle: subtitle, header: header,
		body: body, bodyAlt: bodyAlt, section: section, bodyCenter: bodyCenter,
	}, nil
}

func writeExportTitleBlock(f *excelize.File, sheet, title, subtitle string, colCount int, styles exportXLSXStyles) error {
	if colCount < 1 {
		colCount = 1
	}
	lastCol, _ := excelize.ColumnNumberToName(colCount)
	_ = f.SetCellValue(sheet, "A1", title)
	_ = f.MergeCell(sheet, "A1", lastCol+"1")
	_ = f.SetCellStyle(sheet, "A1", lastCol+"1", styles.title)
	_ = f.SetRowHeight(sheet, 1, 28)

	_ = f.SetCellValue(sheet, "A2", subtitle)
	_ = f.MergeCell(sheet, "A2", lastCol+"2")
	_ = f.SetCellStyle(sheet, "A2", lastCol+"2", styles.subtitle)
	_ = f.SetRowHeight(sheet, 2, 22)
	return nil
}

func writeExportTableHeader(f *excelize.File, sheet string, row int, headers []string, styleID int) error {
	_ = f.SetRowHeight(sheet, row, 26)
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, row)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, styleID)
	}
	return nil
}

func writeExportDataRow(f *excelize.File, sheet string, row int, values []any, styleID int, altStyleID int, zebra bool) error {
	_ = f.SetRowHeight(sheet, row, 22)
	style := styleID
	if zebra {
		style = altStyleID
	}
	for c, v := range values {
		cell, _ := excelize.CoordinatesToCellName(c+1, row)
		_ = f.SetCellValue(sheet, cell, v)
		_ = f.SetCellStyle(sheet, cell, cell, style)
	}
	return nil
}

func freezeExportHeader(f *excelize.File, sheet string, headerRow int) {
	showGrid := false
	_ = f.SetSheetView(sheet, -1, &excelize.ViewOptions{ShowGridLines: &showGrid})
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1),
		ActivePane:  "bottomLeft",
	})
}

func writeExportSectionTitle(f *excelize.File, sheet, text string, row, colCount int, styleID int) {
	if colCount < 1 {
		colCount = 1
	}
	lastCol, _ := excelize.ColumnNumberToName(colCount)
	cell := fmt.Sprintf("A%d", row)
	_ = f.SetCellValue(sheet, cell, text)
	_ = f.MergeCell(sheet, cell, fmt.Sprintf("%s%d", lastCol, row))
	_ = f.SetCellStyle(sheet, cell, fmt.Sprintf("%s%d", lastCol, row), styleID)
	_ = f.SetRowHeight(sheet, row, 24)
}

func patientColumnWidth(key string) float64 {
	switch key {
	case "no":
		return 6
	case "name":
		return 32
	case patientColBirthDate:
		return 18
	case patientColTherapy:
		return 26
	case patientColComplaint:
		return 42
	case patientColPreferredTime:
		return 14
	case patientColStatus:
		return 14
	case patientColSlot:
		return 28
	default:
		return 18
	}
}

func applyColumnWidths(f *excelize.File, sheet string, cols []patientExportCol) {
	for i, c := range cols {
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, colName, colName, patientColumnWidth(c.Key))
	}
}

func staffListColumnWidths() map[int]float64 {
	return map[int]float64{
		1: 7, 2: 30, 3: 14, 4: 16, 5: 28, 6: 22, 7: 12, 8: 36,
	}
}

func applyStaffListColumnWidths(f *excelize.File, sheet string) {
	for col, w := range staffListColumnWidths() {
		name, _ := excelize.ColumnNumberToName(col)
		_ = f.SetColWidth(sheet, name, name, w)
	}
}

func staffSheetColumnWidths() map[int]float64 {
	return map[int]float64{1: 24, 2: 32, 3: 22, 4: 52}
}

func applyStaffSheetColumnWidths(f *excelize.File, sheet string) {
	for col, w := range staffSheetColumnWidths() {
		name, _ := excelize.ColumnNumberToName(col)
		_ = f.SetColWidth(sheet, name, name, w)
	}
}

func exportSubtitleLines(eventName, filterSummary, generatedAt string) string {
	parts := []string{}
	if strings.TrimSpace(eventName) != "" {
		parts = append(parts, "Acara: "+eventName)
	}
	if strings.TrimSpace(filterSummary) != "" {
		parts = append(parts, filterSummary)
	}
	if strings.TrimSpace(generatedAt) != "" {
		parts = append(parts, "Dibuat: "+generatedAt)
	}
	return strings.Join(parts, "  |  ")
}
