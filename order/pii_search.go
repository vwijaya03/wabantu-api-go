package order

import (
	"fmt"
	"strings"

	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
)

var secrets struct {
	DataEncryptionKey string
}

func orderContactSearchSQL(idx int, q string, piiActive bool) (fragment string, extraArgs []any) {
	q = strings.TrimSpace(q)
	like := "%" + q + "%"
	parts := []string{
		fmt.Sprintf(`o.id::text ILIKE $%d`, idx),
		fmt.Sprintf(`COALESCE(o.notes, '') ILIKE $%d`, idx),
		fmt.Sprintf(`COALESCE(o.tracking_number, '') ILIKE $%d`, idx),
		fmt.Sprintf(`COALESCE(o.courier, '') ILIKE $%d`, idx),
		fmt.Sprintf(`o.items::text ILIKE $%d`, idx),
	}
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	if tenantschema.UseBlindIndexSearch(key, piiActive) {
		phoneIdx := pii.BlindIndex(pii.NormalizePhone(q), key)
		nameIdx := pii.BlindIndex(pii.NormalizeName(q), key)
		parts = append(parts,
			fmt.Sprintf(`c.phone_number_idx = $%d`, idx+1),
			fmt.Sprintf(`c.display_name_idx = $%d`, idx+2),
		)
		return "(" + strings.Join(parts, " OR ") + ")", []any{like, phoneIdx, nameIdx}
	}
	parts = append(parts,
		fmt.Sprintf(`COALESCE(c.display_name, '') ILIKE $%d`, idx),
		fmt.Sprintf(`COALESCE(c.phone_number, '') ILIKE $%d`, idx),
	)
	return "(" + strings.Join(parts, " OR ") + ")", []any{like}
}
