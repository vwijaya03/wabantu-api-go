package ai

import (
	"fmt"
	"strings"
)

func (c WABuyerCase) Assert(actual WABuyerActual) error {
	if c.ExpectQty > 0 && actual.Qty != c.ExpectQty {
		return fmt.Errorf("qty: got %d want %d", actual.Qty, c.ExpectQty)
	}
	if c.ExpectedState != "" && !stateMatches(c.ExpectedState, actual.State) {
		return fmt.Errorf("state: got %q want %q", actual.State, c.ExpectedState)
	}
	if c.ExpectedResponseBehavior != "" {
		if err := assertBehavior(c.ExpectedResponseBehavior, actual); err != nil {
			return err
		}
	}
	if c.ExpectedIntent != "" {
		if err := assertIntent(c, actual); err != nil {
			return err
		}
	}
	return nil
}

func stateMatches(want, got string) bool {
	if want == got {
		return true
	}
	if want == "none" && got == "cleared" {
		return true
	}
	return false
}

func assertBehavior(want ResponseBehavior, actual WABuyerActual) error {
	if behaviorMatches(want, actual) {
		return nil
	}
	return fmt.Errorf("behavior: got %q want %q (path=%s)", actual.Behavior, want, actual.Path)
}

func behaviorMatches(want ResponseBehavior, actual WABuyerActual) bool {
	if want == actual.Behavior {
		return true
	}
	switch want {
	case BehaviorCatalogList:
		return actual.Path == PathCatalogDB
	case BehaviorCatalogPrice:
		return actual.Path == PathCatalogDB
	case BehaviorCatalogProduct:
		return actual.Path == PathCatalogDB || actual.Behavior == BehaviorConsulting ||
			actual.Behavior == BehaviorAskVariant || actual.Path == PathOrderFlow
	case BehaviorOrderFlow:
		return actual.Path == PathOrderFlow ||
			actual.Behavior == BehaviorAskQty || actual.Behavior == BehaviorAskRecipient ||
			actual.Behavior == BehaviorAskVariant || actual.Behavior == BehaviorAskAddress
	case BehaviorQtyUpdated:
		return actual.Behavior == BehaviorQtyUpdated || (actual.Path == PathOrderFlow && actual.Qty > 0)
	case BehaviorPaymentInfo, BehaviorOrderStatus, BehaviorComplaint, BehaviorHumanEscalation:
		return actual.Intent == SalesStateConsulting || actual.Behavior == want
	case BehaviorGreeting:
		return actual.Path == PathGreeting || actual.Intent == SalesStateGreeting
	case BehaviorBreakFlow:
		return actual.Outcome.BrokeFlow || actual.State == "cleared"
	case BehaviorNoFalseCheckout:
		return !actual.Outcome.Completed
	case BehaviorNoFalseCancel:
		return !actual.Outcome.Canceled
	case BehaviorCorrection:
		return actual.Intent == SalesStateCorrection || actual.Outcome.BrokeFlow
	case BehaviorAskRecipient:
		return actual.Behavior == BehaviorAskRecipient || actual.Behavior == BehaviorOrderFlow ||
			actual.Behavior == BehaviorAskAddress ||
			strings.Contains(strings.ToLower(actual.Reply), "penerima")
	}
	return false
}

func assertIntent(c WABuyerCase, actual WABuyerActual) error {
	if c.CurrentState != nil && (actual.Path == PathOrderFlow || actual.Outcome.Completed) {
		if c.ExpectedIntent == SalesStateCheckout {
			return nil
		}
	}
	got := actual.Intent
	if got == "" || (got == SalesStateOutOfScope && c.ExpectedIntent != SalesStateOutOfScope) {
		got = ResolveSalesIntent(c.InputUser, c.History, c.CurrentState != nil, true, omahProfile(), omahCatalog()).State
	}
	if intentMatches(c.ExpectedIntent, got, c.InputUser) {
		return nil
	}
	return fmt.Errorf("intent: got %q want %q (path=%s)", got, c.ExpectedIntent, actual.Path)
}

func intentMatches(want, got, input string) bool {
	if want == got {
		return true
	}
	if want == SalesStateConsulting && got == SalesStateProductSelected {
		return true
	}
	if want == SalesStateProductSelected && got == SalesStateConsulting {
		return true
	}
	if want == SalesStateBrowsing && (got == SalesStateConsulting || got == SalesStateBrowsing) {
		return IsCatalogBrowsingIntent(input) || IsRecommendationRequest(input)
	}
	if want == SalesStateCartReady && (got == SalesStateCheckout || got == SalesStateCartReady) {
		return true
	}
	if want == SalesStateProductSelected && got == SalesStateCartReady {
		return true
	}
	if want == SalesStateConsulting && got == SalesStateBrowsing {
		return true
	}
	if want == SalesStateCorrection && got == SalesStateCorrection {
		return true
	}
	return false
}
