package inventory

import (
	"context"
	"fmt"
)

// Document number prefixes (WABantu-style).
const (
	DocPO     = "WPO"
	DocBill   = "WBIL"
	DocInvoice = "WINV"
	DocReturn = "WRET"
)

// formatDocNumber renders a zero-padded document number, e.g. ("WPO", 42) -> "WPO-000042".
func formatDocNumber(prefix string, n int64) string {
	return fmt.Sprintf("%s-%06d", prefix, n)
}

// poStatusFromReceipts derives a purchase-order status from received vs ordered qty.
func poStatusFromReceipts(totalOrdered, totalReceived float64) string {
	if totalReceived <= epsilon {
		return "open"
	}
	if totalReceived+epsilon < totalOrdered {
		return "partial"
	}
	return "received"
}

// nextDocNumber atomically increments the per-tenant sequence and returns the
// formatted number. Must run inside the document-creation transaction.
func nextDocNumber(ctx context.Context, q querier, docType, prefix string) (string, error) {
	var n int64
	err := q.QueryRowContext(ctx, `
		INSERT INTO inv_document_sequence (doc_type, next_no)
		VALUES ($1, 1)
		ON CONFLICT (doc_type) DO UPDATE SET next_no = inv_document_sequence.next_no + 1
		RETURNING next_no`, docType).Scan(&n)
	if err != nil {
		return "", err
	}
	return formatDocNumber(prefix, n), nil
}
