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
	args := []any{like}
	nextIdx := idx + 1
	if refPrefix := OrderRefUUIDPrefix(q); refPrefix != "" {
		parts = append(parts, fmt.Sprintf(`UPPER(REPLACE(o.id::text, '-', '')) LIKE $%d`, nextIdx))
		args = append(args, refPrefix+"%")
		nextIdx++
	}
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	if tenantschema.UseBlindIndexSearch(key, piiActive) {
		phoneIdx := pii.BlindIndex(pii.NormalizePhone(q), key)
		nameIdx := pii.BlindIndex(pii.NormalizeName(q), key)
		parts = append(parts,
			fmt.Sprintf(`c.phone_number_idx = $%d`, nextIdx),
			fmt.Sprintf(`c.display_name_idx = $%d`, nextIdx+1),
		)
		return "(" + strings.Join(parts, " OR ") + ")", append(args, phoneIdx, nameIdx)
	}
	parts = append(parts,
		fmt.Sprintf(`COALESCE(c.display_name, '') ILIKE $%d`, idx),
		fmt.Sprintf(`COALESCE(c.phone_number, '') ILIKE $%d`, idx),
	)
	return "(" + strings.Join(parts, " OR ") + ")", args
}
