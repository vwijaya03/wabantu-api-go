package buyerflow

import "testing"

func TestHumanEscalationPhrasesEN(t *testing.T) {
	for _, s := range []string{"Contact your administrator", "can i speak with the owner", "i want to talk to a person", "let me speak to the owner", "can i talk to a real person", "I want to complain to the manager", "hubungkan ke pemilik toko", "mau ngobrol sama orang"} {
		if !IsHumanEscalationRequest(s) {
			t.Errorf("IsHumanEscalationRequest(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"who is the owner of this shop?", "Name: Budi\nPhone: 081234567890", "is the tuna frozen?", "ongkir ke surabaya berapa?"} {
		if IsHumanEscalationRequest(s) {
			t.Errorf("IsHumanEscalationRequest(%q) = true, want false", s)
		}
	}
}

// "person / human / orang / seller" are ordinary nouns (headcounts, products): a generic verb such
// as mau/bisa or a "please/dong" tail is not enough to hand the chat to staff.
func TestHumanEscalationGenericNounsNeedATalkVerb(t *testing.T) {
	for _, s := range []string{
		"porsi ini bisa untuk 3 orang?", "mau pesan untuk 5 orang", "mau beli human hair please",
		"ready human hair dong", "is this enough for one person please", "bisa kirim ke orang rumah?",
		"mau beli dari seller lain",
	} {
		if IsHumanEscalationRequest(s) {
			t.Errorf("IsHumanEscalationRequest(%q) = true, want false", s)
		}
	}
	for _, s := range []string{
		"bisa hubungi seller?", "i want to speak to the seller", "mau bicara sama penjual",
		"can I talk to a human", "tolong hubungkan ke orang", "how can i reach the owner",
	} {
		if !IsHumanEscalationRequest(s) {
			t.Errorf("IsHumanEscalationRequest(%q) = false, want true", s)
		}
	}
}
