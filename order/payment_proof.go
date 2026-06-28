package order

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"encore.app/wabantu/inventory"
	appErrs "encore.app/wabantu/shared/errs"
)

type RejectPaymentProofRequest struct {
	Reason string `json:"reason,omitempty"`
}

//encore:api auth method=POST path=/api/v1/orders/:id/payment-proof/verify
func VerifyPaymentProof(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}

	var currentStatus, paymentStatus string
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT status, payment_status FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id).Scan(&currentStatus, &paymentStatus)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	if paymentStatus == "verified" {
		return nil, appErrs.BadRequest("payment already verified")
	}
	if paymentStatus != "proof_submitted" && paymentStatus != "rejected" && paymentStatus != "unpaid" {
		return nil, appErrs.BadRequest("order tidak memiliki bukti transfer yang bisa diverifikasi")
	}

	newOrderStatus := currentStatus
	if currentStatus == "draft" || currentStatus == "confirmed" || currentStatus == "paid" {
		newOrderStatus = "processing"
		checkItems, loadErr := loadOrderItems(ctx, u.TenantSchema, id)
		if loadErr != nil {
			return nil, loadErr
		}
		if err := inventory.PrecheckOrderStock(ctx, u.TenantSchema, id, orderStockItems(checkItems)); err != nil {
			return nil, err
		}
	}

	row := db.QueryRow(ctx, fmt.Sprintf(
		`UPDATE "%s"."order" SET
			payment_status = 'verified',
			status = $2,
			payment_proof_verified_at = NOW(),
			payment_proof_verified_by = $3,
			updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING %s`,
		u.TenantSchema, orderSelectCols("")), id, newOrderStatus, u.AccountID)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("verify payment proof: %w", err)
	}

	if newOrderStatus == "processing" && newOrderStatus != currentStatus {
		if err := inventory.SyncOrderStock(ctx, u.TenantSchema, o.ID, o.Status, orderStockItems(o.Items), u.AccountID); err != nil {
			return nil, err
		}
	}
	if o.ConversationID != "" {
		ref := FormatOrderNumber(o.ID)
		_, _ = db.Exec(ctx, fmt.Sprintf(`
			INSERT INTO "%s".message (conversation_id, direction, author, type, body, metadata, status)
			VALUES ($1, 'out', 'system', 'text', $2, '{}'::jsonb, 'sent')`,
			u.TenantSchema), o.ConversationID,
			fmt.Sprintf("Pembayaran pesanan %s terverifikasi. Pesanan sedang diproses.", ref))
	}
	return &o, nil
}

//encore:api auth method=POST path=/api/v1/orders/:id/payment-proof/reject
func RejectPaymentProof(ctx context.Context, id string, req *RejectPaymentProofRequest) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}

	var paymentStatus string
	var metaRaw []byte
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT payment_status, COALESCE(payment_proof_meta, '{}') FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id).Scan(&paymentStatus, &metaRaw)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	if paymentStatus == "verified" {
		return nil, appErrs.BadRequest("payment already verified")
	}

	meta := ParsePaymentProofMeta(metaRaw)
	if req != nil {
		meta.RejectReason = strings.TrimSpace(req.Reason)
	}
	meta = IncrementPaymentRejection(meta)
	metaJSON, _ := json.Marshal(meta)

	row := db.QueryRow(ctx, fmt.Sprintf(
		`UPDATE "%s"."order" SET
			payment_status = 'rejected',
			payment_proof_meta = $2,
			updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING %s`,
		u.TenantSchema, orderSelectCols("")), id, metaJSON)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("reject payment proof: %w", err)
	}
	return &o, nil
}

//encore:api auth method=POST path=/api/v1/orders/:id/payment-proof/unblock
func UnblockPaymentProof(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}

	var paymentStatus, conversationID string
	var metaRaw []byte
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT payment_status, COALESCE(payment_proof_meta, '{}'), COALESCE(conversation_id::text, '')
		 FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`,
		u.TenantSchema), id).Scan(&paymentStatus, &metaRaw, &conversationID)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	if paymentStatus == "verified" {
		return nil, appErrs.BadRequest("payment already verified")
	}

	meta := ParsePaymentProofMeta(metaRaw)
	if !IsPaymentProofBlocked(meta) {
		return nil, appErrs.BadRequest("pesanan tidak dalam status batas bukti transfer")
	}
	meta = ResetPaymentProofBlock(meta)
	metaJSON, _ := json.Marshal(meta)

	row := db.QueryRow(ctx, fmt.Sprintf(
		`UPDATE "%s"."order" SET
			payment_proof_meta = $2,
			updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING %s`,
		u.TenantSchema, orderSelectCols("")), id, metaJSON)

	o, err := scanOrder(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("unblock payment proof: %w", err)
	}

	if conversationID != "" {
		ref := FormatOrderNumber(o.ID)
		msg := fmt.Sprintf(
			"Admin sudah membuka batas upload bukti untuk pesanan %s. Silakan kirim bukti transfer yang benar ya kak.",
			ref,
		)
		_ = sendPaymentProofConversationMessage(ctx, u.TenantSchema, conversationID, msg)
	}
	return &o, nil
}
