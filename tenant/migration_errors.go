package tenant

import "errors"

// errSchemaMigrationBusy is returned when another worker holds the per-schema advisory lock.
var errSchemaMigrationBusy = errors.New("tenant schema migration already in progress")
