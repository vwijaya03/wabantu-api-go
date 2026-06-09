package events

import (
	"bytes"

	"github.com/xuri/excelize/v2"
)

func buildPatientsXLSX(data patientPDFData) ([]byte, error) {
	cols := patientExportColumns(data.HiddenColumns)
	if len(cols) == 0 {
		cols = patientExportColumns(nil)
	}

	f := excelize.NewFile()
	sheet := "Pasien"
	_ = f.SetSheetName("Sheet1", sheet)

	styles, err := newExportXLSXStyles(f, patientExportTheme)
	if err != nil {
		return nil, err
	}

	headerRow := 4
	_ = writeExportTitleBlock(f, sheet, "Daftar Pasien", exportSubtitleLines(
		data.EventName, data.FilterSummary, data.GeneratedAt,
	), len(cols), styles)

	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.Header
	}
	_ = writeExportTableHeader(f, sheet, headerRow, headers, styles.header)
	applyColumnWidths(f, sheet, cols)

	for i, row := range data.Rows {
		r := headerRow + 1 + i
		vals := make([]any, len(cols))
		for c, col := range cols {
			vals[c] = col.Value(row, i+1)
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
