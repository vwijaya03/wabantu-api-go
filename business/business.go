package business

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

// tenantDB references the shared tenant database declared in the tenant service.
var tenantDB = sqldb.Named("tenant")

var secrets struct {
	AnthropicAPIKey string
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type ProfileResponse struct {
	ID                string    `json:"id"`
	BusinessName      string    `json:"businessName"`
	Description       *string   `json:"description"`
	Address           *string   `json:"address"`
	OpeningHours      *string   `json:"openingHours"`
	ProductsServices  *string   `json:"productsServices"`
	BasePricing       *string   `json:"basePricing"`
	DeliveryArea      *string   `json:"deliveryArea"`
	GreetingTemplate  *string   `json:"greetingTemplate"`
	Tone              *string   `json:"tone"`
	AIEnabled         bool      `json:"aiEnabled"`
	ReportingTimezone string    `json:"reportingTimezone"`
	CatalogWebsiteURL *string   `json:"catalogWebsiteUrl"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type GetProfileResponse struct {
	Profile ProfileResponse `json:"profile"`
}

type UpdateProfileRequest struct {
	BusinessName      *string `json:"businessName"`
	Description       *string `json:"description"`
	Address           *string `json:"address"`
	OpeningHours      *string `json:"openingHours"`
	ProductsServices  *string `json:"productsServices"`
	BasePricing       *string `json:"basePricing"`
	DeliveryArea      *string `json:"deliveryArea"`
	GreetingTemplate  *string `json:"greetingTemplate"`
	Tone              *string `json:"tone"`
	AIEnabled         *bool   `json:"aiEnabled"`
	ReportingTimezone *string `json:"reportingTimezone"`
	CatalogWebsiteURL *string `json:"catalogWebsiteUrl"`
}

type UpdateProfileResponse struct {
	Profile ProfileResponse `json:"profile"`
}

type ImportPreviewRequest struct {
	URL string `json:"url"`
}

type ImportPreviewResponse struct {
	Valid         bool           `json:"valid"`
	InvalidReason *string        `json:"invalidReason"`
	SourceURL     string         `json:"sourceUrl"`
	FinalURL      string         `json:"finalUrl"`
	PageTitle     *string        `json:"pageTitle"`
	Confidence    string         `json:"confidence"` // high | medium | low
	Fields        ImportFieldSet `json:"fields"`
	Notes         []string       `json:"notes"`
}

type ImportFieldSet struct {
	BusinessName     *string `json:"businessName"`
	Description      *string `json:"description"`
	Address          *string `json:"address"`
	OpeningHours     *string `json:"openingHours"`
	ProductsServices *string `json:"productsServices"`
	BasePricing      *string `json:"basePricing"`
	DeliveryArea     *string `json:"deliveryArea"`
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

const profileCols = `id, business_name, description, address, opening_hours,
	products_services, base_pricing, delivery_area, greeting_template, tone,
	ai_enabled, reporting_timezone, catalog_website_url, created_at, updated_at`

const profileTable = "business_profile"

func currentUser() (*types.AuthUser, error) {
	data, ok := auth.Data().(*types.AuthUser)
	if !ok || data == nil {
		return nil, apperr.Unauthenticated("not authenticated")
	}
	return data, nil
}

func tConn(ctx context.Context, schema string) (*sql.Conn, error) {
	return appdb.TenantConn(ctx, tenantDB.Stdlib(), schema)
}

func scanProfile(scanner interface{ Scan(...any) error }) (ProfileResponse, error) {
	var p ProfileResponse
	err := scanner.Scan(
		&p.ID, &p.BusinessName, &p.Description, &p.Address,
		&p.OpeningHours, &p.ProductsServices, &p.BasePricing,
		&p.DeliveryArea, &p.GreetingTemplate, &p.Tone,
		&p.AIEnabled, &p.ReportingTimezone, &p.CatalogWebsiteURL,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

func isValidTimezone(tz string) bool {
	_, err := time.LoadLocation(tz)
	return err == nil
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

// GetProfile returns the business profile for the current tenant.
// If none exists yet, a default placeholder is created automatically.
//
//encore:api auth method=GET path=/api/v1/business/profile
func GetProfile(ctx context.Context) (*GetProfileResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	row := conn.QueryRowContext(ctx,
		`SELECT `+profileCols+` FROM `+profileTable+` ORDER BY created_at ASC LIMIT 1`)
	p, err := scanProfile(row)
	if err == sql.ErrNoRows {
		row = conn.QueryRowContext(ctx,
			`INSERT INTO `+profileTable+` (business_name, reporting_timezone)
			 VALUES ($1, $2)
			 RETURNING `+profileCols,
			"Bisnis Baru", "Asia/Jakarta")
		p, err = scanProfile(row)
	}
	if err != nil {
		rlog.Error("get/create profile failed", "err", err)
		return nil, apperr.Internal("failed to load profile")
	}
	return &GetProfileResponse{Profile: p}, nil
}

// UpdateProfile patches the business profile (owner only).
//
//encore:api auth method=PATCH path=/api/v1/business/profile
func UpdateProfile(ctx context.Context, req *UpdateProfileRequest) (*UpdateProfileResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can update profile")
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	var profileID string
	if err := conn.QueryRowContext(ctx,
		`SELECT id FROM `+profileTable+` ORDER BY created_at ASC LIMIT 1`,
	).Scan(&profileID); err != nil {
		return nil, apperr.NotFound("profile not found")
	}

	sets := []string{}
	args := []interface{}{}
	idx := 1

	addStr := func(col string, val *string) {
		if val != nil {
			sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
			args = append(args, *val)
			idx++
		}
	}

	addStr("business_name", req.BusinessName)
	addStr("description", req.Description)
	addStr("address", req.Address)
	addStr("opening_hours", req.OpeningHours)
	addStr("products_services", req.ProductsServices)
	addStr("base_pricing", req.BasePricing)
	addStr("delivery_area", req.DeliveryArea)
	addStr("greeting_template", req.GreetingTemplate)
	addStr("tone", req.Tone)
	addStr("catalog_website_url", req.CatalogWebsiteURL)

	if req.AIEnabled != nil {
		sets = append(sets, fmt.Sprintf("ai_enabled = $%d", idx))
		args = append(args, *req.AIEnabled)
		idx++
	}
	if req.ReportingTimezone != nil {
		tz := strings.TrimSpace(*req.ReportingTimezone)
		if !isValidTimezone(tz) {
			return nil, apperr.BadRequest("timezone tidak didukung")
		}
		sets = append(sets, fmt.Sprintf("reporting_timezone = $%d", idx))
		args = append(args, tz)
		idx++
	}

	if len(sets) == 0 {
		return nil, apperr.BadRequest("no fields to update")
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, profileID)

	q := fmt.Sprintf(
		`UPDATE `+profileTable+` SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(sets, ", "), idx, profileCols,
	)
	p, err := scanProfile(conn.QueryRowContext(ctx, q, args...))
	if err != nil {
		rlog.Error("update profile failed", "err", err)
		return nil, apperr.Internal("failed to update profile")
	}
	return &UpdateProfileResponse{Profile: p}, nil
}

// ImportPreview crawls a website URL and extracts business profile fields.
// Uses SSRF guards and optionally calls Anthropic for structured extraction.
//
//encore:api auth method=POST path=/api/v1/business/profile/import-preview
func ImportPreview(ctx context.Context, req *ImportPreviewRequest) (*ImportPreviewResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if !user.CanPerformOwnerActions() {
		return nil, apperr.Forbidden("only owner can import from website")
	}
	return previewFromURL(ctx, req.URL, secrets.AnthropicAPIKey)
}
