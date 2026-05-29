package finance

import (
	"encoding/json"
	"net/http"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/response"
)

// PreviewTransactionImageImportHTTP accepts screenshots of transaction lists (multipart "files" or "file").
//
//encore:api auth raw method=POST path=/api/v1/finance/transactions/import-image/preview tag:owner
func PreviewTransactionImageImportHTTP(w http.ResponseWriter, req *http.Request) {
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

	if err := req.ParseMultipartForm(txnImageMaxBatchBytes); err != nil {
		writeTxnImageJSONError(w, appErrs.BadRequest("gagal membaca form upload"))
		return
	}

	files := req.MultipartForm.File["files"]
	if len(files) == 0 {
		files = req.MultipartForm.File["file"]
	}

	resp, err := previewTransactionsFromMultipart(ctx, user, files)
	if err != nil {
		rlog.Warn("transaction image preview failed", "err", txnImageErrMessage(err), "files", len(files), "tenant", user.TenantSchema)
		writeTxnImageAPIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response.Wrap(resp))
}

func writeTxnImageJSONError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": txnImageErrMessage(err),
	})
}

func writeTxnImageAPIError(w http.ResponseWriter, err error) {
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
		"message": txnImageErrMessage(err),
	})
}
