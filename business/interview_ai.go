package business

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"encore.app/wabantu/ai"
)

const setupInterviewSystemPrompt = `Kamu adalah asisten setup WABantu yang mewawancarai OWNER toko (bukan pembeli).

Tujuan: lengkapi profil bisnis lalu buat draft FAQ kebijakan toko untuk AI WhatsApp.

Aturan wawancara:
- Bahasa Indonesia, ramah, satu pertanyaan fokus per giliran
- Fase "profile": produk/jasa, deskripsi, area kirim, jam buka, cara bayar (TANPA nomor rekening)
- Fase "faq": pengiriman, pembayaran, retur, minimum order, reseller, dll.
- JANGAN tanya/minta harga per produk, stok SKU, password, token, nomor rekening lengkap
- JANGAN mengarang — hanya catat jawaban owner
- Jika owner bilang "cukup"/"lanjut review", set ready_for_review true

Output HARUS JSON valid saja (tanpa markdown), schema:
{
  "assistant_message": "teks balasan ke owner",
  "phase": "profile|faq|review",
  "profile_updates": {
    "businessName": "...",
    "description": "...",
    "productsServices": "...",
    "deliveryArea": "...",
    "openingHours": "...",
    "basePricing": "..."
  },
  "tone": "friendly|formal|casual",
  "faq_add": [{"question":"...","answer":"...","category":"pengiriman|pembayaran|retur|order|umum"}],
  "ready_for_review": false
}

Hanya sertakan field profile_updates yang baru terisi dari jawaban terakhir.
faq_add: maks 3 entri baru per giliran; jangan duplikat pertanyaan yang sudah ada di draft.
Jangan tulis harga produk spesifik (Rp...) di FAQ — arahkan ke katalog.`

var (
	faqPricePattern  = regexp.MustCompile(`(?i)Rp\s*[\d.,]+`)
	faqBankPattern   = regexp.MustCompile(`(?i)\b(rekening|no\.?\s*rek|account\s*number)\b`)
	faqInstagramOnly = regexp.MustCompile(`(?i)(cek|lihat|follow)\s+(ig|instagram|@)`)
)

type interviewFAQDraft struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Category string `json:"category,omitempty"`
	Include  bool   `json:"include"`
}

type aiInterviewTurn struct {
	AssistantMessage string           `json:"assistant_message"`
	Phase            string           `json:"phase"`
	ProfileUpdates   *ImportFieldSet  `json:"profile_updates,omitempty"`
	Tone             *string          `json:"tone,omitempty"`
	FAQAdd           []interviewFAQDraft `json:"faq_add,omitempty"`
	ReadyForReview   bool             `json:"ready_for_review"`
}

func extractJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if j := strings.LastIndex(raw, "}"); j >= 0 {
		raw = raw[:j+1]
	}
	return raw
}

func parseInterviewTurn(raw string) (aiInterviewTurn, error) {
	raw = extractJSONObject(raw)
	if raw == "" {
		return aiInterviewTurn{}, fmt.Errorf("empty AI response")
	}
	var turn aiInterviewTurn
	if err := json.Unmarshal([]byte(raw), &turn); err != nil {
		return aiInterviewTurn{}, err
	}
	turn.AssistantMessage = strings.TrimSpace(turn.AssistantMessage)
	if turn.AssistantMessage == "" {
		return aiInterviewTurn{}, fmt.Errorf("missing assistant_message")
	}
	turn.Phase = normalizeInterviewPhase(turn.Phase)
	return turn, nil
}

func normalizeInterviewPhase(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "faq", "review", "done":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "profile"
	}
}

func validateFAQDraft(q, a string) error {
	q = strings.TrimSpace(q)
	a = strings.TrimSpace(a)
	if len(q) < 5 || len(a) < 5 {
		return fmt.Errorf("pertanyaan/jawaban terlalu pendek")
	}
	if len(q) > 500 {
		return fmt.Errorf("pertanyaan terlalu panjang")
	}
	if len(a) > 4000 {
		return fmt.Errorf("jawaban terlalu panjang")
	}
	if faqPricePattern.MatchString(a) {
		return fmt.Errorf("jawaban FAQ tidak boleh berisi harga produk spesifik")
	}
	if faqBankPattern.MatchString(a) {
		return fmt.Errorf("jawaban FAQ tidak boleh berisi nomor rekening")
	}
	if faqInstagramOnly.MatchString(a) && !strings.Contains(strings.ToLower(a), "katalog") {
		return fmt.Errorf("jangan jadikan Instagram sumber utama di FAQ")
	}
	return nil
}

func mergeProfileDraft(dst *ImportFieldSet, src *ImportFieldSet) {
	if dst == nil || src == nil {
		return
	}
	setStr := func(target **string, val *string) {
		if val == nil {
			return
		}
		v := strings.TrimSpace(*val)
		if v == "" {
			return
		}
		*target = &v
	}
	setStr(&dst.BusinessName, src.BusinessName)
	setStr(&dst.Description, src.Description)
	setStr(&dst.Address, src.Address)
	setStr(&dst.OpeningHours, src.OpeningHours)
	setStr(&dst.ProductsServices, src.ProductsServices)
	setStr(&dst.BasePricing, src.BasePricing)
	setStr(&dst.DeliveryArea, src.DeliveryArea)
}

func profileDraftFromResponse(p ProfileResponse) ImportFieldSet {
	out := ImportFieldSet{
		BusinessName: strPtr(p.BusinessName),
	}
	if p.Description != nil {
		out.Description = p.Description
	}
	if p.Address != nil {
		out.Address = p.Address
	}
	if p.OpeningHours != nil {
		out.OpeningHours = p.OpeningHours
	}
	if p.ProductsServices != nil {
		out.ProductsServices = p.ProductsServices
	}
	if p.BasePricing != nil {
		out.BasePricing = p.BasePricing
	}
	if p.DeliveryArea != nil {
		out.DeliveryArea = p.DeliveryArea
	}
	return out
}

func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func profileFieldsComplete(d ImportFieldSet) bool {
	hasName := d.BusinessName != nil && strings.TrimSpace(*d.BusinessName) != ""
	hasProducts := d.ProductsServices != nil && strings.TrimSpace(*d.ProductsServices) != ""
	hasDelivery := d.DeliveryArea != nil && strings.TrimSpace(*d.DeliveryArea) != ""
	return hasName && hasProducts && hasDelivery
}

func buildInterviewUserPrompt(session *setupInterviewSession, userMessage string) string {
	var b strings.Builder
	b.WriteString("Profil saat ini (draft):\n")
	b.WriteString(formatProfileDraftForPrompt(session.ProfileDraft))
	b.WriteString("\n\nFase session: ")
	b.WriteString(session.Phase)
	b.WriteString("\nGiliran chat: ")
	b.WriteString(fmt.Sprintf("%d/%d", session.TurnCount, setupInterviewMaxTurns))
	if len(session.FAQDrafts) > 0 {
		b.WriteString("\n\nDraft FAQ yang sudah ada:\n")
		for i, f := range session.FAQDrafts {
			b.WriteString(fmt.Sprintf("%d. Q: %s\n   A: %s\n", i+1, f.Question, f.Answer))
		}
	}
	b.WriteString("\n\nRiwayat percakapan:\n")
	for _, m := range session.Messages {
		role := "Owner"
		if m.Role == "assistant" {
			role = "Asisten"
		}
		b.WriteString(role + ": " + m.Content + "\n")
	}
	b.WriteString("\nPesan owner terbaru: ")
	b.WriteString(userMessage)
	b.WriteString("\n\nBalas dengan JSON sesuai schema.")
	return b.String()
}

func formatProfileDraftForPrompt(d ImportFieldSet) string {
	line := func(label string, v *string) string {
		if v == nil || strings.TrimSpace(*v) == "" {
			return label + ": (belum)"
		}
		return label + ": " + *v
	}
	return strings.Join([]string{
		line("Nama bisnis", d.BusinessName),
		line("Produk/jasa", d.ProductsServices),
		line("Deskripsi", d.Description),
		line("Area kirim", d.DeliveryArea),
		line("Jam buka", d.OpeningHours),
		line("Pembayaran", d.BasePricing),
	}, "\n")
}

func initialSetupMessage(p ProfileResponse) string {
	if !profileFieldsComplete(profileDraftFromResponse(p)) {
		return "Halo! Saya bantu lengkapi profil toko dan FAQ untuk AI WhatsApp.\n\nKita mulai ya — toko Anda jual produk atau jasa apa? Ceritakan singkat saja."
	}
	return "Profil dasar sudah ada. Sekarang kita susun FAQ.\n\nPelanggan biasanya bayar lewat apa? (transfer, COD, QRIS, atau kombinasi?)"
}

func resolveInterviewModel() string {
	return ai.DefaultHaikuAPIID()
}
