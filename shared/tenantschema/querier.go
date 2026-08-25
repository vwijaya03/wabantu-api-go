package tenantschema

import (
	"context"
	"database/sql"
	"fmt"

	appdb "encore.app/wabantu/shared/db"
)

// Querier runs tenant queries with optional 08P01 retry on pool/conn.
type Querier = appdb.TenantQuerier

// Q coerces *sql.DB, *sql.Conn, *sql.Tx, or TenantQuerier for schema checks.
func Q(q any) appdb.TenantQuerier {
	switch v := q.(type) {
	case appdb.TenantQuerier:
		return appdb.CoerceTenantQuerier(v)
	case *sql.DB:
		return appdb.AsTenantQuerier(v)
	case *sql.Conn:
		return appdb.AsTenantQuerier(v)
	case *sql.Tx:
		return appdb.AsTenantQuerier(v)
	default:
		panic(fmt.Sprintf("tenantschema: unsupported querier %T", q))
	}
}

func scanExists(ctx context.Context, q Querier, query string, args ...any) (bool, error) {
	var exists bool
	if err := q.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
