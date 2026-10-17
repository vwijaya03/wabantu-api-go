package salesbrain

import (
	"strings"
	"testing"
)

func omahInput() PromptInput {
	desc := "Premium cut of frozen yellow tail tuna steak, 100 grams per portion. Lean, firm, and rich in protein and omega-3. Sold frozen and vacuum-sealed to lock in freshness and premium quality. Perfect for sashimi."
	return PromptInput{
		Profile: ProfileFact{Name: "Omah Apparel", OpeningHours: "08:00 - 19:00 GMT +7", DeliveryArea: "Seluruh kota di Indonesia", Tone: "friendly"},
		Catalog: []CatalogFact{
			{No: 1, Name: "Australian Beef 500 Gram", Price: 450000, Unit: "pcs", Description: "Sold frozen and vacuum-sealed.", Code: "AUSTRALIAN_BEEF_500_GRAM"},
			{No: 2, Name: "Yellow Tail Tuna Portion 100 Gram", Price: 350000, Unit: "pcs", Description: desc, Code: "YELLOW_TAIL_TUNA_PORTION_100_GRAM"},
		},
		KB: []KBFact{{Question: "Kapan pesanan saya akan dikirim?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan dikirim di hari yang sama.", Category: "pengiriman"}},
		State: TurnState{
			Lang: LangEN, LangLocked: true, Greeting: "siang", Now: "Sabtu 17 Okt 2026 14:20 WIB",
			OpenOrders: []OrderSnapshot{{Ref: "WB-F4BFCE06", Status: StatusDraft, Stage: StageAddress, Subtotal: 425000,
				Lines: []OrderLine{{Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}, {Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}}},
			RemindAllowed: true, PendingField: "address", ResumeLine: "Back to your order: what's the delivery address (street, district, city, postal code)?",
		},
		History: []HistoryTurn{{Role: RoleBuyer, Text: "Hello"}, {Role: RoleShop, Text: "Hi! How can I help?"}},
		Message: "Is it frozen or freshly cut?",
	}
}

func TestBuildPromptStablePrefixHasFacts(t *testing.T) {
	sys, user := BuildPrompt(omahInput())
	for _, want := range []string{
		"Omah Apparel",
		"2. Yellow Tail Tuna Portion 100 Gram | Rp350.000/pcs",
		"Sold frozen and vacuum-sealed",
		"08:00 hingga 12:00 WIB",
		`"reply"`,
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	if strings.Contains(sys, "YELLOW_TAIL_TUNA_PORTION_100_GRAM") {
		t.Error("internal codes must never reach the prompt")
	}
	if strings.Contains(sys, "Is it frozen or freshly cut?") || strings.Contains(sys, "WB-F4BFCE06") {
		t.Error("dynamic state/message must not be in the cacheable system prefix")
	}
	for _, want := range []string{"bahasa: en (terkunci)", "WB-F4BFCE06", "boleh_ingatkan: ya", "kolom_tertunda: address", "Pembeli: Hello", "Toko: Hi! How can I help?", "Is it frozen or freshly cut?"} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
	if strings.Count(user, "Is it frozen or freshly cut?") != 1 {
		t.Error("current message must appear exactly once")
	}
}

func TestBuildPromptTruncatesDescriptions(t *testing.T) {
	sys, _ := BuildPrompt(omahInput())
	for _, line := range strings.Split(sys, "\n") {
		if strings.HasPrefix(line, "2. Yellow Tail") {
			parts := strings.Split(line, " | ")
			if desc := parts[len(parts)-1]; len([]rune(desc)) > 161 {
				t.Fatalf("description not truncated to 160 runes: %d", len([]rune(desc)))
			}
		}
	}
}

func TestBuildPromptKeepsOnlyLastSixHistoryTurns(t *testing.T) {
	in := omahInput()
	in.History = nil
	for i := 0; i < 10; i++ {
		in.History = append(in.History, HistoryTurn{Role: RoleBuyer, Text: "msg" + string(rune('A'+i))})
	}
	_, user := BuildPrompt(in)
	if strings.Contains(user, "msgD") || !strings.Contains(user, "msgE") || !strings.Contains(user, "msgJ") {
		t.Fatalf("history must keep the last %d turns, got:\n%s", MaxHistoryTurns, user)
	}
}

func TestChooseContextMode(t *testing.T) {
	small := omahInput()
	if ChooseContextMode(small.Catalog, small.KB) != "full" {
		t.Fatal("omah-sized tenant must use full context")
	}
	var big []CatalogFact
	for i := 0; i < 400; i++ {
		big = append(big, CatalogFact{No: i + 1, Name: "Product with a fairly long descriptive name", Price: 100000, Description: strings.Repeat("deskripsi produk ", 10)})
	}
	if ChooseContextMode(big, nil) != "topk" {
		t.Fatal("large catalog must switch to top-k")
	}
}

func TestBuildPromptDocumentsFieldsContract(t *testing.T) {
	sys, _ := BuildPrompt(omahInput())
	for _, want := range []string{
		`"fields": {`, `"postal_code"`, `"district"`,
		"fields hanya untuk intent field_answer; isi persis dari pesan pembeli, kosongkan yang tidak disebut, jangan mengarang",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
