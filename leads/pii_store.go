package leads

import (
	"context"
	"database/sql"
	"strings"

	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
)

var secrets struct {
	DataEncryptionKey string
}

func leadEncKey() string {
	return strings.TrimSpace(secrets.DataEncryptionKey)
}

func encryptLeadPhone(phone string) (enc, idx string, err error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", "", nil
	}
	key := leadEncKey()
	enc, err = pii.Encrypt(phone, key)
	if err != nil {
		return "", "", err
	}
	return enc, pii.BlindIndex(pii.NormalizePhone(phone), key), nil
}

func encryptLeadName(name string) (enc, idx string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", nil
	}
	key := leadEncKey()
	enc, err = pii.Encrypt(name, key)
	if err != nil {
		return "", "", err
	}
	return enc, pii.BlindIndex(pii.NormalizeName(name), key), nil
}

func decryptLeadPhone(enc, legacy string) (string, error) {
	return pii.DecryptOrLegacy(enc, legacy, leadEncKey())
}

func decryptLeadName(enc, legacy string) (string, error) {
	return pii.DecryptOrLegacy(enc, legacy, leadEncKey())
}

func scanLeadPII(scanner interface {
	Scan(dest ...any) error
}) (Lead, error) {
	var l Lead
	var phoneEnc, phoneLegacy, nameEnc, nameLegacy sql.NullString
	err := scanner.Scan(
		&l.ID, &phoneEnc, &phoneLegacy, &nameEnc, &nameLegacy,
		&l.ProductInterest, &l.Budget, &l.Location,
		&l.Status, &l.Notes, &l.CreatedAt,
	)
	if err != nil {
		return l, err
	}
	phone, err := decryptLeadPhone(phoneEnc.String, phoneLegacy.String)
	if err != nil {
		return l, err
	}
	l.PhoneNumber = phone
	name, err := decryptLeadName(nameEnc.String, nameLegacy.String)
	if err != nil {
		return l, err
	}
	if name != "" && name != pii.Placeholder {
		l.Name = &name
	}
	return l, nil
}

func leadPhoneFromContact(ctx context.Context, conn *sql.DB, contactID string) (string, error) {
	active, err := tenantschema.ContactPIIActiveDB(ctx, conn, "")
	if err != nil || !active {
		var phone string
		err := conn.QueryRowContext(ctx,
			`SELECT COALESCE(phone_number,'') FROM contact WHERE id = $1`, contactID).Scan(&phone)
		return phone, err
	}
	var phoneEnc, phoneLegacy sql.NullString
	err = conn.QueryRowContext(ctx, `
		SELECT COALESCE(phone_number_enc,''), COALESCE(phone_number,'')
		FROM contact WHERE id = $1`, contactID).Scan(&phoneEnc, &phoneLegacy)
	if err != nil {
		return "", err
	}
	return decryptLeadPhone(phoneEnc.String, phoneLegacy.String)
}
