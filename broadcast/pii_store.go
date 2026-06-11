package broadcast

import (
	"context"
	"strings"

	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
)

var secrets struct {
	DataEncryptionKey string
}

func encKey() string {
	return strings.TrimSpace(secrets.DataEncryptionKey)
}

func encryptBroadcastPhone(phone string) (enc, idx, store string, err error) {
	phone = normalizePhone(phone)
	if phone == "" {
		return "", "", "", nil
	}
	key := encKey()
	if err := pii.ValidateKey(key); err != nil {
		return "", "", phone, nil
	}
	enc, err = pii.Encrypt(phone, key)
	if err != nil {
		return "", "", "", err
	}
	idx = pii.BlindIndex(pii.NormalizePhone(phone), key)
	return enc, idx, pii.Placeholder, nil
}

func decryptBroadcastPhone(enc, legacy string) (string, error) {
	return pii.DecryptOrLegacy(enc, legacy, encKey())
}

func broadcastPIIActive(ctx context.Context, schema string) bool {
	active, err := tenantschema.TableColumnExists(ctx, db.Stdlib(), schema, "broadcast_recipient", "phone_number_idx")
	return err == nil && active
}
