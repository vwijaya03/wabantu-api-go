package inventory

import "testing"

func TestRecommendCostingMethod(t *testing.T) {
	// Perishable (salmon) -> FIFO regardless of other flags.
	if r := recommendCostingMethod(WizardAnswers{Perishable: true, PriceVolatile: true}); r.Method != CostingFIFO {
		t.Fatalf("perishable -> %q, want fifo", r.Method)
	}
	// Batch tracking -> FIFO.
	if r := recommendCostingMethod(WizardAnswers{NeedBatchTracking: true}); r.Method != CostingFIFO {
		t.Fatalf("batch -> %q, want fifo", r.Method)
	}
	// High volume uniform, stable price -> Average.
	if r := recommendCostingMethod(WizardAnswers{HighVolumeUniform: true}); r.Method != CostingAverage {
		t.Fatalf("uniform -> %q, want average", r.Method)
	}
	// Volatile price -> Average.
	if r := recommendCostingMethod(WizardAnswers{PriceVolatile: true}); r.Method != CostingAverage {
		t.Fatalf("volatile -> %q, want average", r.Method)
	}
	// Nothing special -> Average default.
	if r := recommendCostingMethod(WizardAnswers{}); r.Method != CostingAverage {
		t.Fatalf("default -> %q, want average", r.Method)
	}
	// Reason is always populated.
	if recommendCostingMethod(WizardAnswers{Perishable: true}).Reason == "" {
		t.Fatal("reason should not be empty")
	}
}
