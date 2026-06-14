package inventory

import (
	"regexp"
	"strings"
)

// CostingMethod values supported tenant-wide and per-SKU.
const (
	CostingFIFO    = "fifo"
	CostingLIFO    = "lifo"
	CostingAverage = "average"
)

var validCostingMethods = map[string]bool{
	CostingFIFO:    true,
	CostingLIFO:    true,
	CostingAverage: true,
}

// normalizeCostingMethod lowercases/trims and validates a costing method.
// Returns (method, true) when valid; ("", false) otherwise.
func normalizeCostingMethod(s string) (string, bool) {
	m := strings.ToLower(strings.TrimSpace(s))
	if m == "" || !validCostingMethods[m] {
		return "", false
	}
	return m, true
}

// effectiveCostingMethod resolves the method actually used for a SKU:
// the per-SKU override when valid, otherwise the tenant default, otherwise average.
func effectiveCostingMethod(skuOverride, tenantDefault string) string {
	if m, ok := normalizeCostingMethod(skuOverride); ok {
		return m
	}
	if m, ok := normalizeCostingMethod(tenantDefault); ok {
		return m
	}
	return CostingAverage
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeWarehouseCode derives a stable slug code from a display name.
// Falls back to "gudang" when the name has no alphanumeric content.
func normalizeWarehouseCode(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "gudang"
	}
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}
