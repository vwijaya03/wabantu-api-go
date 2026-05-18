// Package system owns the control-plane Postgres database (Nest jb_system).
package system

import "encore.dev/storage/sqldb"

// DB is the system / control-plane database: tenant, tenant_account, audit_log, etc.
var DB = sqldb.NewDatabase("system", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})
