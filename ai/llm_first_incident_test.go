package ai

import (
	"strings"
	"testing"

	"encore.app/wabantu/internal/salesbrain"
)

// Texts of the 18 Oct production incident (tenant omah_apparel, gemini-3.1-flash-lite).
const (
	lfIncidentRecipientText = "Name: Anastasi\nPhone: 081222000111"
	lfIncidentAddressText   = "Jl. Taman Setiabudi II No. 28, South Jakarta, DKI Jakarta, 10197"
)

func lfIncidentAddressState() *orderState {
	st := lfTunaState()
	st.Step = lfStepAskAddressFull
	st.RecipientName, st.RecipientPhone = "Anastasi", "+6281222000111"
	return st
}

// The model labelled "DKI Jakarta" as the city and "South Jakarta" as the district and left the
// province empty; the deterministic parser must still complete the address.
func TestIncidentAddressMislabelledByModelStillCompletes(t *testing.T) {
	st := lfIncidentAddressState()
	f := &salesbrain.FieldData{
		Street: "Jl. Taman Setiabudi II No. 28", District: "South Jakarta", City: "DKI Jakarta", PostalCode: "10197",
	}
	problems := applyFieldAnswer(st, f, lfIncidentAddressText, salesbrain.LangEN)

	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if !st.ShippingComplete() {
		t.Fatalf("shipping must be complete: street=%q city=%q province=%q postal=%q", st.Street, st.City, st.Province, st.PostalCode)
	}
	if st.Street != "Jl. Taman Setiabudi II No. 28" || st.Province != "DKI Jakarta" || st.City != "Jakarta Selatan" || st.PostalCode != "10197" {
		t.Fatalf("wrong fields: %+v", *st)
	}
	if st.Step != lfStepConfirm {
		t.Fatalf("step = %q, want confirm", st.Step)
	}
}

// When the parser knows nothing, the model's mislabelled city is read as the province.
func TestIncidentModelCityThatIsAProvinceBecomesProvince(t *testing.T) {
	st := lfIncidentAddressState()
	text := "Jl. Melati 5 Blok C, Kebayoran Baru, DKI Jakarta 12130"
	f := &salesbrain.FieldData{Street: "Jl. Melati 5 Blok C", District: "Kebayoran Baru", City: "DKI Jakarta", PostalCode: "12130"}
	lfApplyModelAddress(st, f, text, lfParsedFields{})

	if st.Province != "DKI Jakarta" || st.City != "Kebayoran Baru" || st.Kecamatan != "" {
		t.Fatalf("province=%q city=%q kecamatan=%q", st.Province, st.City, st.Kecamatan)
	}
	// A district that is the city the parser already recognised is ignored.
	st = lfIncidentAddressState()
	st.City = "Jakarta Selatan"
	lfApplyModelAddress(st, &salesbrain.FieldData{District: "South Jakarta"}, lfIncidentAddressText, lfParsedFields{})
	if st.Kecamatan != "" {
		t.Fatalf("district that duplicates the city must be ignored, got %q", st.Kecamatan)
	}
}

func TestIncidentRecipientTextFillsNameAndPhone(t *testing.T) {
	st := lfTunaState()
	st.Step = lfStepAskRecipient
	applyFieldAnswer(st, &salesbrain.FieldData{Name: "Anastasi", Phone: "081222000111"}, lfIncidentRecipientText, salesbrain.LangEN)
	if st.RecipientName != "Anastasi" || st.RecipientPhone != "+6281222000111" || st.Step != lfStepAskAddressFull {
		t.Fatalf("got %+v", *st)
	}
	if strings.TrimSpace(st.Street) != "" {
		t.Fatalf("no street in the text: %q", st.Street)
	}
}
