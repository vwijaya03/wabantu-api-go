package events

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/lvillar/gofpdf"
)

func buildPatientFilterSummary(f patientFilterInput, therapyLabel string, total int) string {
	parts := []string{fmt.Sprintf("Terapi: %s", therapyLabel)}
	if st := strings.TrimSpace(f.Status); st != "" {
		parts = append(parts, "Status: "+strings.ToUpper(st))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		parts = append(parts, "Nama mengandung: "+q)
	}
	if sd := strings.TrimSpace(f.SlotDate); sd != "" {
		parts = append(parts, "Tanggal slot: "+sd)
	}
	switch strings.ToLower(strings.TrimSpace(f.HasSlot)) {
	case "true":
		parts = append(parts, "Sudah punya jadwal")
	case "false":
		parts = append(parts, "Belum punya jadwal")
	}
	parts = append(parts, fmt.Sprintf("Total: %d pasien", total))
	return strings.Join(parts, " - ")
}

type patientPDFData struct {
	TenantName    string
	EventName     string
	DateRange     string
	Location      string
	FilterSummary string
	GeneratedAt   string
	Rows          []Patient
	HiddenColumns map[string]bool
}

const patientPDFPageWidth = 277.0 // landscape A4 minus margins

func buildPatientsPDF(data patientPDFData) ([]byte, error) {
	cols := patientExportColumns(data.HiddenColumns)
	widths := scaleColumnWidths(cols, patientPDFPageWidth)

	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetTitle(pdfSafeText("Daftar Pasien — "+data.EventName), false)
	pdf.SetAuthor("WABantu", false)
	pdf.SetMargins(10, 12, 10)
	pdf.SetAutoPageBreak(false, 14)
	pdf.AliasNbPages("")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("Arial", "", 7)
		pdf.SetTextColor(107, 114, 128)
		pdf.CellFormat(0, 4, pdfSafeText("Data rahasia - hanya untuk operasional acara"), "", 0, "L", false, 0, "")
		pdf.CellFormat(0, 4, fmt.Sprintf("Halaman %d/{nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})

	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(0, 8, pdfSafeText("Daftar Pasien"), "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(0, 6, pdfSafeText(data.EventName), "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(75, 85, 99)
	pdf.CellFormat(0, 5, pdfSafeText(data.TenantName), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 5, pdfSafeText("Periode acara: "+data.DateRange), "", 1, "L", false, 0, "")
	if data.Location != "" {
		pdf.CellFormat(0, 5, pdfSafeText("Lokasi: "+data.Location), "", 1, "L", false, 0, "")
	}
	pdf.CellFormat(0, 5, pdfSafeText("Filter: "+data.FilterSummary), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 5, pdfSafeText("Dibuat: "+data.GeneratedAt), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	drawPatientTableHeader(pdf, cols, widths)
	if len(data.Rows) == 0 {
		pdf.SetFont("Arial", "", 10)
		pdf.SetTextColor(107, 114, 128)
		pdf.CellFormat(0, 12, pdfSafeText("Tidak ada pasien untuk filter ini."), "1", 1, "C", false, 0, "")
	} else {
		for i, row := range data.Rows {
			drawPatientTableRow(pdf, cols, widths, row, i+1)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawPatientTableHeader(pdf *gofpdf.Fpdf, cols []patientExportCol, widths []float64) {
	pdf.SetFont("Arial", "B", 7)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFillColor(31, 41, 55)
	for i, c := range cols {
		pdf.CellFormat(widths[i], 7, pdfSafeText(c.Header), "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
}

func drawPatientTableRow(pdf *gofpdf.Fpdf, cols []patientExportCol, widths []float64, row Patient, no int) {
	values := make([]string, len(cols))
	for i, c := range cols {
		values[i] = pdfSafeText(c.Value(row, no))
	}
	pdf.SetFont("Arial", "", 6.5)
	rowHeight := patientRowHeight(pdf, values, widths)
	if pdf.GetY()+rowHeight > 190 {
		pdf.AddPage()
		drawPatientTableHeader(pdf, cols, widths)
	}
	x := pdf.GetX()
	y := pdf.GetY()
	if no%2 == 0 {
		pdf.SetFillColor(249, 250, 251)
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	pdf.SetDrawColor(229, 231, 235)
	pdf.SetTextColor(31, 41, 55)
	cellX := x
	for i, value := range values {
		w := widths[i]
		pdf.Rect(cellX, y, w, rowHeight, "FD")
		pdf.SetXY(cellX+1.5, y+1.5)
		align := cols[i].Align
		text := fitPatientCellText(pdf, value, w-3, 6)
		pdf.MultiCell(w-3, 3.5, text, "", align, false)
		cellX += w
	}
	pdf.SetXY(10, y+rowHeight)
}

func patientRowHeight(pdf *gofpdf.Fpdf, values []string, widths []float64) float64 {
	maxH := 7.0
	for i, value := range values {
		lines := pdf.SplitText(value, widths[i]-3)
		h := float64(len(lines)) * 3.5
		if h+2 > maxH {
			maxH = h + 2
		}
	}
	if maxH > 28 {
		maxH = 28
	}
	return maxH
}

func fitPatientCellText(pdf *gofpdf.Fpdf, value string, width float64, maxLines int) string {
	lines := pdf.SplitText(value, width)
	if len(lines) <= maxLines {
		return value
	}
	lines = lines[:maxLines]
	last := []rune(lines[maxLines-1])
	if len(last) > 3 {
		lines[maxLines-1] = string(last[:len(last)-3]) + "..."
	} else {
		lines[maxLines-1] = "..."
	}
	return strings.Join(lines, "\n")
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
