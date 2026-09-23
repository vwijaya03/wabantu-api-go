package chatwidget

import (
	"context"
	"encoding/json"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
)

// checkEmbedOrigin enforces allowed_domains when configured (soft embed control).
func checkEmbedOrigin(ctx context.Context, ts appdb.TenantScope, origin string) error {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return nil
	}
	var raw []byte
	err := ts.QueryRowContext(ctx, `
		SELECT allowed_domains FROM `+ts.T("chat_widget_config")+` LIMIT 1`).Scan(&raw)
	if err != nil {
		return nil
	}
	var domains []string
	if err := json.Unmarshal(raw, &domains); err != nil || len(domains) == 0 {
		return nil
	}
	host := originHost(origin)
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(d))
		if d == "" {
			continue
		}
		if d == host || d == origin || strings.HasSuffix(host, "."+strings.TrimPrefix(d, ".")) {
			return nil
		}
	}
	return appErrs.Forbidden("domain tidak diizinkan untuk embed widget")
}

func originHost(origin string) string {
	origin = strings.TrimSpace(strings.ToLower(origin))
	origin = strings.TrimPrefix(origin, "https://")
	origin = strings.TrimPrefix(origin, "http://")
	if i := strings.Index(origin, "/"); i >= 0 {
		origin = origin[:i]
	}
	if i := strings.Index(origin, ":"); i >= 0 {
		origin = origin[:i]
	}
	return origin
}
