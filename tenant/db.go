package tenant

import "encore.dev/storage/sqldb"

// DB is the system-level database shared by tenant, audit, and flag services.
var DB = sqldb.NewDatabase("tenant", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})
