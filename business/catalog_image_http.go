package business

import (
	"encoding/json"
	"net/http"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/response"
)

// PreviewCatalogImageImportHTTP accepts one or more screenshots (multipart field "files" or legacy "file").
//
//encore:api auth raw method=POST path=/api/v1/business/catalog/import-image/preview tag:owner
func PreviewCatalogImageImportHTTP(w http.ResponseWriter, req *http.Request) {
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

	if err := req.ParseMultipartForm(CatalogImageMaxBatchBytes); err != nil {
		writeCatalogImageJSONError(w, apperr.BadRequest("gagal membaca form upload"))
		return
	}

	files := req.MultipartForm.File["files"]
	if len(files) == 0 {
		files = req.MultipartForm.File["file"]
	}

	resp, err := previewCatalogFromMultipart(ctx, user, files)
	if err != nil {
		rlog.Warn("catalog image preview failed", "err", catalogImageErrMessage(err), "files", len(files), "tenant", user.TenantSchema)
		writeCatalogImageAPIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response.Wrap(resp))
}

func writeCatalogImageJSONError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": catalogImageErrMessage(err),
	})
}

func writeCatalogImageAPIError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch errs.Code(err) {
	case errs.InvalidArgument:
		code = http.StatusBadRequest
	case errs.PermissionDenied:
		code = http.StatusForbidden
	case errs.NotFound:
		code = http.StatusNotFound
	case errs.Unauthenticated:
		code = http.StatusUnauthorized
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": catalogImageErrMessage(err),
	})
}

