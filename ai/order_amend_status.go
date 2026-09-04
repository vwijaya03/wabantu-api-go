package ai

import "strings"

// isOrderAmendBlockedStatus — shipped/completed/cancelled tidak bisa diubah lewat chat.
func isOrderAmendBlockedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "shipped", "completed", "cancelled":
		return true
	default:
		return false
	}
}

func isOrderDraftAmendable(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "draft")
}

func orderAmendBlockedStatusReply(formal bool, status, ref string) string {
	label := orderStatusLabelID(status)
	if formal {
		return "Maaf kak, pesanan " + ref + " sudah " + label + " dan tidak bisa diubah lewat chat. Silakan buat pesanan baru jika diperlukan."
	}
	return "Maaf kak, pesanan " + ref + " sudah " + label + " jadi belum bisa diubah lewat chat 🙏 Mau order baru?"
}

func orderAmendPickDraftReply(orders []persistedOrder) string {
	intro := "Dari chat ini ada beberapa pesanan draft aktif:"
	if len(orders) == 1 {
		intro = "Dari chat ini masih ada pesanan draft aktif:"
	}
	return formatOrderPickListReply(
		intro,
		orders,
		"Sebut nomor pesanan yang mau dilanjutkan (contoh WB-A1B2C3D4), atau ketik 'pesanan baru' untuk mulai order baru.",
	)
}
