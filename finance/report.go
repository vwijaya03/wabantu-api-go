package finance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"encore.app/wabantu/shared/async"
	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"

	"github.com/lvillar/gofpdf"
)

// ============================================================
// REPORT JOB (async export)
// ============================================================

type ReportJob struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Format      string    `json:"format"`
	Status      string    `json:"status"`
	DownloadURL *string   `json:"downloadUrl,omitempty"`
	ErrorMsg    *string   `json:"errorMsg,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreateReportJobParams struct {
	Type      string `json:"type"`      // "monthly"|"custom"|"all_time"
	StartDate string `json:"startDate"` // YYYY-MM-DD
	EndDate   string `json:"endDate"`
	Period    string `json:"period,omitempty"` // YYYY-MM shortcut
	Format    string `json:"format"`           // "pdf"|"csv"
	WalletID  string `json:"walletId,omitempty"`
}

type ReportJobListResponse struct {
	Items []ReportJob `json:"items"`
}

//encore:api auth method=POST path=/api/v1/finance/reports/export
func CreateReportJob(ctx context.Context, p *CreateReportJobParams) (*ReportJob, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	p.Format = strings.ToLower(strings.TrimSpace(p.Format))
	if p.Format != "csv" {
		p.Format = "pdf"
	}
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	if p.Type == "all" {
		p.Type = "all_time"
	}
	if p.Type == "" {
		p.Type = "monthly"
	}
	if p.Type != "monthly" && p.Type != "custom" && p.Type != "all_time" {
		return nil, appErrs.BadRequest("tipe laporan tidak valid")
	}

	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	switch p.Type {
	case "monthly":
		if p.Period == "" && p.StartDate == "" {
			p.Period = financePeriod(ctx, sch, q)
		}
		if p.Period != "" {
			t, err := time.Parse("2006-01", p.Period)
			if err != nil {
				return nil, appErrs.BadRequest("periode laporan tidak valid")
			}
			p.StartDate = t.Format("2006-01-02")
			p.EndDate = t.AddDate(0, 1, -1).Format("2006-01-02")
		}
		if err := validateReportDateRange(p.StartDate, p.EndDate); err != nil {
			return nil, err
		}
	case "custom":
		if err := validateReportDateRange(p.StartDate, p.EndDate); err != nil {
			return nil, err
		}
	case "all_time":
		p.Period = ""
		p.StartDate = ""
		p.EndDate = ""
	}

	paramsBytes, err := json.Marshal(map[string]string{
		"type": p.Type, "startDate": p.StartDate, "endDate": p.EndDate, "period": p.Period, "format": p.Format,
	})
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	params := string(paramsBytes)

	var id string
	err = qrow(ctx, sch, q,
		`INSERT INTO fin_report_job (type, params, status, error_msg, created_by)
		 VALUES ($1,$2,'processing','Menyiapkan export...',$3) RETURNING id`,
		p.Type, params, u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	async.RunBounded(async.ExportSem, func() {
		processReportJobAsync(u.TenantSchema, id, p)
	})

	now := financeNow(ctx, sch, q)
	return &ReportJob{
		ID:        id,
		Type:      p.Type,
		Format:    p.Format,
		Status:    "processing",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

//encore:api auth method=GET path=/api/v1/finance/reports/jobs/:id
func GetReportJob(ctx context.Context, id string) (*ReportJob, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	var j ReportJob
	var dlURL, errMsg *string
	querySQL := `SELECT id, type, COALESCE(params->>'format', ''), status, download_url, error_msg, created_at, updated_at
		 FROM fin_report_job WHERE id=$1`
	args := []any{id}
	if !isOwner(u) {
		querySQL += ` AND created_by=$2`
		args = append(args, u.AccountID)
	}
	err = qrow(ctx, sch, q, querySQL, args...).Scan(&j.ID, &j.Type, &j.Format, &j.Status, &dlURL, &errMsg, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appErrs.NotFound("job tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}
	j.DownloadURL = dlURL
	j.ErrorMsg = errMsg
	j.Format = normalizeReportJobFormat(j.Format, dlURL)
	return &j, nil
}

//encore:api auth method=GET path=/api/v1/finance/reports/jobs
func ListReportJobs(ctx context.Context) (*ReportJobListResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	q := tenantPool()

	expireStaleReportJobs(ctx, sch, q, u.AccountID)

	rows, err := qquery(ctx, sch, q,
		`SELECT id, type, COALESCE(params->>'format', ''), status, download_url, error_msg, created_at, updated_at
		 FROM fin_report_job WHERE created_by=$1 ORDER BY created_at DESC LIMIT 20`,
		u.AccountID)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []ReportJob
	for rows.Next() {
		var j ReportJob
		var dlURL, errMsg *string
		rows.Scan(&j.ID, &j.Type, &j.Format, &j.Status, &dlURL, &errMsg, &j.CreatedAt, &j.UpdatedAt)
		j.DownloadURL = dlURL
		j.ErrorMsg = errMsg
		j.Format = normalizeReportJobFormat(j.Format, dlURL)
		items = append(items, j)
	}
	if items == nil {
		items = []ReportJob{}
	}
	return &ReportJobListResponse{Items: items}, nil
}

// processReportJob generates a self-contained CSV/PDF report and marks job done.
// For larger production exports this can be swapped to object storage without
// changing the job contract.
func processReportJobAsync(schema, jobID string, p *CreateReportJobParams) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	sch, err := prepareTenant(ctx, schema)
	if err != nil {
		return
	}
	q := tenantPool()

	processReportJob(ctx, sch, q, jobID, p)
}

func processReportJob(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, jobID string, p *CreateReportJobParams) (status string) {
	status = "failed"
	defer func() {
		if r := recover(); r != nil {
			failReportJob(ctx, sch, q, jobID, fmt.Sprintf("export report failed: %v", r))
			status = "failed"
		}
	}()

	_, _ = qexec(ctx, sch, q, `SET statement_timeout TO '15000ms'`)
	defer qexec(context.Background(), sch, q, `RESET statement_timeout`)

	updateReportJobProgress(ctx, sch, q, jobID, "Memuat transaksi...")
	report, err := loadFinanceReport(ctx, sch, q, p, func(msg string) {
		updateReportJobProgress(ctx, sch, q, jobID, msg)
	})
	if err != nil {
		failReportJob(ctx, sch, q, jobID, err.Error())
		return status
	}

	updateReportJobProgress(ctx, sch, q, jobID, fmt.Sprintf("Membuat %s untuk %d transaksi...", strings.ToUpper(p.Format), len(report.Rows)))
	dataURL, err := reportDataURL(p, report)
	if err != nil {
		failReportJob(ctx, sch, q, jobID, err.Error())
		return status
	}
	updateReportJobProgress(ctx, sch, q, jobID, "Menyimpan hasil export...")
	if _, err := qexec(ctx, sch, q,
		`UPDATE fin_report_job SET status='done', download_url=$1, error_msg=NULL, updated_at=now() WHERE id=$2`,
		dataURL, jobID); err != nil {
		failReportJob(ctx, sch, q, jobID, "gagal menyimpan hasil export: "+err.Error())
		return status
	}
	return "done"
}

type financeReportRow struct {
	Date        time.Time
	Type        string
	Flow        string
	Category    string
	Wallet      string
	Description string
	Status      string
	Amount      float64
}

type financeReportData struct {
	Title        string
	PeriodLabel  string
	GeneratedAt  string
	Rows         []financeReportRow
	TotalIncome  float64
	TotalExpense float64
	Net          float64
}

func loadFinanceReport(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, p *CreateReportJobParams, progress func(string)) (financeReportData, error) {
	report := financeReportData{
		Title:       "Laporan Keuangan",
		PeriodLabel: reportPeriodLabel(p),
		GeneratedAt: formatReportDateTime(financeNow(ctx, sch, q)),
		Rows:        []financeReportRow{},
	}

	const batchSize = 250
	for offset := 0; ; offset += batchSize {
		if progress != nil {
			progress(fmt.Sprintf("Memuat transaksi... %d baris", len(report.Rows)))
		}
		rows, err := queryFinanceReportRows(ctx, sch, q, p, batchSize, offset)
		if err != nil {
			return financeReportData{}, err
		}
		batchCount, err := appendFinanceReportRows(rows, &report)
		if err != nil {
			return financeReportData{}, err
		}
		if batchCount < batchSize {
			break
		}
	}
	report.Net = report.TotalIncome - report.TotalExpense
	return report, nil
}

func queryFinanceReportRows(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, p *CreateReportJobParams, limit, offset int) (*sql.Rows, error) {
	if p.Type == "all_time" {
		return qquery(ctx, sch, q, `
			SELECT t.transaction_date, t.type,
			       COALESCE(tt.flow,
			         CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
			              WHEN t.type IN ('expense','investment_buy') THEN 'expense'
			              WHEN t.type = 'transfer' THEN 'transfer'
			              WHEN t.type = 'adjustment' THEN 'adjustment'
			              ELSE 'expense' END
			       ) AS flow,
			       COALESCE(c.name,''), t.amount,
			       COALESCE(w.name,''), COALESCE(t.description,''), t.status
			FROM fin_transaction t
			LEFT JOIN fin_transaction_type tt ON tt.code = t.type AND tt.deleted_at IS NULL
			LEFT JOIN fin_category c ON c.id=t.category_id
			LEFT JOIN fin_wallet w ON w.id=t.wallet_id
			WHERE t.deleted_at IS NULL AND t.status='approved'
			ORDER BY t.transaction_date DESC, t.created_at DESC
			LIMIT $1 OFFSET $2`, limit, offset)
	}

	return qquery(ctx, sch, q, `
		SELECT t.transaction_date, t.type,
		       COALESCE(tt.flow,
		         CASE WHEN t.type IN ('income','dividend','interest','cashback','investment_sell') THEN 'income'
		              WHEN t.type IN ('expense','investment_buy') THEN 'expense'
		              WHEN t.type = 'transfer' THEN 'transfer'
		              WHEN t.type = 'adjustment' THEN 'adjustment'
		              ELSE 'expense' END
		       ) AS flow,
		       COALESCE(c.name,''), t.amount,
		       COALESCE(w.name,''), COALESCE(t.description,''), t.status
		FROM fin_transaction t
		LEFT JOIN fin_transaction_type tt ON tt.code = t.type AND tt.deleted_at IS NULL
		LEFT JOIN fin_category c ON c.id=t.category_id
		LEFT JOIN fin_wallet w ON w.id=t.wallet_id
		WHERE t.deleted_at IS NULL AND t.status='approved'
		  AND t.transaction_date >= $1::date AND t.transaction_date <= $2::date
		ORDER BY t.transaction_date DESC, t.created_at DESC
		LIMIT $3 OFFSET $4`, p.StartDate, p.EndDate, limit, offset)
}

func appendFinanceReportRows(rows *sql.Rows, report *financeReportData) (int, error) {
	defer rows.Close()
	if cols, err := rows.Columns(); err != nil {
		return 0, err
	} else if len(cols) != 8 {
		return 0, fmt.Errorf("kolom report tidak valid: got %d, want 8", len(cols))
	}

	count := 0
	for rows.Next() {
		var r financeReportRow
		if err := rows.Scan(&r.Date, &r.Type, &r.Flow, &r.Category, &r.Amount, &r.Wallet, &r.Description, &r.Status); err != nil {
			return count, err
		}
		switch r.Flow {
		case "income":
			report.TotalIncome += r.Amount
		case "expense":
			report.TotalExpense += r.Amount
		}
		report.Rows = append(report.Rows, r)
		count++
	}
	if err := rows.Err(); err != nil {
		return count, err
	}
	return count, nil
}

func failReportJob(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, jobID, msg string) {
	if strings.TrimSpace(msg) == "" {
		msg = "export report failed"
	}
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = qexec(updateCtx, sch, q,
		`UPDATE fin_report_job SET status='failed', error_msg=$1, updated_at=now() WHERE id=$2`,
		msg, jobID)
}

func updateReportJobProgress(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, jobID, msg string) {
	if strings.TrimSpace(msg) == "" {
		return
	}
	_, _ = qexec(ctx, sch, q,
		`UPDATE fin_report_job SET error_msg=$1, updated_at=now() WHERE id=$2 AND status='processing'`,
		msg, jobID)
}

func expireStaleReportJobs(ctx context.Context, sch appdb.SchemaSQL, q finQuerier, accountID string) {
	_, _ = qexec(ctx, sch, q,
		`UPDATE fin_report_job
		 SET status='failed',
		     error_msg='export sebelumnya tidak selesai, silakan ulangi export',
		     updated_at=now()
		 WHERE created_by=$1
		   AND status IN ('queued','processing')
		   AND updated_at < now() - interval '15 minutes'`,
		accountID)
}

func normalizeReportJobFormat(format string, downloadURL *string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "pdf" || format == "csv" {
		return format
	}
	if downloadURL != nil && strings.HasPrefix(*downloadURL, "data:application/pdf") {
		return "pdf"
	}
	return "csv"
}

func reportDataURL(p *CreateReportJobParams, report financeReportData) (string, error) {
	if p.Format == "pdf" {
		pdf, err := buildFinanceReportPDF(report)
		if err != nil {
			return "", err
		}
		return "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(pdf), nil
	}

	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM for Excel compatibility.
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(reportCSVRecords(report)); err != nil {
		return "", err
	}
	return "data:text/csv;charset=utf-8;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func buildFinanceReportPDF(report financeReportData) ([]byte, error) {
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetTitle(report.Title, false)
	pdf.SetAuthor("WABantu", false)
	pdf.SetMargins(10, 12, 10)
	pdf.SetAutoPageBreak(false, 14)
	pdf.AliasNbPages("")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("Arial", "", 8)
		pdf.SetTextColor(107, 114, 128)
		pdf.CellFormat(0, 5, fmt.Sprintf("Halaman %d/{nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})

	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.AddPage()
	drawReportHeader(pdf, tr, report)
	drawReportSummary(pdf, tr, report)
	drawReportTableHeader(pdf, tr)

	if len(report.Rows) == 0 {
		pdf.SetFont("Arial", "", 10)
		pdf.SetTextColor(107, 114, 128)
		pdf.CellFormat(0, 12, tr("Tidak ada transaksi approved pada periode ini."), "1", 1, "C", false, 0, "")
	} else {
		for i, row := range report.Rows {
			drawReportTableRow(pdf, tr, row, i)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawReportHeader(pdf *gofpdf.Fpdf, tr func(string) string, report financeReportData) {
	pdf.SetFont("Arial", "B", 17)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(0, 8, tr(report.Title), "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(75, 85, 99)
	pdf.CellFormat(0, 5, tr("Periode: "+report.PeriodLabel), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 5, tr("Dibuat: "+report.GeneratedAt), "", 1, "L", false, 0, "")
	pdf.Ln(3)
}

func drawReportSummary(pdf *gofpdf.Fpdf, tr func(string) string, report financeReportData) {
	cardY := pdf.GetY()
	cardW := 66.5
	gap := 3.5
	drawSummaryCard(pdf, tr, 10, cardY, cardW, "Pemasukan", formatReportIDR(report.TotalIncome), 236, 253, 245, 22, 101, 52)
	drawSummaryCard(pdf, tr, 10+cardW+gap, cardY, cardW, "Pengeluaran", formatReportIDR(report.TotalExpense), 254, 242, 242, 185, 28, 28)
	drawSummaryCard(pdf, tr, 10+(cardW+gap)*2, cardY, cardW, "Net", formatReportIDR(report.Net), 239, 246, 255, 29, 78, 216)
	drawSummaryCard(pdf, tr, 10+(cardW+gap)*3, cardY, cardW, "Transaksi", fmt.Sprintf("%d", len(report.Rows)), 249, 250, 251, 55, 65, 81)
	pdf.SetY(cardY + 19)
}

func drawSummaryCard(pdf *gofpdf.Fpdf, tr func(string) string, x, y, w float64, label, value string, fr, fg, fb, trr, trg, trb int) {
	pdf.SetFillColor(fr, fg, fb)
	pdf.SetDrawColor(229, 231, 235)
	pdf.RoundedRect(x, y, w, 15, 2, "1234", "FD")
	pdf.SetXY(x+4, y+3)
	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(107, 114, 128)
	pdf.CellFormat(w-8, 4, tr(label), "", 1, "L", false, 0, "")
	pdf.SetXY(x+4, y+8)
	pdf.SetFont("Arial", "B", 11)
	pdf.SetTextColor(trr, trg, trb)
	pdf.CellFormat(w-8, 5, tr(value), "", 1, "L", false, 0, "")
}

func drawReportTableHeader(pdf *gofpdf.Fpdf, tr func(string) string) {
	pdf.SetFont("Arial", "B", 8)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFillColor(31, 41, 55)
	pdf.SetDrawColor(31, 41, 55)
	for i, header := range reportHeaders() {
		pdf.CellFormat(reportColumnWidths()[i], 8, tr(header), "1", 0, reportColumnAligns()[i], true, 0, "")
	}
	pdf.Ln(-1)
}

func drawReportTableRow(pdf *gofpdf.Fpdf, tr func(string) string, row financeReportRow, idx int) {
	values := []string{
		formatReportDate(row.Date),
		reportTypeLabel(row.Type),
		emptyDash(row.Category),
		emptyDash(row.Wallet),
		truncateReportText(emptyDash(row.Description), 180),
		reportStatusLabel(row.Status),
		formatReportIDR(row.Amount),
	}
	widths := reportColumnWidths()
	aligns := reportColumnAligns()

	pdf.SetFont("Arial", "", 7.5)
	rowHeight := reportRowHeight(pdf, tr, values, widths)
	if pdf.GetY()+rowHeight > 190 {
		pdf.AddPage()
		drawReportTableHeader(pdf, tr)
	}

	x := pdf.GetX()
	y := pdf.GetY()
	fill := idx%2 == 1
	if fill {
		pdf.SetFillColor(249, 250, 251)
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	pdf.SetDrawColor(229, 231, 235)
	pdf.SetTextColor(31, 41, 55)

	for i, value := range values {
		cellX := x
		w := widths[i]
		pdf.Rect(cellX, y, w, rowHeight, "FD")
		pdf.SetXY(cellX+2, y+2)
		if i == len(values)-1 {
			switch row.Flow {
			case "income":
				pdf.SetTextColor(22, 101, 52)
			case "expense":
				pdf.SetTextColor(185, 28, 28)
			default:
				pdf.SetTextColor(31, 41, 55)
			}
		} else {
			pdf.SetTextColor(31, 41, 55)
		}
		pdf.MultiCell(w-4, 4, fitReportCellText(pdf, tr, value, w-4, 4), "", aligns[i], false)
		x += w
	}
	pdf.SetXY(10, y+rowHeight)
}

func reportCSVRecords(report financeReportData) [][]string {
	records := [][]string{{"Tanggal", "Jenis", "Kategori", "Jumlah", "Wallet", "Deskripsi", "Status"}}
	for _, r := range report.Rows {
		records = append(records, []string{
			r.Date.Format("2006-01-02"),
			r.Type,
			r.Category,
			moneyString(r.Amount),
			r.Wallet,
			r.Description,
			r.Status,
		})
	}
	return records
}

func validateReportDateRange(startDate, endDate string) error {
	if startDate == "" || endDate == "" {
		return appErrs.BadRequest("tanggal mulai dan selesai wajib diisi")
	}
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return appErrs.BadRequest("tanggal mulai tidak valid")
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return appErrs.BadRequest("tanggal selesai tidak valid")
	}
	if end.Before(start) {
		return appErrs.BadRequest("tanggal selesai tidak boleh sebelum tanggal mulai")
	}
	return nil
}

func reportPeriodLabel(p *CreateReportJobParams) string {
	if p.Type == "all_time" {
		return "Sepanjang waktu"
	}
	start, startErr := time.Parse("2006-01-02", p.StartDate)
	end, endErr := time.Parse("2006-01-02", p.EndDate)
	if startErr != nil || endErr != nil {
		return strings.TrimSpace(p.StartDate + " s/d " + p.EndDate)
	}
	return fmt.Sprintf("%s s/d %s", formatReportDate(start), formatReportDate(end))
}

func reportHeaders() []string {
	return []string{"Tanggal", "Jenis", "Kategori", "Dompet", "Deskripsi", "Status", "Jumlah"}
}

func reportColumnWidths() []float64 {
	return []float64{24, 32, 38, 34, 80, 22, 39}
}

func reportColumnAligns() []string {
	return []string{"L", "L", "L", "L", "L", "C", "R"}
}

func reportRowHeight(pdf *gofpdf.Fpdf, tr func(string) string, values []string, widths []float64) float64 {
	maxLines := 1
	for i, value := range values {
		lines := pdf.SplitText(tr(value), widths[i]-4)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	if maxLines > 4 {
		maxLines = 4
	}
	return math.Max(8, float64(maxLines)*4+4)
}

func formatReportDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	months := []string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	return fmt.Sprintf("%02d %s %d", t.Day(), months[int(t.Month())-1], t.Year())
}

func formatReportDateTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return fmt.Sprintf("%s %02d:%02d", formatReportDate(t), t.Hour(), t.Minute())
}

func fitReportCellText(pdf *gofpdf.Fpdf, tr func(string) string, value string, width float64, maxLines int) string {
	text := tr(value)
	lines := pdf.SplitText(text, width)
	if len(lines) <= maxLines {
		return text
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

func formatReportIDR(amount float64) string {
	if math.Abs(amount) < 0.005 {
		amount = 0
	}
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = math.Abs(amount)
	}
	parts := strings.Split(fmt.Sprintf("%.2f", amount), ".")
	intPart := groupThousands(parts[0])
	if len(parts) == 2 && parts[1] != "00" {
		return "Rp " + sign + intPart + "," + parts[1]
	}
	return "Rp " + sign + intPart
}

func groupThousands(s string) string {
	if len(s) <= 3 {
		return s
	}
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	out = append([]string{s}, out...)
	return strings.Join(out, ".")
}

func reportTypeLabel(code string) string {
	labels := map[string]string{
		"income":          "Pemasukan",
		"expense":         "Pengeluaran",
		"transfer":        "Transfer",
		"adjustment":      "Penyesuaian",
		"investment_buy":  "Beli Investasi",
		"investment_sell": "Jual Investasi",
		"dividend":        "Dividen",
		"interest":        "Bunga",
		"cashback":        "Cashback",
	}
	if label, ok := labels[code]; ok {
		return label
	}
	return emptyDash(code)
}

func reportStatusLabel(code string) string {
	labels := map[string]string{
		"approved":         "Approved",
		"pending_approval": "Pending",
		"rejected":         "Ditolak",
	}
	if label, ok := labels[code]; ok {
		return label
	}
	return emptyDash(code)
}

func emptyDash(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "))
	if s == "" {
		return "-"
	}
	return s
}

func truncateReportText(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-3]) + "..."
}
