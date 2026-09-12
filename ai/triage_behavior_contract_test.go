package ai

import "testing"

func TestValidateBehaviorContractRejectsSQL(t *testing.T) {
	c := BehaviorContract{Lane: "buyerflow", Channel: "whatsapp", Assertions: BehaviorAssertions{WantPath: "order_flow", ReplyContains: []string{"DROP TABLE order"}}}
	if err := ValidateBehaviorContract(c); err == nil {
		t.Fatal("SQL in assertion must fail")
	}
}

func TestValidateBehaviorContractRejectsMixed(t *testing.T) {
	c := BehaviorContract{Lane: "mixed", Channel: "whatsapp", Assertions: BehaviorAssertions{WantPath: "order_flow"}}
	if err := ValidateBehaviorContract(c); err == nil {
		t.Fatal("mixed lane must be split")
	}
}

func TestHasDeterministicInvariant(t *testing.T) {
	if HasDeterministicInvariant(BehaviorContract{Lane: "grounded_content"}) {
		t.Fatal("empty grounded contract is not deterministic")
	}
	if !HasDeterministicInvariant(BehaviorContract{Lane: "buyerflow", Assertions: BehaviorAssertions{WantPath: "order_flow", CartInclude: []CartAssertion{{NameContains: "durian", Qty: 1}}}}) {
		t.Fatal("buyerflow cart include is deterministic")
	}
	if HasDeterministicInvariant(BehaviorContract{Lane: "buyerflow", Assertions: BehaviorAssertions{NeedCustomerInput: true, WantPath: "order_flow"}}) {
		t.Fatal("needs customer input must not run Composer")
	}
}
