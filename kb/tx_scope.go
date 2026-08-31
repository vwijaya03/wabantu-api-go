package kb

import (
	"database/sql"

	appdb "encore.app/wabantu/shared/db"
)

// txn returns a TenantScope that qualifies SQL on the open transaction.
func txn(ts appdb.TenantScope, tx *sql.Tx) appdb.TenantScope {
	return ts.WithQ(tx)
}
