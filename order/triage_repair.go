package order

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/triagerepair"
)

type TriageDraftRepairParams struct {
	TenantSchema   string      `json:"tenantSchema"`
	OrderID        string      `json:"orderId"`
	ConversationID string      `json:"conversationId"`
	ContactID      string      `json:"contactId"`
	WebSessionID   string      `json:"webSessionId"`
	Operation      string      `json:"operation"`
	Items          []OrderItem `json:"items"`
	ExpectedHash   string      `json:"expectedHash"`
	Apply          bool        `json:"apply"`
}

type TriageDraftRepairResult struct {
	Blocked      bool            `json:"blocked"`
	BlockReasons []string        `json:"blockReasons,omitempty"`
	BeforeHash   string          `json:"beforeHash"`
	AfterHash    string          `json:"afterHash"`
	BeforeJSON   json.RawMessage `json:"beforeJson"`
	AfterJSON    json.RawMessage `json:"afterJson"`
	UpdatedAt    string          `json:"updatedAt,omitempty"`
}

type triageOrderRow struct {
	ID                   string
	Status               string
	PaymentStatus        string
	ConversationID       string
	ContactID            string
	ItemsJSON            []byte
	PaymentTransactionID string
	PaymentProofMsgID    string
	TrackingNumber       string
	Deleted              bool
	UpdatedAt            string
}

// DryRunTriageDraftRepair previews a typed draft item mutation (private).
//
//encore:api private method=POST path=/api/v1/internal/order/triage-draft-repair/dry-run
func DryRunTriageDraftRepair(ctx context.Context, p *TriageDraftRepairParams) (*TriageDraftRepairResult, error) {
	return runTriageDraftRepair(ctx, p, false)
}

// ApplyTriageDraftRepair applies a previously dry-run hashed draft mutation (private).
//
//encore:api private method=POST path=/api/v1/internal/order/triage-draft-repair/apply
func ApplyTriageDraftRepair(ctx context.Context, p *TriageDraftRepairParams) (*TriageDraftRepairResult, error) {
	return runTriageDraftRepair(ctx, p, true)
}

func runTriageDraftRepair(ctx context.Context, p *TriageDraftRepairParams, apply bool) (*TriageDraftRepairResult, error) {
	if p == nil {
		return nil, appErrs.BadRequest("params required")
	}
	schema := strings.TrimSpace(p.TenantSchema)
	orderID := strings.TrimSpace(p.OrderID)
	if schema == "" || orderID == "" {
		return nil, appErrs.BadRequest("tenantSchema and orderId required")
	}
	if !triagerepair.ValidOperation(p.Operation) || (p.Operation != triagerepair.OpDraftItemsReplace && p.Operation != triagerepair.OpDraftItemsMerge) {
		return nil, appErrs.BadRequest("operation tidak diizinkan")
	}

	row, err := loadTriageOrderRow(ctx, schema, orderID)
	if err != nil {
		return nil, err
	}
	reasons := eligibilityReasons(row, p)
	beforeHash := hashJSON(row.ItemsJSON)
	res := &TriageDraftRepairResult{
		BeforeHash:   beforeHash,
		BeforeJSON:   row.ItemsJSON,
		BlockReasons: reasons,
		Blocked:      len(reasons) > 0,
		UpdatedAt:    row.UpdatedAt,
	}
	if len(reasons) > 0 {
		return res, nil
	}
	if p.ExpectedHash != "" && p.ExpectedHash != beforeHash {
		res.Blocked = true
		res.BlockReasons = []string{triagerepair.BlockStaleHash}
		return res, nil
	}

	normalized, err := normalizeOrderItems(ctx, schema, row.ContactID, p.Items)
	if err != nil {
		return nil, err
	}
	if p.Operation == triagerepair.OpDraftItemsMerge {
		var existing []OrderItem
		_ = json.Unmarshal(row.ItemsJSON, &existing)
		normalized = mergeOrderItems(existing, normalized)
	}
	after, _ := json.Marshal(normalized)
	res.AfterJSON = after
	res.AfterHash = hashJSON(after)
	if !apply {
		return res, nil
	}

	subtotal := 0.0
	for _, it := range normalized {
		subtotal += it.Qty * it.UnitPrice
	}
	_, err = db.Exec(ctx, fmt.Sprintf(`
		UPDATE "%s"."order"
		SET items = $2::jsonb, subtotal = $3, total = $3 + shipping_cost, updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL AND status = 'draft' AND payment_status = 'unpaid'`, schema),
		orderID, string(after), subtotal)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func loadTriageOrderRow(ctx context.Context, schema, orderID string) (triageOrderRow, error) {
	var r triageOrderRow
	var deletedAt sql.NullTime
	err := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT id::text, status, payment_status,
		       COALESCE(conversation_id::text, ''), COALESCE(contact_id::text, ''),
		       items, COALESCE(payment_transaction_id::text, ''),
		       COALESCE(payment_proof_message_id::text, ''), COALESCE(tracking_number, ''),
		       deleted_at, updated_at::text
		FROM "%s"."order" WHERE id = $1::uuid`, schema), orderID).Scan(
		&r.ID, &r.Status, &r.PaymentStatus, &r.ConversationID, &r.ContactID, &r.ItemsJSON,
		&r.PaymentTransactionID, &r.PaymentProofMsgID, &r.TrackingNumber, &deletedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return r, appErrs.NotFound("order tidak ditemukan")
	}
	if err != nil {
		return r, err
	}
	r.Deleted = deletedAt.Valid
	return r, nil
}

func eligibilityReasons(row triageOrderRow, p *TriageDraftRepairParams) []string {
	var out []string
	if row.Deleted {
		out = append(out, triagerepair.BlockNotDraftUnpaid)
	}
	if row.Status != "draft" || row.PaymentStatus != "unpaid" {
		out = append(out, triagerepair.BlockNotDraftUnpaid)
	}
	switch row.Status {
	case "processing", "confirmed", "paid", "shipped", "completed", "cancelled":
		out = append(out, triagerepair.BlockStockSensitive)
	}
	if row.PaymentTransactionID != "" || row.PaymentProofMsgID != "" {
		out = append(out, triagerepair.BlockPaymentInFlight)
	}
	if row.TrackingNumber != "" {
		out = append(out, triagerepair.BlockStockSensitive)
	}
	conv := strings.TrimSpace(p.ConversationID)
	if conv != "" && row.ConversationID != "" && row.ConversationID != conv {
		out = append(out, triagerepair.BlockIdentityMismatch)
	}
	if strings.TrimSpace(p.WebSessionID) != "" {
		out = append(out, triagerepair.BlockIdentityMismatch)
	}
	if strings.TrimSpace(p.OrderID) == "" {
		out = append(out, triagerepair.BlockCartNotPersisted)
	}
	return out
}

func mergeOrderItems(existing, incoming []OrderItem) []OrderItem {
	out := append([]OrderItem{}, existing...)
	for _, in := range incoming {
		found := false
		for i, ex := range out {
			if ex.CatalogItemID != "" && ex.CatalogItemID == in.CatalogItemID {
				out[i].Qty += in.Qty
				found = true
				break
			}
		}
		if !found {
			out = append(out, in)
		}
	}
	return out
}

func hashJSON(raw []byte) string {
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
