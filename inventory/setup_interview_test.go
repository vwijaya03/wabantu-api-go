package inventory

import "testing"

func TestMergeWizardAnswersUpdate(t *testing.T) {
	dst := WizardAnswers{ProductDescription: "Jual salmon frozen"}
	mergeWizardAnswersUpdate(&dst, wizardAnswersUpdate{
		BusinessType:       strPtrTest("food"),
		ProductDescription: strPtrTest("by kg per batch"),
		Perishable:         boolPtrTest(true),
		StockTurnover:      strPtrTest("fast"),
	})
	if dst.BusinessType != "food" {
		t.Fatalf("businessType: %q", dst.BusinessType)
	}
	if !dst.Perishable {
		t.Fatal("expected perishable")
	}
	if dst.StockTurnover != "fast" {
		t.Fatalf("stockTurnover: %q", dst.StockTurnover)
	}
	if dst.ProductDescription == "" {
		t.Fatal("expected merged product description")
	}
}

func TestWizardAnswersReady(t *testing.T) {
	if wizardAnswersReady(WizardAnswers{BusinessType: "food", ProductDescription: "short"}) {
		t.Fatal("expected not ready")
	}
	if !wizardAnswersReady(WizardAnswers{BusinessType: "food", ProductDescription: "Jual frozen food by kg dengan batch supplier"}) {
		t.Fatal("expected ready")
	}
}

func strPtrTest(s string) *string { return &s }
func boolPtrTest(b bool) *bool    { return &b }
