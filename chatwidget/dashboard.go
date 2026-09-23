package chatwidget

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

type WidgetConfig struct {
	Enabled        bool            `json:"enabled"`
	WelcomeMessage string          `json:"welcomeMessage,omitempty"`
	PersonaName    string          `json:"personaName,omitempty"`
	Position       string          `json:"position"`
	Locale         string          `json:"locale"`
	AllowedDomains []string        `json:"allowedDomains"`
	CustomTokens   json.RawMessage `json:"customTokens,omitempty"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type UpdateWidgetConfigRequest struct {
	Enabled        bool            `json:"enabled"`
	WelcomeMessage string          `json:"welcomeMessage,omitempty"`
	PersonaName    string          `json:"personaName,omitempty"`
	Position       string          `json:"position,omitempty"`
	Locale         string          `json:"locale,omitempty"`
	AllowedDomains []string        `json:"allowedDomains,omitempty"`
	CustomTokens   json.RawMessage `json:"customTokens,omitempty"`
}

type EmbedSnippetResponse struct {
	HTML string `json:"html"`
}

func owner(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	return u, nil
}

//encore:api auth method=GET path=/api/v1/chat-widget/config tag:owner
func GetChatWidgetConfig(ctx context.Context) (*WidgetConfig, error) {
	u, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	var cfg WidgetConfig
	var domains []byte
	var tokens []byte
	err = ts.QueryRowContext(ctx, `
		SELECT is_enabled, COALESCE(welcome_message, ''), COALESCE(persona_name, ''),
		       position, locale, allowed_domains, custom_tokens, updated_at
		FROM `+ts.T("chat_widget_config")+` LIMIT 1`).
		Scan(&cfg.Enabled, &cfg.WelcomeMessage, &cfg.PersonaName, &cfg.Position, &cfg.Locale, &domains, &tokens, &cfg.UpdatedAt)
	if err == sql.ErrNoRows {
		return &WidgetConfig{Position: "bottom-right", Locale: "id"}, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(domains, &cfg.AllowedDomains)
	if len(tokens) > 0 {
		cfg.CustomTokens = tokens
	}
	return &cfg, nil
}

//encore:api auth method=PUT path=/api/v1/chat-widget/config tag:owner
func UpdateChatWidgetConfig(ctx context.Context, req *UpdateWidgetConfigRequest) (*WidgetConfig, error) {
	u, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}
	pos := strings.TrimSpace(req.Position)
	if pos == "" {
		pos = "bottom-right"
	}
	loc := strings.TrimSpace(req.Locale)
	if loc == "" {
		loc = "id"
	}
	domains, _ := json.Marshal(req.AllowedDomains)
	tokens := req.CustomTokens
	if tokens == nil {
		tokens = json.RawMessage(`{}`)
	}
	cfgTable := ts.T("chat_widget_config")
	var exists bool
	if err := ts.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+cfgTable+`)`).Scan(&exists); err != nil {
		return nil, err
	}
	var updatedAt time.Time
	if exists {
		err = ts.QueryRowContext(ctx, `
			UPDATE `+cfgTable+`
			SET is_enabled = $1, welcome_message = $2, persona_name = $3, position = $4,
			    locale = $5, allowed_domains = $6::jsonb, custom_tokens = $7::jsonb, updated_at = now()
			WHERE id = (SELECT id FROM `+cfgTable+` LIMIT 1)
			RETURNING updated_at`,
			req.Enabled, req.WelcomeMessage, req.PersonaName, pos, loc, domains, tokens).Scan(&updatedAt)
	} else {
		err = ts.QueryRowContext(ctx, `
			INSERT INTO `+cfgTable+`
			(is_enabled, welcome_message, persona_name, position, locale, allowed_domains, custom_tokens, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, now())
			RETURNING updated_at`,
			req.Enabled, req.WelcomeMessage, req.PersonaName, pos, loc, domains, tokens).Scan(&updatedAt)
	}
	if err != nil {
		return nil, err
	}
	return &WidgetConfig{
		Enabled: req.Enabled, WelcomeMessage: req.WelcomeMessage, PersonaName: req.PersonaName,
		Position: pos, Locale: loc, AllowedDomains: req.AllowedDomains, CustomTokens: tokens, UpdatedAt: updatedAt,
	}, nil
}

//encore:api auth method=GET path=/api/v1/chat-widget/embed-snippet tag:owner
func GetChatWidgetEmbedSnippet(ctx context.Context) (*EmbedSnippetResponse, error) {
	u, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	slug := strings.TrimPrefix(u.TenantSchema, "t_")
	html := fmt.Sprintf(`<script async src="https://app.wabantu.id/embed/%s/chat.js"></script>`, slug)
	return &EmbedSnippetResponse{HTML: html}, nil
}
