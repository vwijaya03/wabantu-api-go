package storefront

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

type StorefrontConfig struct {
	Enabled     bool            `json:"enabled"`
	StoreTitle  string          `json:"storeTitle,omitempty"`
	Description string          `json:"description,omitempty"`
	SEOTitle    string          `json:"seoTitle,omitempty"`
	SEODesc     string          `json:"seoDescription,omitempty"`
	Tokens      json.RawMessage `json:"customTokens,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

//encore:api auth method=GET path=/api/v1/storefront/config tag:owner
func GetStorefrontConfig(ctx context.Context) (*StorefrontConfig, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	var cfg StorefrontConfig
	var tokens []byte
	err = ts.QueryRowContext(ctx, `
		SELECT is_enabled, COALESCE(store_title,''), COALESCE(store_description,''),
		       COALESCE(seo_title,''), COALESCE(seo_description,''), custom_tokens, updated_at
		FROM `+ts.T("storefront_config")+` LIMIT 1`).
		Scan(&cfg.Enabled, &cfg.StoreTitle, &cfg.Description, &cfg.SEOTitle, &cfg.SEODesc, &tokens, &cfg.UpdatedAt)
	if err == sql.ErrNoRows {
		return &StorefrontConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 {
		cfg.Tokens = tokens
	}
	return &cfg, nil
}

//encore:api auth method=PUT path=/api/v1/storefront/config tag:owner
func UpdateStorefrontConfig(ctx context.Context, cfg *StorefrontConfig) (*StorefrontConfig, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	tokens := cfg.Tokens
	if tokens == nil {
		tokens = json.RawMessage(`{}`)
	}
	table := ts.T("storefront_config")
	var exists bool
	_ = ts.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+`)`).Scan(&exists)
	var updatedAt time.Time
	if exists {
		err = ts.QueryRowContext(ctx, `
			UPDATE `+table+` SET is_enabled=$1, store_title=$2, store_description=$3,
			    seo_title=$4, seo_description=$5, custom_tokens=$6::jsonb, updated_at=now()
			WHERE id = (SELECT id FROM `+table+` LIMIT 1) RETURNING updated_at`,
			cfg.Enabled, cfg.StoreTitle, cfg.Description, cfg.SEOTitle, cfg.SEODesc, tokens).Scan(&updatedAt)
	} else {
		err = ts.QueryRowContext(ctx, `
			INSERT INTO `+table+` (is_enabled, store_title, store_description, seo_title, seo_description, custom_tokens, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,now()) RETURNING updated_at`,
			cfg.Enabled, cfg.StoreTitle, cfg.Description, cfg.SEOTitle, cfg.SEODesc, tokens).Scan(&updatedAt)
	}
	if err != nil {
		return nil, err
	}
	cfg.UpdatedAt = updatedAt
	return cfg, nil
}
