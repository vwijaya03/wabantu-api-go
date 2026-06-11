package tenantschema

import "sync"

var (
	contactPIICache sync.Map // schema string -> bool
	leadPIICache    sync.Map // schema string -> bool
)

// MarkContactPIIActive records that a tenant schema has contact PII columns.
func MarkContactPIIActive(schema string) {
	if schema != "" {
		contactPIICache.Store(schema, true)
	}
}

// MarkLeadPIIActive records that a tenant schema has lead PII columns.
func MarkLeadPIIActive(schema string) {
	if schema != "" {
		leadPIICache.Store(schema, true)
	}
}

// InvalidateContactPIICache clears cached contact PII state (after DDL).
func InvalidateContactPIICache(schema string) {
	if schema != "" {
		contactPIICache.Delete(schema)
	}
}

// InvalidateLeadPIICache clears cached lead PII state (after DDL).
func InvalidateLeadPIICache(schema string) {
	if schema != "" {
		leadPIICache.Delete(schema)
	}
}

func cachedContactPII(schema string) (bool, bool) {
	if schema == "" {
		return false, false
	}
	v, ok := contactPIICache.Load(schema)
	if !ok {
		return false, false
	}
	return v.(bool), true
}

func cachedLeadPII(schema string) (bool, bool) {
	if schema == "" {
		return false, false
	}
	v, ok := leadPIICache.Load(schema)
	if !ok {
		return false, false
	}
	return v.(bool), true
}

func storeContactPII(schema string, active bool) {
	if schema != "" {
		contactPIICache.Store(schema, active)
	}
}

func storeLeadPII(schema string, active bool) {
	if schema != "" {
		leadPIICache.Store(schema, active)
	}
}
