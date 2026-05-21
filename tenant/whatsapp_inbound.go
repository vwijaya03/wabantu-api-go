package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/system"
)

// isMissingRow reports whether err is "no matching row" from system.DB (Encore may wrap as errs.NotFound).
func isMissingRow(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	var e *errs.Error
	if errors.As(err, &e) && e.Code == errs.NotFound {
		return strings.Contains(e.Message, "no rows in result set")
	}
	return false
}

// WhatsAppInboundRef is a resolved tenant channel for Meta webhook ingress.
type WhatsAppInboundRef struct {
	Schema    string
	ChannelID string
}

// RegisterWhatsAppInbound binds a Meta phone_number_id to exactly one tenant channel.
// Re-registering the same schema+channel updates the row; another tenant with the same ID is rejected.
func RegisterWhatsAppInbound(ctx context.Context, schema, channelID, metaPhoneNumberID, displayPhone string) error {
	metaPhoneNumberID = strings.TrimSpace(metaPhoneNumberID)
	if metaPhoneNumberID == "" {
		return nil
	}
	schema = strings.TrimSpace(schema)
	channelID = strings.TrimSpace(channelID)
	if schema == "" || channelID == "" {
		return fmt.Errorf("schema and channel_id required")
	}

	var existingSchema, existingChannel string
	err := system.DB.QueryRow(ctx,
		`SELECT tenant_schema, channel_id::text FROM whatsapp_inbound_map WHERE meta_phone_number_id = $1`,
		metaPhoneNumberID,
	).Scan(&existingSchema, &existingChannel)
	if err == nil {
		if existingSchema == schema && existingChannel == channelID {
			_, _ = system.DB.Exec(ctx,
				`UPDATE whatsapp_inbound_map SET display_phone_norm = $1, updated_at = NOW()
				 WHERE meta_phone_number_id = $2`,
				normalizePhoneDigits(displayPhone), metaPhoneNumberID)
			return nil
		}
		return fmt.Errorf(
			"meta_phone_number_id %s sudah dipakai tenant %s (channel %s); putuskan koneksi di tenant itu atau gunakan nomor Meta berbeda",
			metaPhoneNumberID, existingSchema, existingChannel,
		)
	}
	if err != nil && !isMissingRow(err) {
		return err
	}

	_, err = system.DB.Exec(ctx,
		`INSERT INTO whatsapp_inbound_map (meta_phone_number_id, tenant_schema, channel_id, display_phone_norm)
		 VALUES ($1, $2, $3::uuid, $4)`,
		metaPhoneNumberID, schema, channelID, normalizePhoneDigits(displayPhone),
	)
	return err
}

// UnregisterWhatsAppInbound removes routing for a channel (on disconnect / delete).
func UnregisterWhatsAppInbound(ctx context.Context, schema, channelID string) error {
	_, err := system.DB.Exec(ctx,
		`DELETE FROM whatsapp_inbound_map WHERE tenant_schema = $1 AND channel_id = $2::uuid`,
		schema, channelID,
	)
	return err
}

// ResolveWhatsAppInbound finds the tenant for an inbound Meta webhook.
func ResolveWhatsAppInbound(ctx context.Context, metaPhoneNumberID, displayPhone string) (*WhatsAppInboundRef, error) {
	metaPhoneNumberID = strings.TrimSpace(metaPhoneNumberID)
	if metaPhoneNumberID != "" {
		var ref WhatsAppInboundRef
		err := system.DB.QueryRow(ctx,
			`SELECT tenant_schema, channel_id::text FROM whatsapp_inbound_map WHERE meta_phone_number_id = $1`,
			metaPhoneNumberID,
		).Scan(&ref.Schema, &ref.ChannelID)
		if err == nil {
			return &ref, nil
		}
		if err != nil && !isMissingRow(err) {
			return nil, err
		}
	}

	// Map miss: scan tenant schemas (backfills whatsapp_inbound_map when unambiguous).
	return resolveInboundChannelScan(ctx, metaPhoneNumberID, displayPhone)
}

func resolveInboundChannelScan(ctx context.Context, phoneNumberID, displayPhone string) (*WhatsAppInboundRef, error) {
	schemas, err := ListSchemaNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant schemas: %w", err)
	}
	pool := tenantPool()
	phoneNumberID = strings.TrimSpace(phoneNumberID)
	normDisplay := normalizePhoneDigits(displayPhone)

	var byMetaID []WhatsAppInboundRef
	if phoneNumberID != "" {
		for _, schema := range schemas {
			var id string
			err := pool.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT id::text FROM %q.whatsapp_channel WHERE meta_phone_number_id = $1`, schema),
				phoneNumberID,
			).Scan(&id)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				continue
			}
			byMetaID = append(byMetaID, WhatsAppInboundRef{Schema: schema, ChannelID: id})
		}
		if len(byMetaID) > 1 {
			names := make([]string, len(byMetaID))
			for i, m := range byMetaID {
				names[i] = m.Schema
			}
			return nil, fmt.Errorf(
				"meta_phone_number_id %s terdaftar di beberapa tenant (%s) — hanya satu tenant boleh memakai nomor Meta yang sama; reconnect WhatsApp di tenant yang benar",
				phoneNumberID, strings.Join(names, ", "),
			)
		}
		if len(byMetaID) == 1 {
			ref := byMetaID[0]
			_ = RegisterWhatsAppInbound(ctx, ref.Schema, ref.ChannelID, phoneNumberID, displayPhone)
			rlog.Info("backfilled whatsapp_inbound_map from scan", "schema", ref.Schema, "phoneNumberId", phoneNumberID)
			return &ref, nil
		}
	}

	var byPhone []WhatsAppInboundRef
	for _, schema := range schemas {
		var id string
		q := fmt.Sprintf(`
			SELECT id::text FROM %q.whatsapp_channel
			WHERE ($1 <> '' AND REGEXP_REPLACE(phone_number, '[^0-9]', '', 'g') = $1)
			   OR ($2 <> '' AND phone_number = $2)
			LIMIT 1`, schema)
		err := pool.QueryRowContext(ctx, q, normDisplay, strings.TrimSpace(displayPhone)).Scan(&id)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			continue
		}
		byPhone = append(byPhone, WhatsAppInboundRef{Schema: schema, ChannelID: id})
	}
	if len(byPhone) > 1 {
		names := make([]string, len(byPhone))
		for i, m := range byPhone {
			names[i] = m.Schema
		}
		return nil, fmt.Errorf(
			"nomor WhatsApp bisnis cocok di beberapa tenant (%s) — gunakan meta_phone_number_id unik per tenant",
			strings.Join(names, ", "),
		)
	}
	if len(byPhone) == 1 {
		ref := byPhone[0]
		if phoneNumberID != "" {
			_, _ = pool.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %q.whatsapp_channel SET meta_phone_number_id = $1, updated_at = NOW() WHERE id = $2::uuid`, ref.Schema),
				phoneNumberID, ref.ChannelID)
			_ = RegisterWhatsAppInbound(ctx, ref.Schema, ref.ChannelID, phoneNumberID, displayPhone)
		}
		return &ref, nil
	}

	return nil, fmt.Errorf(
		"no tenant channel for phone_number_id=%s display=%s",
		phoneNumberID, displayPhone,
	)
}

func tenantPool() *sql.DB {
	return DataDB.Stdlib()
}

func normalizePhoneDigits(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
