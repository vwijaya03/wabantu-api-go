package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/system"
)

// Export job kinds
const (
	exportKindPatientsPDF = "patients_pdf"
	exportKindStaffSheet  = "staff_sheet"
)

type ExportJob struct {
	ID          string    `json:"id"`
	EventID     string    `json:"eventId"`
	Kind        string    `json:"kind"`
	Format      string    `json:"format,omitempty"`
	Status      string    `json:"status"`
	DownloadURL *string   `json:"downloadUrl,omitempty"`
	FileName    *string   `json:"fileName,omitempty"`
	RowCount    *int      `json:"rowCount,omitempty"`
	ErrorMsg    *string   `json:"errorMsg,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PatientExportFilters — filter pasien untuk kind=patients_pdf (Encore API schema).
type PatientExportFilters struct {
	Q         string `json:"q,omitempty"`
	TherapyID string `json:"therapyId,omitempty"`
	Status    string `json:"status,omitempty"`
	SlotDate  string `json:"slotDate,omitempty"`
	HasSlot   string `json:"hasSlot,omitempty"`
}

type CreateExportJobParams struct {
	Kind    string                `json:"kind"`
	Format  string                `json:"format,omitempty"`
	Filters *PatientExportFilters `json:"filters,omitempty"`
}

type ExportJobListResponse struct {
	Items []ExportJob `json:"items"`
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/export-jobs
func CreateExportJob(ctx context.Context, eventId string, p *CreateExportJobParams) (*ExportJob, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, appErrs.BadRequest("parameter export wajib diisi")
	}
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	if kind != exportKindPatientsPDF && kind != exportKindStaffSheet {
		return nil, appErrs.BadRequest("jenis export tidak valid (patients_pdf atau staff_sheet)")
	}

	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	if err := assertEventExists(ctx, conn, eventId); err != nil {
		return nil, err
	}

	format := strings.ToLower(strings.TrimSpace(p.Format))
	if kind == exportKindStaffSheet {
		format = "xlsx"
	} else if format != "pdf" {
		format = "pdf"
	}

	filters := patientFilterInput{}
	if kind == exportKindPatientsPDF {
		filters = patientFilterFromExport(p.Filters)
		if err := validatePatientFilters(filters); err != nil {
			return nil, err
		}
		_, total, err := queryPatients(ctx, conn, eventId, filters, 1, 0)
		if err != nil {
			return nil, err
		}
		if total > maxPatientExportRows {
			return nil, appErrs.BadRequest(fmt.Sprintf(
				"terlalu banyak pasien (%d). Persempit filter (maks. %d baris per export).",
				total, maxPatientExportRows,
			))
		}
	}

	paramsBytes, err := json.Marshal(map[string]any{
		"kind":    kind,
		"format":  format,
		"filters": patientFiltersToMap(filters),
	})
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	var id string
	err = conn.QueryRowContext(ctx, `
		INSERT INTO evt_export_job (event_id, kind, params, status, error_msg, created_by)
		VALUES ($1::uuid,$2,$3,'processing','Menyiapkan export...',$4)
		RETURNING id::text`,
		eventId, kind, string(paramsBytes), u.AccountID,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	go processExportJobAsync(u.TenantSchema, id, eventId, kind, format, filters, u.AccountID, u.TenantID, u.ImpersonationTenantName)

	now := time.Now()
	auditEvent(ctx, conn, u, "event", eventId, "export_job_created", nil, map[string]any{"kind": kind, "jobId": id})

	return &ExportJob{
		ID:        id,
		EventID:   eventId,
		Kind:      kind,
		Format:    format,
		Status:    "processing",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/export-jobs/:jobId
func GetExportJob(ctx context.Context, eventId, jobId string) (*ExportJob, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	return scanExportJob(ctx, conn, eventId, jobId, u.AccountID, isOwner(u))
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/export-jobs
func ListExportJobs(ctx context.Context, eventId string) (*ExportJobListResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := tenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	if err := assertEventExists(ctx, conn, eventId); err != nil {
		return nil, err
	}

	expireStaleExportJobs(ctx, conn, u.AccountID, eventId)

	owner := isOwner(u)
	q := `SELECT id::text, event_id::text, kind,
		COALESCE(params->>'format',''), status, download_url, file_name, row_count, error_msg,
		created_at, updated_at
		FROM evt_export_job WHERE event_id=$1::uuid`
	args := []any{eventId}
	if !owner {
		q += ` AND created_by=$2`
		args = append(args, u.AccountID)
	}
	q += ` ORDER BY created_at DESC LIMIT 20`

	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()

	var items []ExportJob
	for rows.Next() {
		j, err := scanExportJobRow(rows)
		if err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		items = append(items, j)
	}
	if items == nil {
		items = []ExportJob{}
	}
	return &ExportJobListResponse{Items: items}, nil
}

func processExportJobAsync(schema, jobID, eventID, kind, format string, filters patientFilterInput, accountID, tenantID, tenantName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	conn, err := tenantConn(ctx, schema)
	if err != nil {
		return
	}
	defer conn.Close()

	processExportJob(ctx, conn, jobID, eventID, kind, format, filters, accountID, tenantID, tenantName)
}

func processExportJob(ctx context.Context, conn *sql.Conn, jobID, eventID, kind, format string, filters patientFilterInput, accountID, tenantID, tenantName string) {
	defer func() {
		if r := recover(); r != nil {
			failExportJob(ctx, conn, jobID, fmt.Sprintf("export gagal: %v", r))
		}
	}()

	switch kind {
	case exportKindPatientsPDF:
		processPatientsExportJob(ctx, conn, jobID, eventID, filters, tenantID, tenantName)
	case exportKindStaffSheet:
		processStaffSheetExportJob(ctx, conn, jobID, eventID)
	default:
		failExportJob(ctx, conn, jobID, "jenis export tidak dikenali")
	}
	_ = format
}

func processPatientsExportJob(ctx context.Context, conn *sql.Conn, jobID, eventID string, filters patientFilterInput, tenantID, tenantName string) {
	updateExportJobProgress(ctx, conn, jobID, "Memuat data pasien...")

	var eventName, startDate, endDate, location string
	var loc sql.NullString
	if err := conn.QueryRowContext(ctx, `
		SELECT event_name, start_date::text, end_date::text, location
		FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventID,
	).Scan(&eventName, &startDate, &endDate, &loc); err != nil {
		failExportJob(ctx, conn, jobID, "acara tidak ditemukan")
		return
	}
	if loc.Valid {
		location = loc.String
	}

	items, _, err := queryPatients(ctx, conn, eventID, filters, maxPatientExportRows, 0)
	if err != nil {
		failExportJob(ctx, conn, jobID, exportErrMessage(err, "gagal memuat pasien"))
		return
	}

	therapyLabel := "Semua terapi"
	if tid := strings.TrimSpace(filters.TherapyID); tid != "" {
		for _, it := range items {
			if it.TherapyID == tid {
				therapyLabel = it.TherapyName
				break
			}
		}
		if therapyLabel == "Semua terapi" {
			_ = conn.QueryRowContext(ctx, `SELECT therapy_name FROM evt_therapy WHERE id=$1::uuid`, tid).Scan(&therapyLabel)
		}
	}
	_, total, _ := queryPatients(ctx, conn, eventID, filters, 1, 0)
	filterSummary := buildPatientFilterSummary(filters, therapyLabel, total)

	tenantName = strings.TrimSpace(tenantName)
	if tenantName == "" && tenantID != "" {
		_ = system.DB.QueryRow(ctx, `
			SELECT name FROM tenant WHERE id=$1::uuid AND deleted_at IS NULL`, tenantID,
		).Scan(&tenantName)
	}
	if tenantName == "" {
		tenantName = "WABantu"
	}

	updateExportJobProgress(ctx, conn, jobID, fmt.Sprintf("Membuat PDF (%d pasien)...", len(items)))
	pdfBytes, err := buildPatientsPDF(patientPDFData{
		TenantName:    tenantName,
		EventName:     eventName,
		DateRange:     startDate + " — " + endDate,
		Location:      location,
		FilterSummary: filterSummary,
		GeneratedAt:   time.Now().Format("02/01/2006 15:04"),
		Rows:          items,
	})
	if err != nil {
		failExportJob(ctx, conn, jobID, "gagal membuat PDF")
		return
	}

	dataURL := pdfDataURL(pdfBytes)
	slug := slugify(eventName)
	fileName := fmt.Sprintf("pasien-%s-%s.pdf", slug, time.Now().Format("20060102"))
	completeExportJob(ctx, conn, jobID, dataURL, fileName, len(items))
}

func processStaffSheetExportJob(ctx context.Context, conn *sql.Conn, jobID, eventID string) {
	updateExportJobProgress(ctx, conn, jobID, "Memuat staf & penugasan...")
	data, err := loadStaffSheetExportData(ctx, conn, eventID)
	if err != nil {
		failExportJob(ctx, conn, jobID, exportErrMessage(err, "gagal memuat data staf"))
		return
	}

	updateExportJobProgress(ctx, conn, jobID, "Membuat lembar Excel...")
	xlsxBytes, err := buildStaffSheetXLSX(data)
	if err != nil {
		failExportJob(ctx, conn, jobID, "gagal membuat Excel: "+err.Error())
		return
	}

	dataURL := xlsxDataURL(xlsxBytes)
	slug := slugify(data.EventName)
	fileName := fmt.Sprintf("terapis-%s-%s.xlsx", slug, time.Now().Format("20060102"))
	completeExportJob(ctx, conn, jobID, dataURL, fileName, len(data.TherapyStaff))
}

func completeExportJob(ctx context.Context, conn *sql.Conn, jobID, dataURL, fileName string, rowCount int) {
	updateExportJobProgress(ctx, conn, jobID, "Menyimpan hasil export...")
	_, err := conn.ExecContext(ctx, `
		UPDATE evt_export_job
		SET status='done', download_url=$1, file_name=$2, row_count=$3, error_msg=NULL, updated_at=now()
		WHERE id=$4::uuid`,
		dataURL, fileName, rowCount, jobID)
	if err != nil {
		failExportJob(ctx, conn, jobID, "gagal menyimpan hasil export")
	}
}

func failExportJob(ctx context.Context, conn *sql.Conn, jobID, msg string) {
	if strings.TrimSpace(msg) == "" {
		msg = "export gagal"
	}
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = conn.ExecContext(updateCtx,
		`UPDATE evt_export_job SET status='failed', error_msg=$1, updated_at=now() WHERE id=$2::uuid`,
		msg, jobID)
}

func updateExportJobProgress(ctx context.Context, conn *sql.Conn, jobID, msg string) {
	if strings.TrimSpace(msg) == "" {
		return
	}
	_, _ = conn.ExecContext(ctx,
		`UPDATE evt_export_job SET error_msg=$1, updated_at=now() WHERE id=$2::uuid AND status='processing'`,
		msg, jobID)
}

func expireStaleExportJobs(ctx context.Context, conn *sql.Conn, accountID, eventID string) {
	_, _ = conn.ExecContext(ctx, `
		UPDATE evt_export_job
		SET status='failed',
		    error_msg='export sebelumnya tidak selesai, silakan ulangi export',
		    updated_at=now()
		WHERE created_by=$1 AND event_id=$2::uuid
		  AND status IN ('queued','processing')
		  AND updated_at < now() - interval '15 minutes'`,
		accountID, eventID)
}

func scanExportJob(ctx context.Context, conn *sql.Conn, eventID, jobID, accountID string, owner bool) (*ExportJob, error) {
	q := `SELECT id::text, event_id::text, kind,
		COALESCE(params->>'format',''), status, download_url, file_name, row_count, error_msg,
		created_at, updated_at
		FROM evt_export_job WHERE id=$1::uuid AND event_id=$2::uuid`
	args := []any{jobID, eventID}
	if !owner {
		q += ` AND created_by=$3`
		args = append(args, accountID)
	}
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, appErrs.NotFound("job export tidak ditemukan")
	}
	j, err := scanExportJobRow(rows)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	return &j, nil
}

func scanExportJobRow(rows *sql.Rows) (ExportJob, error) {
	var j ExportJob
	var dlURL, fileName, errMsg sql.NullString
	var rowCount sql.NullInt64
	if err := rows.Scan(&j.ID, &j.EventID, &j.Kind, &j.Format, &j.Status, &dlURL, &fileName, &rowCount, &errMsg, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return ExportJob{}, err
	}
	if dlURL.Valid {
		j.DownloadURL = &dlURL.String
	}
	if fileName.Valid {
		j.FileName = &fileName.String
	}
	if rowCount.Valid {
		n := int(rowCount.Int64)
		j.RowCount = &n
	}
	if errMsg.Valid && errMsg.String != "" {
		s := errMsg.String
		j.ErrorMsg = &s
	}
	j.Format = normalizeExportJobFormat(j.Kind, j.Format, j.DownloadURL)
	return j, nil
}

func normalizeExportJobFormat(kind, format string, downloadURL *string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "pdf" || format == "xlsx" {
		return format
	}
	if downloadURL != nil {
		u := strings.ToLower(*downloadURL)
		if strings.Contains(u, "application/pdf") {
			return "pdf"
		}
		if strings.Contains(u, "spreadsheetml") {
			return "xlsx"
		}
	}
	if kind == exportKindStaffSheet {
		return "xlsx"
	}
	return "pdf"
}

func patientFilterFromExport(f *PatientExportFilters) patientFilterInput {
	if f == nil {
		return patientFilterInput{}
	}
	return patientFilterInput{
		Q: f.Q, TherapyID: f.TherapyID, Status: f.Status, SlotDate: f.SlotDate, HasSlot: f.HasSlot,
	}
}

func patientFiltersToMap(f patientFilterInput) map[string]string {
	return map[string]string{
		"q": f.Q, "therapyId": f.TherapyID, "status": f.Status,
		"slotDate": f.SlotDate, "hasSlot": f.HasSlot,
	}
}

func exportErrMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	var appErr interface{ Error() string }
	if errors.As(err, &appErr) {
		return err.Error()
	}
	return fallback
}
