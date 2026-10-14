package order

import "encoding/json"

const (
	pathOrderPaymentNotify = "order_payment_notify"
	reasonNonQuestion        = "non_question"
)

// systemMessageMeta mirrors AI reply metadata shape (path, reason, llmUsed only).
type systemMessageMeta struct {
	Reason  string `json:"reason"`
	Path    string `json:"path"`
	LLMUsed bool   `json:"llmUsed"`
}

func orderPaymentNotifyMetadataJSON() []byte {
	b, err := json.Marshal(systemMessageMeta{
		Reason:  reasonNonQuestion,
		Path:    pathOrderPaymentNotify,
		LLMUsed: false,
	})
	if err != nil {
		return []byte(`{"reason":"non_question","path":"order_payment_notify","llmUsed":false}`)
	}
	return b
}
