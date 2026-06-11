package events

import (
	"database/sql"
	"fmt"
	"strings"

	"encore.app/wabantu/shared/pii"
)

// SQL fragment: two columns for decryptPersonName scan order (enc, legacy).
const personNameEncLegacyCols = `COALESCE(full_name_enc,''), COALESCE(full_name,'')`

const personNameEncLegacyColsP = `COALESCE(p.full_name_enc,''), COALESCE(p.full_name,'')`

func scanPersonNameFromRow(enc, legacy string) (string, error) {
	return decryptPersonName(enc, legacy)
}

func scanPersonNameNull(enc, legacy sql.NullString) (string, error) {
	return decryptPersonName(enc.String, legacy.String)
}

func personNameSearchCondition(tableAlias string, paramIdx int, q string) (cond string, arg any) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", nil
	}
	col := "normalized_name"
	if tableAlias != "" {
		col = tableAlias + ".normalized_name"
	}
	idx := pii.BlindIndex(pii.NormalizeName(q), strings.TrimSpace(secrets.DataEncryptionKey))
	return fmt.Sprintf("%s = $%d", col, paramIdx), idx
}
