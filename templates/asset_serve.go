package templates

import (
	"database/sql"
	"net/http"
	"strings"

	"encore.app/wabantu/shared/mediastorage"
)

// ServeWebComponentModule streams a validated web component bundle with short-lived token.
//
//encore:api public raw method=GET path=/api/v1/public/template-assets/:assetId/module.js
func ServeWebComponentModule(w http.ResponseWriter, r *http.Request) {
	assetID := parseAssetID(r.URL.Path)
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if assetID == "" || token == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !verifyAssetToken(assetSecrets.JWTSecret, assetID, token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	var s3Key, kind, status string
	err := sysDB.QueryRow(ctx, `
		SELECT s3_key, kind, scan_status FROM template_asset WHERE id = $1::uuid`, assetID).
		Scan(&s3Key, &kind, &status)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	if kind != "web_component" || status != "clean" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	data, mime, err := mediastorage.Get(ctx, s3Key)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if mime == "" {
		mime = "text/javascript"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func parseAssetID(path string) string {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	// /api/v1/public/template-assets/:assetId/module.js
	if len(parts) < 6 {
		return ""
	}
	return parts[4]
}
