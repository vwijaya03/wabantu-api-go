// Package proofcheck derives the owner-facing payment-proof check result from the OCR
// meta stored on an order (order.payment_proof_meta). Pure, so it is unit-testable.
package proofcheck

const (
	// Match: OCR amount equals the order total and the destination account matches.
	Match = "cocok"
	// NeedsReview: anything the owner must look at (mismatch, missing amount, duplicate, ...).
	NeedsReview = "perlu_dicek"
)

// informational flags do not affect the result (owner approval is still always required
// when the tenant's mode demands it; that is payment_status, not the proof check).
var informational = map[string]bool{"ai_review_required": true}

// Result returns "" when no proof was processed, else Match or NeedsReview, plus the
// reasons (the non-informational flags) for NeedsReview.
func Result(hasMeta bool, amount *float64, flags []string, proofBlocked bool) (string, []string) {
	if !hasMeta {
		return "", nil
	}
	var reasons []string
	for _, f := range flags {
		if !informational[f] {
			reasons = append(reasons, f)
		}
	}
	if amount == nil || *amount <= 0 {
		if !contains(reasons, "missing_amount") {
			reasons = append(reasons, "missing_amount")
		}
	}
	if proofBlocked && !contains(reasons, "proof_blocked") {
		reasons = append(reasons, "proof_blocked")
	}
	if len(reasons) == 0 {
		return Match, nil
	}
	return NeedsReview, reasons
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
