package buyerflow

import (
	"strings"
	"testing"
)

func TestParseRecipientLineEnglishLabels(t *testing.T) {
	name, phone := parseRecipientLine("Name: Anastasia\nPhone: 98822123")
	if name != "Anastasia" {
		t.Fatalf("name = %q", name)
	}
	if phone != "+6298822123" {
		t.Fatalf("phone = %q", phone)
	}

	name, phone = parseRecipientLine("name: Budi Santoso\nphone: 0812 3456 7890")
	if name != "Budi Santoso" || phone != "+6281234567890" {
		t.Fatalf("got %q / %q", name, phone)
	}
}

func TestExtractRecipientPhone(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"081234567890", "+6281234567890"},
		{"+6281234567890", "+6281234567890"},
		{"+62 812-3456-7890", "+6281234567890"},
		{"98822123", "+6298822123"},
		{"812345678", "+62812345678"},
		{"", ""},
		{"1234", ""},
		{"12345678901", ""},
		{"call me maybe", ""},
		{"0812345678901234567890", ""},
		{strings.Repeat("9", 500), ""},
	}
	for _, tc := range cases {
		if got := extractRecipientPhone(tc.in); got != tc.want {
			t.Errorf("extractRecipientPhone(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMergeShippingTextEnglishLabels(t *testing.T) {
	st := &OrderState{Step: "ask_recipient"}
	mergeShippingText(st, "Name: Anastasia\nPhone: 98822123")
	if st.RecipientName != "Anastasia" {
		t.Fatalf("RecipientName = %q", st.RecipientName)
	}
	if st.RecipientPhone != "+6298822123" {
		t.Fatalf("RecipientPhone = %q", st.RecipientPhone)
	}
}

func TestMergeShippingTextIndonesianLabelsBareNumber(t *testing.T) {
	st := &OrderState{Step: "ask_recipient"}
	mergeShippingText(st, "Nama: Budi\nHP: 081234567890")
	if st.RecipientName != "Budi" || st.RecipientPhone != "+6281234567890" {
		t.Fatalf("got %q / %q", st.RecipientName, st.RecipientPhone)
	}
}

func TestParseRecipientHintFromMessageEnglishInline(t *testing.T) {
	name, phone := parseRecipientHintFromMessage("Name: Anastasia\nPhone: 081234567890")
	if name != "Anastasia" || phone != "+6281234567890" {
		t.Fatalf("got %q / %q", name, phone)
	}
	if !hasRecipientHintInMessage("Name: Anastasia\nPhone: 081234567890") {
		t.Fatal("expected recipient hint")
	}
}

// Baris "Phone: 0812…" saat checkout tidak boleh memicu handoff manusia.
func TestEnglishRecipientLinesNotHumanHandoff(t *testing.T) {
	for _, msg := range []string{
		"Name: Anastasia\nPhone: 081234567890",
		"Phone: 98822123",
		"Name: Anastasia",
	} {
		if IsHumanEscalationRequest(msg) {
			t.Fatalf("unexpected human handoff for %q", msg)
		}
	}
}

func askRecipientEnglishSim(withName bool) *Simulator {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = LangEN
	s.Order = &OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "salmon-100",
		ProductName:   "Salmon Steak 100 Gram",
		Qty:           1,
		UnitPrice:     75000,
		Items: []OrderLineState{{
			CatalogItemID: "salmon-100",
			ProductName:   "Salmon Steak 100 Gram",
			Qty:           1,
			UnitPrice:     75000,
		}},
	}
	if withName {
		s.Order.RecipientName = "Anastasia"
	}
	syncOrderStateFromItems(s.Order)
	return s
}

func TestAskRecipientEnglishLabelsAdvancesToAddress(t *testing.T) {
	s := askRecipientEnglishSim(false)
	out := s.Turn("Name: Anastasia\nPhone: 98822123")
	if out.Order == nil {
		t.Fatalf("expected order state, path=%s", out.Path)
	}
	if out.Order.RecipientName != "Anastasia" || out.Order.RecipientPhone != "+6298822123" {
		t.Fatalf("recipient = %q / %q", out.Order.RecipientName, out.Order.RecipientPhone)
	}
	if strings.Contains(out.Reply, "Example:\nName: Budi") {
		t.Fatalf("must not repeat full AskRecipient template:\n%s", out.Reply)
	}
}

func TestAskRecipientClarifiesPhoneWhenNameOnly(t *testing.T) {
	s := askRecipientEnglishSim(false)
	out := s.Turn("Name: Anastasia\nPhone: abc")
	if out.Order == nil || out.Order.RecipientName != "Anastasia" || out.Order.RecipientPhone != "" {
		t.Fatalf("order = %+v", out.Order)
	}
	low := strings.ToLower(out.Reply)
	if !strings.Contains(out.Reply, "Anastasia") || !strings.Contains(low, "08") || !strings.Contains(out.Reply, "+62") {
		t.Fatalf("expected short phone clarification, got:\n%s", out.Reply)
	}
	if strings.Contains(out.Reply, "Example:\nName: Budi") {
		t.Fatalf("clarify must not be the full AskRecipient template:\n%s", out.Reply)
	}
}

func TestRecipientClarifyPhoneReplyIndonesian(t *testing.T) {
	pop := PushSavedBuyerLang(LangID)
	defer pop()
	reply := recipientClarifyPhoneReply(OrderState{RecipientName: "Budi"}, "Nama: Budi", false)
	if !strings.Contains(reply, "Budi") || !strings.Contains(reply, "08") || !strings.Contains(reply, "+62") {
		t.Fatalf("reply = %q", reply)
	}
}

func TestAskRecipientNoNameStillFullTemplate(t *testing.T) {
	s := askRecipientEnglishSim(false)
	out := s.Turn("ok 123")
	if !strings.Contains(out.Reply, "recipient") {
		t.Fatalf("expected full ask-recipient template, got:\n%s", out.Reply)
	}
}
