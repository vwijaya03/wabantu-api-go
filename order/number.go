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
