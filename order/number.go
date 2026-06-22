package order

import "strings"

// FormatOrderNumber — nomor pesanan singkat untuk pembeli (WB-A1B2C3D4 dari UUID).
func FormatOrderNumber(orderID string) string {
	id := strings.ReplaceAll(strings.TrimSpace(orderID), "-", "")
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return "WB-" + strings.ToUpper(id)
}

// OrderRefUUIDPrefix extracts hex prefix from WB- order ref search (case-insensitive).
// Returns empty when q is not an order ref pattern.
func OrderRefUUIDPrefix(q string) string {
	q = strings.TrimSpace(q)
	upper := strings.ToUpper(q)
	if !strings.HasPrefix(upper, "WB-") {
		return ""
	}
	prefix := strings.ReplaceAll(strings.TrimSpace(upper[3:]), "-", "")
	if prefix == "" {
		return ""
	}
	for _, c := range prefix {
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return prefix
}
