package business

import "testing"

func TestValidateFAQDraftRejectsPrice(t *testing.T) {
	err := validateFAQDraft("Berapa harga jeans?", "Jeans highwaist Rp199000 per pcs")
	if err == nil {
		t.Fatal("expected price rejection")
	}
}

func TestValidateFAQDraftAcceptsPolicy(t *testing.T) {
	err := validateFAQDraft("Kirim ke luar Jawa?", "Bisa, ongkir dihitung setelah alamat lengkap.")
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
}

func TestParseInterviewTurnJSON(t *testing.T) {
	raw := `{"assistant_message":"Siap, noted.","phase":"faq","ready_for_review":false}`
	turn, err := parseInterviewTurn(raw)
	if err != nil || turn.Phase != "faq" {
		t.Fatalf("parse failed: %+v err=%v", turn, err)
	}
}

func TestParseInterviewTurnStripsMarkdown(t *testing.T) {
	raw := "```json\n{\"assistant_message\":\"Halo\",\"phase\":\"profile\"}\n```"
	turn, err := parseInterviewTurn(raw)
	if err != nil || turn.AssistantMessage != "Halo" {
		t.Fatalf("got %+v err=%v", turn, err)
	}
}

func TestProfileFieldsComplete(t *testing.T) {
	d := ImportFieldSet{
		BusinessName:     strPtr("Toko A"),
		ProductsServices: strPtr("Pakaian"),
		DeliveryArea:     strPtr("Seluruh Indonesia"),
	}
	if !profileFieldsComplete(d) {
		t.Fatal("expected complete")
	}
}

func TestMergeProfileDraft(t *testing.T) {
	dst := ImportFieldSet{BusinessName: strPtr("Lama")}
	src := ImportFieldSet{Description: strPtr("Toko fashion")}
	mergeProfileDraft(&dst, &src)
	if dst.Description == nil || *dst.Description != "Toko fashion" {
		t.Fatal("merge failed")
	}
	if dst.BusinessName == nil || *dst.BusinessName != "Lama" {
		t.Fatal("should keep existing name")
	}
}
