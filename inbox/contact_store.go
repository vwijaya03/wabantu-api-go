package inbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/rlog"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/tenant"
)

var secrets struct {
	DataEncryptionKey string
}

const contactSelectSQL = `
	SELECT id,
	       COALESCE(phone_number_enc, ''),
	       COALESCE(phone_number, ''),
	       COALESCE(display_name_enc, ''),
	       COALESCE(display_name, ''),
	       COALESCE(birth_date_enc, ''),
	       birth_date::text,
	       notes,
	       COALESCE(status, 'active'),
	       price_type_id::text,
	       tags
	FROM contact`

const contactSelectLegacySQL = `
	SELECT id,
	       '',
	       COALESCE(phone_number, ''),
	       '',
	       COALESCE(display_name, ''),
	       '',
	       birth_date::text,
	       notes,
	       COALESCE(status, 'active'),
	       price_type_id::text,
	       tags
	FROM contact`

func contactSelectFor(ctx context.Context, q appdb.TenantQuerier, tenantSchema string) string {
	active, err := tenantschema.ContactPIIActive(ctx, q, tenantSchema)
	if err != nil || !active {
		return contactSelectLegacySQL
	}
	return contactSelectSQL
}

func encKey() string {
	return strings.TrimSpace(secrets.DataEncryptionKey)
}

func ensurePIISchema(ctx context.Context, q any, tenantSchema string) error {
	active, err := tenantschema.ContactPIIActive(ctx, q, tenantSchema)
	if err != nil {
		return err
	}
	if !active {
		if err := tenant.RunPIISchemaPatches(ctx, tenantSchema); err != nil {
			rlog.Warn("PII schema patch failed", "schema", tenantSchema, "err", err)
			return nil
		}
		tenantschema.InvalidateContactPIICache(tenantSchema)
		tenantschema.InvalidateLeadPIICache(tenantSchema)
		if ok, _ := tenantschema.ContactPIIActive(ctx, q, tenantSchema); ok {
			tenantschema.MarkContactPIIActive(tenantSchema)
			tenantschema.MarkLeadPIIActive(tenantSchema)
		}
	}
	return nil
}

func applyContactFieldPII(ctx context.Context, q appdb.TenantQuerier, id string, displayName, birthDate *string) error {
	key := encKey()
	if err := pii.ValidateKey(key); err != nil {
		return err
	}
	if displayName != nil {
		v := strings.TrimSpace(*displayName)
		var enc, idx string
		if v != "" {
			var err error
			enc, err = pii.Encrypt(v, key)
			if err != nil {
				return err
			}
			idx = pii.BlindIndex(pii.NormalizeName(v), key)
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE contact SET display_name_enc = NULLIF($1,''), display_name_idx = NULLIF($2,''),
			  display_name = $3, updated_at = NOW()
			WHERE id = $4`, enc, idx, pii.Placeholder, id); err != nil {
			return err
		}
	}
	if birthDate != nil {
		v := strings.TrimSpace(*birthDate)
		var enc string
		if v != "" {
			var err error
			enc, err = pii.Encrypt(v, key)
			if err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE contact SET birth_date_enc = NULLIF($1,''), birth_date = NULL, updated_at = NOW()
			WHERE id = $2`, enc, id); err != nil {
			return err
		}
	}
	return nil
}

func scanContactPII(scanner interface{ Scan(...any) error }) (ContactDetail, error) {
	var c ContactDetail
	var tagsJSON []byte
	var priceTypeID sql.NullString
	var phoneEnc, phoneLegacy, displayEnc, displayLegacy, birthEnc, birthLegacy sql.NullString
	err := scanner.Scan(
		&c.ID,
		&phoneEnc, &phoneLegacy,
		&displayEnc, &displayLegacy,
		&birthEnc, &birthLegacy,
		&c.Notes, &c.Status, &priceTypeID, &tagsJSON,
	)
	if err != nil {
		return c, err
	}
	key := encKey()
	phone, err := pii.DecryptOrLegacy(phoneEnc.String, phoneLegacy.String, key)
	if err != nil {
		return c, err
	}
	c.PhoneNumber = phone
	if display, err := pii.DecryptOrLegacy(displayEnc.String, displayLegacy.String, key); err != nil {
		return c, err
	} else if display != "" && display != pii.Placeholder {
		c.DisplayName = &display
	}
	if birth, err := pii.DecryptOrLegacy(birthEnc.String, birthLegacy.String, key); err != nil {
		return c, err
	} else if birth != "" {
		bd := birth
		if len(bd) > 10 {
			bd = bd[:10]
		}
		c.BirthDate = &bd
	}
	if c.Status == "" {
		c.Status = "active"
	}
	if priceTypeID.Valid && strings.TrimSpace(priceTypeID.String) != "" {
		v := priceTypeID.String
		c.PriceTypeID = &v
	}
	_ = json.Unmarshal(tagsJSON, &c.Tags)
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c, nil
}

func upsertContactPII(ctx context.Context, q appdb.TenantQuerier, tenantSchema, phone, displayName string, birthDate, notes *string, status string, priceTypeID *string, tagsJSON string) (ContactDetail, error) {
	key := encKey()
	active, _ := tenantschema.ContactPIIActive(ctx, q, tenantSchema)
	if err := pii.ValidateKey(key); err != nil || !active {
		return upsertContactLegacy(ctx, q, phone, displayName, birthDate, notes, status, priceTypeID, tagsJSON)
	}
	phoneEnc, err := pii.Encrypt(phone, key)
	if err != nil {
		return ContactDetail{}, err
	}
	phoneIdx := pii.BlindIndex(pii.NormalizePhone(phone), key)
	var displayEnc, displayIdx string
	if displayName != "" {
		displayEnc, err = pii.Encrypt(displayName, key)
		if err != nil {
			return ContactDetail{}, err
		}
		displayIdx = pii.BlindIndex(pii.NormalizeName(displayName), key)
	}
	var birthEnc string
	if birthDate != nil && strings.TrimSpace(*birthDate) != "" {
		birthEnc, err = pii.Encrypt(strings.TrimSpace(*birthDate), key)
		if err != nil {
			return ContactDetail{}, err
		}
	}
	var existingID string
	err = q.QueryRowContext(ctx, `
		SELECT id FROM contact WHERE phone_number_idx = $1 LIMIT 1`, phoneIdx).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return ContactDetail{}, err
	}
	if err == sql.ErrNoRows {
		return scanContactPII(q.QueryRowContext(ctx, `
			INSERT INTO contact (
			  phone_number, phone_number_enc, phone_number_idx,
			  display_name, display_name_enc, display_name_idx,
			  birth_date, birth_date_enc,
			  notes, status, price_type_id, tags
			) VALUES ($1,$2,$3,$1,$4,$5,NULL,$6,$7,$8,$9,$10::jsonb)
			RETURNING `+contactReturnCols(),
			pii.Placeholder, phoneEnc, phoneIdx, displayEnc, nullIfEmpty(displayIdx), birthEnc,
			notes, status, nullableUUIDPtr(priceTypeID), tagsJSON))
	}
	_, err = q.ExecContext(ctx, `
		UPDATE contact SET
		  phone_number_enc = $1,
		  phone_number_idx = $2,
		  display_name = $3,
		  display_name_enc = NULLIF($4, ''),
		  display_name_idx = NULLIF($5, ''),
		  birth_date_enc = COALESCE(NULLIF($6, ''), birth_date_enc),
		  birth_date = NULL,
		  notes = $7,
		  status = $8,
		  price_type_id = $9,
		  tags = $10::jsonb,
		  deleted_at = NULL,
		  deleted_by = NULL,
		  updated_at = NOW()
		WHERE id = $11`,
		phoneEnc, phoneIdx, pii.Placeholder, displayEnc, displayIdx, birthEnc,
		notes, status, nullableUUIDPtr(priceTypeID), tagsJSON, existingID)
	if err != nil {
		return ContactDetail{}, err
	}
	return scanContactPII(q.QueryRowContext(ctx, contactSelectFor(ctx, q, tenantSchema)+` WHERE id = $1`, existingID))
}

func upsertContactLegacy(ctx context.Context, q appdb.TenantQuerier, phone, displayName string, birthDate, notes *string, status string, priceTypeID *string, tagsJSON string) (ContactDetail, error) {
	var existingID string
	err := q.QueryRowContext(ctx, `
		SELECT id FROM contact WHERE phone_number = $1 AND deleted_at IS NULL LIMIT 1`, phone).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return ContactDetail{}, err
	}
	var dn *string
	if displayName != "" {
		dn = &displayName
	}
	if err == sql.ErrNoRows {
		return scanContactPII(q.QueryRowContext(ctx, `
			INSERT INTO contact (phone_number, display_name, birth_date, notes, status, price_type_id, tags)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
			RETURNING `+contactReturnLegacyCols(),
			phone, dn, birthDate, notes, status, nullableUUIDPtr(priceTypeID), tagsJSON))
	}
	_, err = q.ExecContext(ctx, `
		UPDATE contact SET
		  display_name = COALESCE($1, display_name),
		  birth_date = COALESCE($2, birth_date),
		  notes = COALESCE($3, notes),
		  status = $4,
		  price_type_id = $5,
		  tags = $6::jsonb,
		  deleted_at = NULL,
		  deleted_by = NULL,
		  updated_at = NOW()
		WHERE id = $7`, dn, birthDate, notes, status, nullableUUIDPtr(priceTypeID), tagsJSON, existingID)
	if err != nil {
		return ContactDetail{}, err
	}
	return scanContactPII(q.QueryRowContext(ctx, contactSelectLegacySQL+` WHERE id = $1`, existingID))
}

func contactReturnLegacyCols() string {
	return `id, '', phone_number, '', display_name, '', birth_date::text,
		notes, COALESCE(status, 'active'), price_type_id::text, tags`
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func upsertContactPIIByIdx(ctx context.Context, q appdb.TenantQuerier, phone, displayName string) (string, error) {
	key := encKey()
	if err := pii.ValidateKey(key); err != nil {
		return "", err
	}
	phoneEnc, err := pii.Encrypt(phone, key)
	if err != nil {
		return "", err
	}
	phoneIdx := pii.BlindIndex(pii.NormalizePhone(phone), key)
	var displayEnc *string
	if displayName != "" {
		enc, err := pii.Encrypt(displayName, key)
		if err != nil {
			return "", err
		}
		displayEnc = &enc
	}
	var id string
	err = q.QueryRowContext(ctx, `
		SELECT id FROM contact WHERE phone_number_idx = $1 AND deleted_at IS NULL LIMIT 1`,
		phoneIdx).Scan(&id)
	if err == sql.ErrNoRows {
		err = q.QueryRowContext(ctx, `
			INSERT INTO contact (phone_number, phone_number_enc, phone_number_idx, display_name, display_name_enc, tags)
			VALUES ($1,$2,$3,$1,$4,'["new"]'::jsonb)
			RETURNING id`,
			pii.Placeholder, phoneEnc, phoneIdx, displayEnc).Scan(&id)
		return id, err
	}
	if err != nil {
		return "", err
	}
	if displayName != "" {
		enc, _ := pii.Encrypt(displayName, key)
		_, _ = q.ExecContext(ctx, `
			UPDATE contact SET display_name_enc = $1, display_name = $2, updated_at = NOW()
			WHERE id = $3 AND (display_name_enc IS NULL OR TRIM(display_name_enc) = '')`,
			enc, pii.Placeholder, id)
	}
	return id, nil
}

func contactPhoneByID(ctx context.Context, q appdb.TenantQuerier, tenantSchema, id string) (string, error) {
	active, err := tenantschema.ContactPIIActive(ctx, q, tenantSchema)
	if err != nil || !active {
		var phone string
		err := q.QueryRowContext(ctx,
			`SELECT COALESCE(phone_number,'') FROM contact WHERE id = $1`, id).Scan(&phone)
		return phone, err
	}
	var phoneEnc, phoneLegacy sql.NullString
	err = q.QueryRowContext(ctx, `
		SELECT COALESCE(phone_number_enc,''), COALESCE(phone_number,'')
		FROM contact WHERE id = $1`, id).Scan(&phoneEnc, &phoneLegacy)
	if err != nil {
		return "", err
	}
	return pii.DecryptOrLegacy(phoneEnc.String, phoneLegacy.String, encKey())
}

func contactReturnCols() string {
	return `id,
		COALESCE(phone_number_enc, ''),
		COALESCE(phone_number, ''),
		COALESCE(display_name_enc, ''),
		COALESCE(display_name, ''),
		COALESCE(birth_date_enc, ''),
		birth_date::text,
		notes,
		COALESCE(status, 'active'),
		price_type_id::text,
		tags`
}

func nullableUUIDPtr(value *string) any {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil
	}
	return v
}

func buildConversationSearchCondition(q string, idx int, piiActive bool, args *[]any) string {
	q = strings.TrimSpace(q)
	parts := []string{fmt.Sprintf(`LOWER(COALESCE(c.last_message_preview,'')) LIKE $%d`, idx)}
	key := encKey()
	if tenantschema.UseBlindIndexSearch(key, piiActive) {
		phoneIdx := pii.BlindIndex(pii.NormalizePhone(q), key)
		if phoneIdx != "" {
			*args = append(*args, phoneIdx)
			parts = append(parts, fmt.Sprintf(`ct.phone_number_idx = $%d`, len(*args)))
		}
		nameIdx := pii.BlindIndex(pii.NormalizeName(q), key)
		if nameIdx != "" {
			*args = append(*args, nameIdx)
			parts = append(parts, fmt.Sprintf(`ct.display_name_idx = $%d`, len(*args)))
		}
	} else {
		parts = append(parts,
			fmt.Sprintf(`LOWER(ct.phone_number) LIKE $%d`, idx),
			fmt.Sprintf(`LOWER(COALESCE(ct.display_name,'')) LIKE $%d`, idx),
		)
	}
	like := "%" + strings.ToLower(q) + "%"
	*args = append(*args, like)
	return "(" + strings.Join(parts, " OR ") + ")"
}

func buildContactSearchWhere(q string, piiActive bool, args *[]any) string {
	key := encKey()
	where := "deleted_at IS NULL"
	if strings.TrimSpace(q) == "" {
		return where
	}
	q = strings.TrimSpace(q)
	likeIdx := len(*args) + 1
	*args = append(*args, "%"+q+"%")
	parts := []string{
		fmt.Sprintf(`COALESCE(notes, '') ILIKE $%d`, likeIdx),
		fmt.Sprintf(`COALESCE(status, '') ILIKE $%d`, likeIdx),
		fmt.Sprintf(`tags::text ILIKE $%d`, likeIdx),
	}
	if tenantschema.UseBlindIndexSearch(key, piiActive) {
		phoneIdx := pii.BlindIndex(pii.NormalizePhone(q), key)
		if phoneIdx != "" {
			idxParam := len(*args) + 1
			*args = append(*args, phoneIdx)
			parts = append(parts, fmt.Sprintf(`phone_number_idx = $%d`, idxParam))
		}
		nameIdx := pii.BlindIndex(pii.NormalizeName(q), key)
		if nameIdx != "" {
			idxParam := len(*args) + 1
			*args = append(*args, nameIdx)
			parts = append(parts, fmt.Sprintf(`display_name_idx = $%d`, idxParam))
		}
	} else {
		parts = append(parts,
			fmt.Sprintf(`phone_number ILIKE $%d`, likeIdx),
			fmt.Sprintf(`COALESCE(display_name, '') ILIKE $%d`, likeIdx),
		)
	}
	return where + " AND (" + strings.Join(parts, " OR ") + ")"
}
