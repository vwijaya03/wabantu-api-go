package inventory

import (
	"strings"
)

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func hasOperationalSignal(a WizardAnswers) bool {
	return a.StockTurnover != "" || a.PriceTrend != "" ||
		a.Perishable || a.UsesExpiryDates || a.NeedBatchTracking ||
		a.HighVolumeUniform || a.PriceVolatile || a.SeasonalStock
}

func inferWizardAnswersFromMessage(text string) wizardAnswersUpdate {
	low := strings.ToLower(strings.TrimSpace(text))
	upd := wizardAnswersUpdate{}

	switch {
	case containsAny(low, "frozen", "makanan", "minuman", "kuliner", "food", "bakery", "kue"):
		bt := "food"
		upd.BusinessType = &bt
	case containsAny(low, "fashion", "baju", "pakaian", "apparel", "kaos", "distro"):
		bt := "fashion"
		upd.BusinessType = &bt
	case containsAny(low, "produksi", "manufaktur", "produksi sendiri"):
		bt := "manufacturing"
		upd.BusinessType = &bt
	case containsAny(low, "jasa", "service"):
		bt := "services"
		upd.BusinessType = &bt
	case containsAny(low, "retail", "toko", "sembako", "warung", "grosir"):
		bt := "retail"
		upd.BusinessType = &bt
	}

	trimmed := strings.TrimSpace(text)
	if len(trimmed) >= 8 {
		upd.ProductDescription = &trimmed
	}

	switch {
	case containsAny(low, "cepat", "harian", "mingguan", "putar cepat"):
		st := "fast"
		upd.StockTurnover = &st
	case containsAny(low, "lambat", "bulanan", "jarang keluar"):
		st := "slow"
		upd.StockTurnover = &st
	case containsAny(low, "sedang", "2 minggu", "3 minggu", "2-4 minggu"):
		st := "medium"
		upd.StockTurnover = &st
	}

	switch {
	case containsAny(low, "naik turun", "naik-turun", "fluktuat", "volatil", "berubah-ubah"):
		pt := "volatile"
		upd.PriceTrend = &pt
	case containsAny(low, "cenderung naik", "harga naik", "naik terus"):
		pt := "rising"
		upd.PriceTrend = &pt
	case containsAny(low, "stabil", "jarang berubah"):
		pt := "stable"
		upd.PriceTrend = &pt
	}

	if containsAny(low, "frozen", "basi", "perishable", "expiry", "kedaluwarsa", "kadaluarsa") {
		t := true
		upd.Perishable = &t
	}
	if containsAny(low, "expiry", "kedaluwarsa", "kadaluarsa", "tanggal") {
		t := true
		upd.UsesExpiryDates = &t
	}
	if containsAny(low, "batch", "lot", "nomor seri", "serial") {
		t := true
		upd.NeedBatchTracking = &t
	}
	if containsAny(low, "musiman", "ramadan", "natal", "lebaran", "musim") {
		t := true
		upd.SeasonalStock = &t
	}
	if containsAny(low, "naik turun", "fluktuat", "volatil", "berubah-ubah") {
		t := true
		upd.PriceVolatile = &t
	}
	if containsAny(low, "volume tinggi", "banyak sku", "seragam", "ribuan") {
		t := true
		upd.HighVolumeUniform = &t
	}

	return upd
}

func completeInvSetupInterviewTurnRules(session *invSetupInterviewSession, latestUser string) invSetupInterviewTurn {
	upd := inferWizardAnswersFromMessage(latestUser)
	draft := session.AnswersDraft
	mergeWizardAnswersUpdate(&draft, upd)

	turn := invSetupInterviewTurn{
		AnswersUpdate: upd,
		Phase:         session.Phase,
	}

	low := strings.ToLower(strings.TrimSpace(latestUser))
	if containsAny(low, "cukup", "lanjut rekomendasi", "lanjut") && wizardAnswersReady(draft) {
		turn.Phase = "ready"
		turn.ReadyForRecommendation = true
		turn.AssistantMessage = "Baik! Data sudah cukup. Klik «Lanjut ke rekomendasi HPP» untuk melihat saran metode FIFO, LIFO, atau Average."
		return turn
	}

	if draft.BusinessType == "" {
		turn.Phase = "products"
		turn.AssistantMessage = "Terima kasih! Toko Anda lebih ke kategori apa — makanan/frozen, fashion, retail umum, produksi, atau lainnya?"
		return turn
	}

	if len(strings.TrimSpace(draft.ProductDescription)) < 20 {
		turn.Phase = "products"
		turn.AssistantMessage = "Bisa ceritakan sedikit lebih detail produk yang dijual dan pola stoknya? (misalnya masuk per batch, keluar harian/mingguan, atau ada barang mudah basi)"
		return turn
	}

	if !hasOperationalSignal(draft) {
		turn.Phase = "operations"
		turn.AssistantMessage = "Stok biasanya cepat atau lambat keluar? Harga beli dari supplier stabil, naik, atau sering naik-turun? Ada batch/expiry atau stok musiman?"
		return turn
	}

	turn.Phase = "ready"
	turn.ReadyForRecommendation = true
	turn.AssistantMessage = "Terima kasih, profil bisnis & pola stok sudah cukup jelas. Klik «Lanjut ke rekomendasi HPP» jika siap melanjutkan."
	return turn
}
