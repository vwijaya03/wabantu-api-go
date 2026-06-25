package ai

import (
	"strings"
	"testing"
)

func TestIsRecipientPolicyQuestion(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"mau beli atas nama orang lain bisa ya ?", true},
		{"boleh pesan buat orang lain?", true},
		{"bisa kirim untuk orang lain ga?", true},
		{"pembeli dengan nama Lavana Snack ada ?", false},
		{"pembeli atas nama saya ada ?", false},
		{"pembeli atas nama ini ada? Nama: supriyanto", false},
		{"saya masih punya pesanan aktif nggak ?", false},
		{"mau beli abon sapi 1 pcs", false},
		{"boleh beli 1 pcs ?", false},
	}
	for _, tc := range cases {
		if got := IsRecipientPolicyQuestion(tc.text); got != tc.want {
			t.Fatalf("IsRecipientPolicyQuestion(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsConsultingPurchaseQuestion_excludesRecipientPolicy(t *testing.T) {
	msg := "mau beli atas nama orang lain bisa ya ?"
	if IsConsultingPurchaseQuestion(msg, omahCatalog()) {
		t.Fatalf("consulting purchase should be false for recipient policy: %q", msg)
	}
}

func TestReplyFromBusinessCatalog_recipientPolicyNotAbon(t *testing.T) {
	profile := omahProfile()
	catalog := omahCatalog()
	history := []dbMessage{
		{Direction: "out", Body: "Ini katalog Omah Apparel ya kak:\n\n• Abon Sapi 500G\nRp35000/pcs"},
	}
	msg := "mau beli atas nama orang lain bisa ya ?"
	if _, ok := replyFromBusinessCatalog(msg, profile, catalog, history); ok {
		t.Fatal("replyFromBusinessCatalog should not handle recipient policy question")
	}
}

func TestRecipientPolicy_transcriptTurn(t *testing.T) {
	sim := newOmahSimulator()
	msgs := []string{
		"pagi",
		"disini jual apa aja ya ?",
		"mau beli atas nama orang lain bisa ya ?",
	}
	outcomes := sim.RunScript(msgs...)
	last := outcomes[len(outcomes)-1]
	if last.Path != PathRecipientPolicy {
		t.Fatalf("path = %q, want %q", last.Path, PathRecipientPolicy)
	}
	low := strings.ToLower(last.Reply)
	if strings.Contains(low, "abon sapi") {
		t.Fatalf("reply should not mention Abon Sapi: %q", last.Reply)
	}
	if !strings.Contains(low, "orang lain") && !strings.Contains(low, "penerima") {
		t.Fatalf("reply should mention recipient policy: %q", last.Reply)
	}
}

func TestResolveSalesIntent_recipientPolicy(t *testing.T) {
	msg := "mau beli atas nama orang lain bisa ya ?"
	intent := ResolveSalesIntent(msg, nil, false, true, omahProfile(), omahCatalog())
	if intent.Topic != SalesTopicRecipient {
		t.Fatalf("topic = %q, want %q", intent.Topic, SalesTopicRecipient)
	}
}

func TestReplyRecipientPolicyQuestion_usesFAQ(t *testing.T) {
	kb := []dbKBEntry{{
		Question: "mau beli atas nama orang lain bisa ya",
		Answer:   "Tentu, isi data penerima saat checkout.",
	}}
	got := replyRecipientPolicyQuestion("mau beli atas nama orang lain bisa ya ?", kb, false)
	if got != "Tentu, isi data penerima saat checkout." {
		t.Fatalf("FAQ answer = %q", got)
	}
}
