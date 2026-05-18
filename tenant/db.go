package tenant

import "encore.dev/storage/sqldb"

// DataDB is the tenant-data database (Nest jb_tenant): one schema per tenant (t_<slug>).
var DataDB = sqldb.NewDatabase("tenant", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})
