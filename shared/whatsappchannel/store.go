// Package whatsappchannel loads and stores WhatsApp channel credentials with optional PII encryption.
package whatsappchannel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
)

// WriteFields are column values for INSERT/UPDATE on whatsapp_channel.
type WriteFields struct {
	UsePII          bool
	DisplayName     string
	DisplayNameEnc  sql.NullString
	PhoneNumber     string
	PhoneNumberEnc  sql.NullString
	PhoneNumberIdx  sql.NullString
	AccessToken     string
	AccessTokenEnc  sql.NullString
}

// PrepareWrite builds encrypted/plain column values for channel upsert.
func PrepareWrite(displayName, phone, accessToken, encKey string) (WriteFields, error) {
	displayName = strings.TrimSpace(displayName)
	phone = strings.TrimSpace(phone)
	accessToken = strings.TrimSpace(accessToken)

	if err := pii.ValidateKey(encKey); err != nil {
		return WriteFields{
			UsePII:      false,
			DisplayName: displayName,
			PhoneNumber: phone,
			AccessToken: accessToken,
		}, nil
	}

	out := WriteFields{UsePII: true, DisplayName: pii.Placeholder, PhoneNumber: pii.Placeholder, AccessToken: pii.Placeholder}

	if displayName != "" {
		enc, err := pii.Encrypt(displayName, encKey)
		if err != nil {
			return WriteFields{}, err
		}
		out.DisplayNameEnc = sql.NullString{String: enc, Valid: true}
	}
	if phone != "" {
		enc, err := pii.Encrypt(phone, encKey)
		if err != nil {
			return WriteFields{}, err
		}
		out.PhoneNumberEnc = sql.NullString{String: enc, Valid: true}
		out.PhoneNumberIdx = sql.NullString{String: pii.BlindIndex(pii.NormalizePhone(phone), encKey), Valid: true}
	}
	if accessToken != "" {
		enc, err := pii.Encrypt(accessToken, encKey)
		if err != nil {
			return WriteFields{}, err
		}
		out.AccessTokenEnc = sql.NullString{String: enc, Valid: true}
	}
	return out, nil
}

// PIIActive reports whether whatsapp_channel has encryption columns applied.
func PIIActive(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, schema string) (bool, error) {
	return tenantschema.ColumnExists(ctx, q, schema, "whatsapp_channel", "access_token_enc")
}

// FindIDByPhone returns channel id matching normalized phone (encrypted index or legacy plaintext).
func FindIDByPhone(ctx context.Context, ts appdb.TenantScope, phone, encKey string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", sql.ErrNoRows
	}
	if err := pii.ValidateKey(encKey); err == nil {
		idx := pii.BlindIndex(pii.NormalizePhone(phone), encKey)
		var id string
		err := ts.QueryRowContext(ctx,
			`SELECT id::text FROM whatsapp_channel WHERE phone_number_idx = $1 LIMIT 1`, idx,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	var id string
	err := ts.QueryRowContext(ctx,
		`SELECT id::text FROM whatsapp_channel WHERE phone_number = $1 LIMIT 1`, phone,
	).Scan(&id)
	return id, err
}

// Credentials are decrypted channel secrets used for Meta API calls.
type Credentials struct {
	AccessToken       string
	MetaPhoneNumberID string
	Provider          string
	Status            string
}

// ChannelRow is a decrypted whatsapp_channel row for internal services.
type ChannelRow struct {
	ID                string
	Provider          string
	Status            string
	AccessToken       string
	DisplayName       string
	PhoneNumber       string
	MetaPhoneNumberID string
	MetaWabaID        string
}

// LoadFull loads and decrypts a whatsapp_channel row by id.
func LoadFull(ctx context.Context, ts appdb.TenantScope, channelID, encKey string) (*ChannelRow, error) {
	var (
		provider, status string
		tokenEnc, tokenLegacy string
		displayEnc, displayLegacy string
		phoneEnc, phoneLegacy string
		metaPhoneID, metaWabaID sql.NullString
	)
	err := ts.QueryRowContext(ctx, `
		SELECT provider, status,
		       COALESCE(access_token_enc, ''), COALESCE(access_token, ''),
		       COALESCE(display_name_enc, ''), COALESCE(display_name, ''),
		       COALESCE(phone_number_enc, ''), COALESCE(phone_number, ''),
		       meta_phone_number_id, meta_waba_id
		FROM whatsapp_channel WHERE id = $1`, channelID,
	).Scan(&provider, &status, &tokenEnc, &tokenLegacy, &displayEnc, &displayLegacy, &phoneEnc, &phoneLegacy, &metaPhoneID, &metaWabaID)
	if err != nil {
		return nil, err
	}
	token, err := pii.DecryptOrLegacy(tokenEnc, tokenLegacy, encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt access_token: %w", err)
	}
	display, err := DecryptDisplay(displayEnc, displayLegacy, phoneEnc, phoneLegacy, encKey)
	if err != nil {
		return nil, err
	}
	row := &ChannelRow{
		ID:          channelID,
		Provider:    provider,
		Status:      status,
		AccessToken: strings.TrimSpace(token),
		DisplayName: display.DisplayName,
		PhoneNumber: display.PhoneNumber,
	}
	if metaPhoneID.Valid {
		row.MetaPhoneNumberID = strings.TrimSpace(metaPhoneID.String)
	}
	if metaWabaID.Valid {
		row.MetaWabaID = strings.TrimSpace(metaWabaID.String)
	}
	return row, nil
}

// LoadCredentials loads and decrypts access_token for a channel id.
func LoadCredentials(ctx context.Context, ts appdb.TenantScope, channelID, encKey string) (*Credentials, error) {
	row, err := LoadFull(ctx, ts, channelID, encKey)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		AccessToken:       row.AccessToken,
		MetaPhoneNumberID: row.MetaPhoneNumberID,
		Provider:          row.Provider,
		Status:            row.Status,
	}, nil
}

// DisplayRow holds decrypted display fields for API responses.
type DisplayRow struct {
	DisplayName string
	PhoneNumber string
}

// DecryptDisplay returns decrypted display_name and phone_number for list/detail APIs.
func DecryptDisplay(displayEnc, displayLegacy, phoneEnc, phoneLegacy, encKey string) (DisplayRow, error) {
	display, err := pii.DecryptOrLegacy(displayEnc, displayLegacy, encKey)
	if err != nil {
		return DisplayRow{}, err
	}
	phone, err := pii.DecryptOrLegacy(phoneEnc, phoneLegacy, encKey)
	if err != nil {
		return DisplayRow{}, err
	}
	return DisplayRow{
		DisplayName: strings.TrimSpace(display),
		PhoneNumber: strings.TrimSpace(phone),
	}, nil
}

// ConnectedTokenQuerySuffix is appended to SELECT for picking a connected channel token.
const ConnectedTokenQuerySuffix = `
		COALESCE(access_token_enc, ''), COALESCE(access_token, ''),
		COALESCE(meta_phone_number_id, '')
		FROM whatsapp_channel
		WHERE status = 'connected'
		  AND (
		    NULLIF(TRIM(access_token_enc), '') IS NOT NULL
		    OR (NULLIF(TRIM(access_token), '') IS NOT NULL AND access_token <> $1)
		  )
		ORDER BY connected_at DESC NULLS LAST LIMIT 1`
