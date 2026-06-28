package order

import "encoding/json"

const PaymentProofMaxRejections = 5

func ParsePaymentProofMeta(raw []byte) PaymentProofMeta {
	var meta PaymentProofMeta
	if len(raw) > 2 {
		_ = json.Unmarshal(raw, &meta)
	}
	return meta
}

func IsPaymentProofBlocked(meta PaymentProofMeta) bool {
	return meta.ProofBlocked || meta.RejectionCount >= PaymentProofMaxRejections
}

func IncrementPaymentRejection(meta PaymentProofMeta) PaymentProofMeta {
	meta.RejectionCount++
	if meta.RejectionCount >= PaymentProofMaxRejections {
		meta.ProofBlocked = true
	}
	return meta
}

func ResetPaymentProofBlock(meta PaymentProofMeta) PaymentProofMeta {
	meta.RejectionCount = 0
	meta.ProofBlocked = false
	meta.BlockedNotified = false
	return meta
}
