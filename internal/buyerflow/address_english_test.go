package buyerflow

import (
	"strings"
	"testing"
)

func TestAddressEnglishFormsComplete(t *testing.T) {
	cases := []struct{ in, street, city, prov, postal string }{
		{"Street Setiabudi XII No. 8, South Jakarta, DKI Jakarta, 10197", "Street Setiabudi XII No. 8", "Jakarta Selatan", "DKI Jakarta", "10197"},
		{"street: Street Setiabudi XII No. 8\ncity: south jakarta\nprovince: DKI Jakarta\npostal code: 10197\n\nis it enough ?", "Street Setiabudi XII No. 8", "south jakarta", "DKI Jakarta", "10197"},
		{"Jl. Teuku Umar No 5, Denpasar, Bali, 80114", "Jl. Teuku Umar No 5", "Denpasar", "Bali", "80114"},
		{"Villa Kalijudan Indah A9, Surabaya, East Java, 60177", "Villa Kalijudan Indah A9", "Surabaya", "Jawa Timur", "60177"},
		{"Address: Jl Melati 12\nCity: Malang\nProvince: Jawa Timur\nZip: 65145", "Jl Melati 12", "Malang", "Jawa Timur", "65145"},
	}
	for _, c := range cases {
		st := OrderState{Step: "ask_address_full", RecipientName: "Anastasia", RecipientPhone: "+6281200300500"}
		mergeShippingText(&st, c.in)
		if st.Street != c.street || st.City != c.city || st.Province != c.prov || st.PostalCode != c.postal || !st.ShippingComplete() {
			t.Errorf("%q\n got street=%q city=%q prov=%q postal=%q complete=%v", c.in, st.Street, st.City, st.Province, st.PostalCode, st.ShippingComplete())
		}
	}
}

func TestAddressMissingReplyNamesOnlyMissingParts(t *testing.T) {
	en := englishDefaultOrderTemplates(false)
	got := addressMissingReply(OrderState{Street: "Jl Melati 12", Province: "Jawa Timur", PostalCode: "65145"}, en)
	if got != "Thanks! To complete the address I still need the city." {
		t.Fatalf("EN got %q", got)
	}
	id := defaultOrderTemplates(false)
	got = addressMissingReply(OrderState{Street: "Jl Melati 12", City: "Malang", Province: "Jawa Timur"}, id)
	if got != "Makasih kak, alamatnya tinggal kurang kode pos 5 digit ya." {
		t.Fatalf("ID got %q", got)
	}
}

// Replays the 17 Oct 23:14–23:17 WIB production thread (tenant omah_apparel).
func TestEnglishCheckoutAddressThreadCompletes(t *testing.T) {
	for _, addr := range []string{
		"Street Setiabudi XII No. 8, South Jakarta, DKI Jakarta, 10197",
		"street: Street Setiabudi XII No. 8\ncity: south jakarta\nprovince: DKI Jakarta\npostal code: 10197\n\nis it enough ?",
	} {
		s := newOmahSteakSimulator()
		s.SavedBuyerLang = LangEN
		s.Turn("Okay, can i order 1 tuna steak please, and 1 australian beef 500gram ?")
		s.Turn("okay i will continue my order.\n\nName: Anastasia\nPhone: 081200300500")
		out := s.Turn(addr)
		if out.Reply == "" || strings.Contains(out.Reply, "isn't complete") || strings.Contains(out.Reply, "still need") {
			t.Fatalf("address %q not accepted; reply=%q order=%+v", addr, out.Reply, s.Order)
		}
	}
}
