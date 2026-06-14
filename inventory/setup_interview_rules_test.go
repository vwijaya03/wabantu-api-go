package inventory

import "testing"

func TestInferWizardAnswersFromMessage_frozenQuickReply(t *testing.T) {
	upd := inferWizardAnswersFromMessage("Jual frozen food, stok cepat keluar")
	if upd.BusinessType == nil || *upd.BusinessType != "food" {
		t.Fatalf("businessType: %v", upd.BusinessType)
	}
	if upd.StockTurnover == nil || *upd.StockTurnover != "fast" {
		t.Fatalf("stockTurnover: %v", upd.StockTurnover)
	}
	if upd.Perishable == nil || !*upd.Perishable {
		t.Fatal("expected perishable")
	}
}

func TestCompleteInvSetupInterviewTurnRules_readyAfterEnoughInfo(t *testing.T) {
	session := &invSetupInterviewSession{
		Phase: "intro",
		Messages: []invSetupMessage{
			{Role: "assistant", Content: "Halo"},
		},
	}
	turn := completeInvSetupInterviewTurnRules(session, "Jual frozen food by kg, batch supplier, stok cepat keluar tiap minggu, harga beli naik turun")
	if !turn.ReadyForRecommendation {
		t.Fatal("expected ready for recommendation")
	}
	if turn.AssistantMessage == "" {
		t.Fatal("expected assistant message")
	}
}
