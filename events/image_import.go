package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/aivision"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
)

const (
	eventImageStagingTTL       = 24 * time.Hour
	eventImageMaxBytes         = 5 << 20
	eventImageMaxBatchBytes    = 20 << 20
	eventImageMaxFilesPerBatch = 5
	eventImageMinBytes         = 1024
	eventImageMaxStaffItems    = 80
	eventImageMaxPatientItems  = 80
	eventImageMaxTherapyItems  = 30
)

type EventImageLimits struct {
	MaxBytes          int64    `json:"maxBytes"`
	MaxMegabytes      int      `json:"maxMegabytes"`
	MaxBatchBytes     int64    `json:"maxBatchBytes"`
	MaxBatchMegabytes int      `json:"maxBatchMegabytes"`
	MaxFilesPerBatch  int      `json:"maxFilesPerBatch"`
	MinBytes          int64    `json:"minBytes"`
	AllowedMIME       []string `json:"allowedMime"`
	AllowedExt        []string `json:"allowedExt"`
}

type StaffImageDraftItem struct {
	FullName          string   `json:"fullName"`
	Role              string   `json:"role"`
	TherapyNames      []string `json:"therapyNames,omitempty"`
	VolunteerRoleName string   `json:"volunteerRoleName,omitempty"`
	IsPencatat        bool     `json:"isPencatat"`
	AttendanceLabel   string   `json:"attendanceLabel,omitempty"`
	Include           bool     `json:"include"`
}

type PatientImageDraftItem struct {
	FullName      string `json:"fullName"`
	BirthDate     string `json:"birthDate"`
	TherapyName   string `json:"therapyName"`
	Complaint     string `json:"complaint,omitempty"`
	PreferredTime string `json:"preferredTime,omitempty"`
	Include       bool   `json:"include"`
}

type TherapyImageDraftItem struct {
	TherapyName string `json:"therapyName"`
	Description string `json:"description,omitempty"`
	Include     bool   `json:"include"`
}

type ImagePreviewMeta struct {
	JobID               string   `json:"jobId"`
	ImagesProcessed     int      `json:"imagesProcessed"`
	InputTokens         int      `json:"inputTokens"`
	OutputTokens        int      `json:"outputTokens"`
	TokensUsed          int      `json:"tokensUsed"`
	TokenQuotaRemaining int      `json:"tokenQuotaRemaining"`
	TokenQuotaLimit     int      `json:"tokenQuotaLimit"`
	QuotaNotice         string   `json:"quotaNotice,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

type StaffImagePreviewResponse struct {
	ImagePreviewMeta
	Items []StaffImageDraftItem `json:"items"`
}

type PatientImagePreviewResponse struct {
	ImagePreviewMeta
	Items []PatientImageDraftItem `json:"items"`
}

type TherapyImagePreviewResponse struct {
	ImagePreviewMeta
	Items []TherapyImageDraftItem `json:"items"`
}

type CommitStaffImageRequest struct {
	Items []StaffImageDraftItem `json:"items"`
}

type CommitPatientImageRequest struct {
	Items []PatientImageDraftItem `json:"items"`
}

type CommitTherapyImageRequest struct {
	Items []TherapyImageDraftItem `json:"items"`
}

type CommitImageResponse struct {
	JobID        string `json:"jobId"`
	SavedCount   int    `json:"savedCount"`
	SkippedCount int    `json:"skippedCount"`
	Message      string `json:"message"`
}

//encore:api auth method=GET path=/api/v1/events/import-image-limits tag:owner
func GetEventImageLimits(ctx context.Context) (*EventImageLimits, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	return eventImageLimits(), nil
}

func eventImageLimits() *EventImageLimits {
	return &EventImageLimits{
		MaxBytes: eventImageMaxBytes, MaxMegabytes: 5,
		MaxBatchBytes: eventImageMaxBatchBytes, MaxBatchMegabytes: 20,
		MaxFilesPerBatch: eventImageMaxFilesPerBatch, MinBytes: eventImageMinBytes,
		AllowedMIME: []string{"image/jpeg", "image/png", "image/webp"},
		AllowedExt:  []string{".jpg", ".jpeg", ".png", ".webp"},
	}
}

func ensureEventImageAIQuota(ctx context.Context, schema string) (remaining, limit int, err error) {
	allowed, rem, lim := usage.CheckQuota(ctx, schema, "ai_token")
	if !allowed {
		return rem, lim, appErrs.Forbidden("kuota token AI bulan ini sudah habis")
	}
	return rem, lim, nil
}

func readEventImageBytes(fh *multipart.FileHeader) ([]byte, string, error) {
	if fh == nil {
		return nil, "", appErrs.BadRequest("file wajib")
	}
	if fh.Size > eventImageMaxBytes {
		return nil, "", appErrs.BadRequest("maksimal 5 MB per gambar")
	}
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return nil, "", appErrs.BadRequest("format: JPG, PNG, WEBP")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, "", appErrs.BadRequest("gagal membaca file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, eventImageMaxBytes+1))
	if err != nil {
		return nil, "", appErrs.Internal("gagal membaca gambar")
	}
	if len(b) < eventImageMinBytes {
		return nil, "", appErrs.BadRequest("gambar terlalu kecil")
	}
	mt := "image/jpeg"
	switch ext {
	case ".png":
		mt = "image/png"
	case ".webp":
		mt = "image/webp"
	}
	return b, mt, nil
}

func stagingKey(kind, jobID string) string {
	return "events:image:" + kind + ":" + jobID
}

type staffStaging struct {
	TenantSchema string                `json:"tenantSchema"`
	EventID      string                `json:"eventId"`
	Items        []StaffImageDraftItem `json:"items"`
}

type patientStaging struct {
	TenantSchema string                  `json:"tenantSchema"`
	EventID      string                  `json:"eventId"`
	Items        []PatientImageDraftItem `json:"items"`
}

type therapyStaging struct {
	TenantSchema string                  `json:"tenantSchema"`
	Items        []TherapyImageDraftItem `json:"items"`
}

func saveStaging(ctx context.Context, kind string, payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	jobID := uuid.NewString()
	if err := appauth.RedisClient().Set(ctx, stagingKey(kind, jobID), b, eventImageStagingTTL).Err(); err != nil {
		return "", appErrs.Internal("gagal menyimpan draft")
	}
	return jobID, nil
}

func loadStaffStaging(ctx context.Context, jobID, tenantSchema string) (*staffStaging, error) {
	b, err := appauth.RedisClient().Get(ctx, stagingKey("staff", jobID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, appErrs.NotFound("draft tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}
	var st staffStaging
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, appErrs.Internal("draft rusak")
	}
	if st.TenantSchema != tenantSchema {
		return nil, appErrs.Forbidden("draft tidak valid")
	}
	return &st, nil
}

func loadPatientStaging(ctx context.Context, jobID, tenantSchema string) (*patientStaging, error) {
	b, err := appauth.RedisClient().Get(ctx, stagingKey("patients", jobID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, appErrs.NotFound("draft tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}
	var st patientStaging
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, appErrs.Internal("draft rusak")
	}
	if st.TenantSchema != tenantSchema {
		return nil, appErrs.Forbidden("draft tidak valid")
	}
	return &st, nil
}

func loadTherapyStaging(ctx context.Context, jobID, tenantSchema string) (*therapyStaging, error) {
	b, err := appauth.RedisClient().Get(ctx, stagingKey("therapies", jobID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, appErrs.NotFound("draft tidak ditemukan")
		}
		return nil, appErrs.Internal(err.Error())
	}
	var st therapyStaging
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, appErrs.Internal("draft rusak")
	}
	if st.TenantSchema != tenantSchema {
		return nil, appErrs.Forbidden("draft tidak valid")
	}
	return &st, nil
}

func validateImageBatch(files []*multipart.FileHeader) error {
	if len(files) == 0 {
		return appErrs.BadRequest("pilih minimal satu gambar")
	}
	if len(files) > eventImageMaxFilesPerBatch {
		return appErrs.BadRequest(fmt.Sprintf("maksimal %d gambar", eventImageMaxFilesPerBatch))
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	if total > eventImageMaxBatchBytes {
		return appErrs.BadRequest("total ukuran gambar terlalu besar")
	}
	return nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/people/import-image/draft/:jobId tag:owner
func GetStaffImageDraft(ctx context.Context, eventId, jobId string) (*StaffImagePreviewResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	st, err := loadStaffStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	if st.EventID != eventId {
		return nil, appErrs.NotFound("draft tidak ditemukan")
	}
	return &StaffImagePreviewResponse{Items: st.Items, ImagePreviewMeta: ImagePreviewMeta{JobID: jobId}}, nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/people/import-image/draft/:jobId/commit tag:owner
func CommitStaffImageImport(ctx context.Context, eventId, jobId string, p *CommitStaffImageRequest) (*CommitImageResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return nil, err
	}
	st, err := loadStaffStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	items := p.Items
	if len(items) == 0 {
		items = st.Items
	}
	saved, skipped := 0, 0
	for _, it := range items {
		if !it.Include || strings.TrimSpace(it.FullName) == "" {
			skipped++
			continue
		}
		params := &UpsertPersonParams{
			FullName: it.FullName, Role: it.Role, IsPencatat: it.IsPencatat,
		}
		att, notes := mapStaffAttendanceLabel(it.AttendanceLabel)
		params.AttendanceStatus = att
		params.Notes = notes
		if roleUsesTherapies(it.Role) {
			ids, err := resolveTherapyIDsByNames(ctx, ts, it.TherapyNames)
			if err != nil {
				skipped++
				continue
			}
			params.TherapyIDs = ids
		}
		if strings.EqualFold(it.Role, "relawan") || strings.EqualFold(it.Role, "volunteer") {
			rid, err := resolveVolunteerRoleIDByName(ctx, ts, it.VolunteerRoleName)
			if err != nil {
				skipped++
				continue
			}
			params.VolunteerRoleID = &rid
		}
		if _, err := CreateEventPerson(ctx, eventId, params); err != nil {
			skipped++
			continue
		}
		saved++
	}
	_ = appauth.RedisClient().Del(ctx, stagingKey("staff", jobId)).Err()
	return &CommitImageResponse{
		JobID: jobId, SavedCount: saved, SkippedCount: skipped,
		Message: fmt.Sprintf("%d staf disimpan", saved),
	}, nil
}

func ensureAnthropicForImageImport() error {
	if strings.TrimSpace(secrets.AnthropicAPIKey) == "" {
		return appErrs.BadRequest(
			"kunci Anthropic belum dikonfigurasi — jalankan: encore secret set --type local AnthropicAPIKey lalu restart encore run",
		)
	}
	return nil
}

func imageVisionFailureWarning(filename string, err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return filename + ": gagal dibaca AI"
	}
	if strings.Contains(strings.ToLower(msg), "anthropic") || strings.Contains(msg, "kunci") {
		return msg
	}
	return filename + ": gagal dibaca AI — " + msg
}

func previewStaffImages(ctx context.Context, u *types.AuthUser, eventID string, files []*multipart.FileHeader) (*StaffImagePreviewResponse, error) {
	if err := ensureAnthropicForImageImport(); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventExists(ctx, u, ts, eventID); err != nil {
		return nil, err
	}
	if err := validateImageBatch(files); err != nil {
		return nil, err
	}
	rem, lim, err := ensureEventImageAIQuota(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	var items []StaffImageDraftItem
	var warnings []string
	inTok, outTok, processed := 0, 0, 0
	for _, fh := range files {
		b, mt, err := readEventImageBytes(fh)
		if err != nil {
			return nil, err
		}
		raw, usg, err := aivision.ExtractEventStaffFromScreenshot(ctx, secrets.AnthropicAPIKey, b, mt)
		if err != nil {
			warnings = append(warnings, imageVisionFailureWarning(fh.Filename, err))
			continue
		}
		inTok += usg.InputTokens
		outTok += usg.OutputTokens
		processed++
		_ = usage.RecordEvent(ctx, u.TenantSchema, "ai_token", usg.InputTokens+usg.OutputTokens, nil)
		parsed, err := parseStaffVisionResponse(raw)
		if err != nil {
			warnings = append(warnings, fh.Filename+": format AI tidak valid")
			continue
		}
		for _, it := range parsed {
			if len(items) >= eventImageMaxStaffItems {
				break
			}
			it.FullName = strings.TrimSpace(it.FullName)
			if it.FullName == "" {
				continue
			}
			it.Include = true
			if strings.TrimSpace(it.Role) == "" {
				if len(it.TherapyNames) > 0 {
					it.Role = "terapis"
				} else {
					it.Role = "relawan"
				}
			}
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil, appErrs.BadRequest(staffVisionEmptyMessage(warnings))
	}
	jobID, err := saveStaging(ctx, "staff", staffStaging{TenantSchema: u.TenantSchema, EventID: eventID, Items: items})
	if err != nil {
		return nil, err
	}
	return &StaffImagePreviewResponse{
		Items: items,
		ImagePreviewMeta: ImagePreviewMeta{
			JobID: jobID, ImagesProcessed: processed,
			InputTokens: inTok, OutputTokens: outTok, TokensUsed: inTok + outTok,
			TokenQuotaRemaining: rem, TokenQuotaLimit: lim, Warnings: warnings,
		},
	}, nil
}

//encore:api auth method=GET path=/api/v1/events/detail/:eventId/patients/import-image/draft/:jobId tag:owner
func GetPatientImageDraft(ctx context.Context, eventId, jobId string) (*PatientImagePreviewResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	st, err := loadPatientStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	if st.EventID != eventId {
		return nil, appErrs.NotFound("draft tidak ditemukan")
	}
	return &PatientImagePreviewResponse{Items: st.Items, ImagePreviewMeta: ImagePreviewMeta{JobID: jobId}}, nil
}

//encore:api auth method=POST path=/api/v1/events/detail/:eventId/patients/import-image/draft/:jobId/commit tag:owner
func CommitPatientImageImport(ctx context.Context, eventId, jobId string, p *CommitPatientImageRequest) (*CommitImageResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventMutable(ctx, ts, eventId); err != nil {
		return nil, err
	}
	st, err := loadPatientStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	items := p.Items
	if len(items) == 0 {
		items = st.Items
	}
	saved, skipped := 0, 0
	for _, it := range items {
		if !it.Include || strings.TrimSpace(it.FullName) == "" {
			skipped++
			continue
		}
		therapyID, err := resolveTherapyIDByName(ctx, ts, it.TherapyName)
		if err != nil {
			skipped++
			continue
		}
		preferred := normalizePreferredTime(it.PreferredTime)
		patientID, err := createPatientForEvent(ctx, u.TenantSchema, eventId, &CreatePatientParams{
			FullName: it.FullName, BirthDate: it.BirthDate, TherapyID: therapyID,
			Complaint: it.Complaint, PreferredTime: preferred,
		}, false, false)
		if err != nil {
			skipped++
			continue
		}
		if preferred != "" {
			_ = assignPatientSlotBestEffort(ctx, u.TenantSchema, eventId, patientID, therapyID, preferred)
		}
		saved++
	}
	_ = appauth.RedisClient().Del(ctx, stagingKey("patients", jobId)).Err()
	return &CommitImageResponse{
		JobID: jobId, SavedCount: saved, SkippedCount: skipped,
		Message: fmt.Sprintf("%d pasien disimpan", saved),
	}, nil
}

func previewPatientImages(ctx context.Context, u *types.AuthUser, eventID string, files []*multipart.FileHeader) (*PatientImagePreviewResponse, error) {
	if err := ensureAnthropicForImageImport(); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if err := assertEventExists(ctx, u, ts, eventID); err != nil {
		return nil, err
	}
	if err := validateImageBatch(files); err != nil {
		return nil, err
	}
	rem, lim, err := ensureEventImageAIQuota(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	var items []PatientImageDraftItem
	var warnings []string
	inTok, outTok, processed := 0, 0, 0
	for _, fh := range files {
		b, mt, err := readEventImageBytes(fh)
		if err != nil {
			return nil, err
		}
		raw, usg, err := aivision.ExtractEventPatientsFromScreenshot(ctx, secrets.AnthropicAPIKey, b, mt)
		if err != nil {
			warnings = append(warnings, imageVisionFailureWarning(fh.Filename, err))
			continue
		}
		inTok += usg.InputTokens
		outTok += usg.OutputTokens
		processed++
		_ = usage.RecordEvent(ctx, u.TenantSchema, "ai_token", usg.InputTokens+usg.OutputTokens, nil)
		var parsed struct {
			Items []PatientImageDraftItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(aivision.SanitizeVisionJSON(raw)), &parsed); err != nil {
			warnings = append(warnings, fh.Filename+": format AI tidak valid")
			continue
		}
		for _, it := range parsed.Items {
			if len(items) >= eventImageMaxPatientItems {
				break
			}
			it.FullName = strings.TrimSpace(it.FullName)
			if it.FullName == "" {
				continue
			}
			it.Include = true
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil, appErrs.BadRequest("tidak ada pasien terdeteksi")
	}
	jobID, err := saveStaging(ctx, "patients", patientStaging{TenantSchema: u.TenantSchema, EventID: eventID, Items: items})
	if err != nil {
		return nil, err
	}
	return &PatientImagePreviewResponse{
		Items: items,
		ImagePreviewMeta: ImagePreviewMeta{
			JobID: jobID, ImagesProcessed: processed,
			InputTokens: inTok, OutputTokens: outTok, TokensUsed: inTok + outTok,
			TokenQuotaRemaining: rem, TokenQuotaLimit: lim, Warnings: warnings,
		},
	}, nil
}

//encore:api auth method=GET path=/api/v1/events/masters/therapies/import-image/draft/:jobId tag:owner
func GetTherapyImageDraft(ctx context.Context, jobId string) (*TherapyImagePreviewResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	st, err := loadTherapyStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	return &TherapyImagePreviewResponse{Items: st.Items, ImagePreviewMeta: ImagePreviewMeta{JobID: jobId}}, nil
}

//encore:api auth method=POST path=/api/v1/events/masters/therapies/import-image/draft/:jobId/commit tag:owner
func CommitTherapyImageImport(ctx context.Context, jobId string, p *CommitTherapyImageRequest) (*CommitImageResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	st, err := loadTherapyStaging(ctx, jobId, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	items := p.Items
	if len(items) == 0 {
		items = st.Items
	}
	saved, skipped := 0, 0
	for _, it := range items {
		if !it.Include || strings.TrimSpace(it.TherapyName) == "" {
			skipped++
			continue
		}
		var exists bool
		_ = ts.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM evt_therapy WHERE deleted_at IS NULL AND therapy_name ILIKE $1)`,
			strings.TrimSpace(it.TherapyName)).Scan(&exists)
		if exists {
			skipped++
			continue
		}
		_, err := ts.ExecContext(ctx, `
			INSERT INTO evt_therapy (therapy_name, description, display_order)
			VALUES ($1,$2, (SELECT COALESCE(MAX(display_order),0)+1 FROM evt_therapy))`,
			it.TherapyName, nullStr(it.Description))
		if err != nil {
			skipped++
			continue
		}
		saved++
	}
	_ = appauth.RedisClient().Del(ctx, stagingKey("therapies", jobId)).Err()
	return &CommitImageResponse{
		JobID: jobId, SavedCount: saved, SkippedCount: skipped,
		Message: fmt.Sprintf("%d terapi disimpan", saved),
	}, nil
}

func previewTherapyImages(ctx context.Context, u *types.AuthUser, files []*multipart.FileHeader) (*TherapyImagePreviewResponse, error) {
	if err := ensureAnthropicForImageImport(); err != nil {
		return nil, err
	}
	if err := validateImageBatch(files); err != nil {
		return nil, err
	}
	rem, lim, err := ensureEventImageAIQuota(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	var items []TherapyImageDraftItem
	var warnings []string
	inTok, outTok, processed := 0, 0, 0
	for _, fh := range files {
		b, mt, err := readEventImageBytes(fh)
		if err != nil {
			return nil, err
		}
		raw, usg, err := aivision.ExtractEventTherapiesFromScreenshot(ctx, secrets.AnthropicAPIKey, b, mt)
		if err != nil {
			warnings = append(warnings, imageVisionFailureWarning(fh.Filename, err))
			continue
		}
		inTok += usg.InputTokens
		outTok += usg.OutputTokens
		processed++
		_ = usage.RecordEvent(ctx, u.TenantSchema, "ai_token", usg.InputTokens+usg.OutputTokens, nil)
		var parsed struct {
			Items []TherapyImageDraftItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(aivision.SanitizeVisionJSON(raw)), &parsed); err != nil {
			warnings = append(warnings, fh.Filename+": format AI tidak valid")
			continue
		}
		for _, it := range parsed.Items {
			if len(items) >= eventImageMaxTherapyItems {
				break
			}
			it.TherapyName = strings.TrimSpace(it.TherapyName)
			if it.TherapyName == "" {
				continue
			}
			it.Include = true
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil, appErrs.BadRequest("tidak ada terapi terdeteksi")
	}
	jobID, err := saveStaging(ctx, "therapies", therapyStaging{TenantSchema: u.TenantSchema, Items: items})
	if err != nil {
		return nil, err
	}
	return &TherapyImagePreviewResponse{
		Items: items,
		ImagePreviewMeta: ImagePreviewMeta{
			JobID: jobID, ImagesProcessed: processed,
			InputTokens: inTok, OutputTokens: outTok, TokensUsed: inTok + outTok,
			TokenQuotaRemaining: rem, TokenQuotaLimit: lim, Warnings: warnings,
		},
	}, nil
}
