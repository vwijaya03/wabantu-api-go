package events

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/response"
)

func writeEventImageJSONError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch errs.Code(err) {
	case errs.PermissionDenied:
		code = http.StatusForbidden
	case errs.NotFound:
		code = http.StatusNotFound
	case errs.Unauthenticated:
		code = http.StatusUnauthorized
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": err.Error()})
}

func parseEventImageUpload(req *http.Request) ([]*multipart.FileHeader, error) {
	if err := req.ParseMultipartForm(eventImageMaxBatchBytes); err != nil {
		return nil, appErrs.BadRequest("gagal membaca form upload")
	}
	files := req.MultipartForm.File["files"]
	if len(files) == 0 {
		files = req.MultipartForm.File["file"]
	}
	return files, nil
}

func eventIDFromPath(req *http.Request) string {
	if v := req.PathValue("eventId"); v != "" {
		return v
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	for i, p := range parts {
		if p == "detail" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

//encore:api auth raw method=POST path=/api/v1/events/detail/:eventId/people/import-image/preview tag:owner
func PreviewStaffImageHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	user, err := appauth.AuthenticateHTTP(ctx, req)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.CanPerformOwnerActions() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	eventID := eventIDFromPath(req)
	if eventID == "" {
		writeEventImageJSONError(w, appErrs.BadRequest("event tidak valid"))
		return
	}
	files, err := parseEventImageUpload(req)
	if err != nil {
		writeEventImageJSONError(w, err)
		return
	}
	resp, err := previewStaffImages(ctx, user, eventID, files)
	if err != nil {
		rlog.Warn("staff image preview failed", "err", err, "event", eventID)
		writeEventImageJSONError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response.Wrap(resp))
}

//encore:api auth raw method=POST path=/api/v1/events/detail/:eventId/patients/import-image/preview tag:owner
func PreviewPatientImageHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	user, err := appauth.AuthenticateHTTP(ctx, req)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.CanPerformOwnerActions() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	eventID := eventIDFromPath(req)
	if eventID == "" {
		writeEventImageJSONError(w, appErrs.BadRequest("event tidak valid"))
		return
	}
	files, err := parseEventImageUpload(req)
	if err != nil {
		writeEventImageJSONError(w, err)
		return
	}
	resp, err := previewPatientImages(ctx, user, eventID, files)
	if err != nil {
		writeEventImageJSONError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response.Wrap(resp))
}

//encore:api auth raw method=POST path=/api/v1/events/masters/therapies/import-image/preview tag:owner
func PreviewTherapyImageHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	user, err := appauth.AuthenticateHTTP(ctx, req)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.CanPerformOwnerActions() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	files, err := parseEventImageUpload(req)
	if err != nil {
		writeEventImageJSONError(w, err)
		return
	}
	resp, err := previewTherapyImages(ctx, user, files)
	if err != nil {
		writeEventImageJSONError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response.Wrap(resp))
}
