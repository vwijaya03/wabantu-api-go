package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	"encore.app/wabantu/finance"
	"encore.app/wabantu/internal/orderapprove"
	"encore.app/wabantu/inventory"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/shared/types"
)

// Approve verifies the payment proof AND completes the order (incl. finance income) in ONE
// database transaction. Owner only (CanPerformOwnerActions); the order is looked up in the
// session tenant's schema, so another tenant's order id is 404. Idempotent: an already
// completed order returns 200 without writing (no duplicate income). An order without a
// payment proof → 409. The Firestore mirror runs after commit. Contract: docs/API_AI_CONFIG.md §4.
//
//encore:api auth method=POST path=/api/v1/orders/:id/approve
func Approve(ctx context.Context, id string) (*Order, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}
	if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
		return nil, err
	}

	// Fail fast on overselling before the transaction (same check as verify / PATCH completed).
	// Skipped for already-completed orders so an idempotent retry never fails on stock.
	if cur, lerr := loadOrderByID(ctx, u.TenantSchema, id); lerr == nil && cur.Status != "completed" {
		if err := inventory.PrecheckOrderStock(ctx, u.TenantSchema, id, orderStockItems(cur.Items)); err != nil {
			return nil, err
		}
	}

	tx, err := db.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		return nil, appErrs.Internal("begin transaction failed")
	}
	st := &approveTxStore{ctx: ctx, tx: tx, u: u, id: id}
	res, err := orderapprove.Run(ctx, st)
	if err != nil {
		var ae *orderapprove.Error
		switch {
		case errors.Is(err, orderapprove.ErrNotFound):
			return nil, appErrs.NotFound("order not found")
		case errors.As(err, &ae):
			return nil, &errs.Error{Code: errs.Aborted, Message: ae.Message} // HTTP 409
		}
		return nil, err
	}

	o, err := loadOrderByID(ctx, u.TenantSchema, id)
	if err != nil {
		return nil, err
	}
	// Stock after commit (inventory has no tx API), at most once per order via the
	// stock_applied_at claim; on failure the claim is released so a retry applies it.
	ss := &approveStockStore{u: u, o: o}
	if _, err := orderapprove.ApplyStockOnce(ctx, ss); err != nil {
		rlog.Error("approve: stock apply after commit failed (claim released; retry /approve)", "orderId", o.ID, "err", err)
		return nil, err
	}
	if res.Decision == orderapprove.Proceed {
		rlog.Info("order approved", "orderId", o.ID, "actor", u.AccountID, "prevStatus", res.Prev.Status, "prevPayment", res.Prev.PaymentStatus)
	}
	mirrorOrderState(ctx, u, o.ConversationID)
	return o, nil
}

func loadOrderByID(ctx context.Context, schema, id string) (*Order, error) {
	o, err := scanOrder(db.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
		`SELECT %s FROM "%s"."order" WHERE id=$1 AND deleted_at IS NULL`, orderSelectCols(""), schema), id).Scan)
	if err != nil {
		return nil, appErrs.NotFound("order not found")
	}
	return &o, nil
}

// approveTxStore implements orderapprove.Store on one *sql.Tx.
type approveTxStore struct {
	ctx   context.Context
	tx    *sql.Tx
	u     *types.AuthUser
	id    string
	order Order
}

func (s *approveTxStore) LockOrder(ctx context.Context) (orderapprove.State, bool, error) {
	var st orderapprove.State
	var meta []byte
	err := s.tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT status, payment_status, COALESCE(payment_proof_message_id::text, ''), COALESCE(payment_proof_meta, '{}'::jsonb)
		FROM "%s"."order" WHERE id::text = $1 AND deleted_at IS NULL FOR UPDATE`, s.u.TenantSchema), s.id).
		Scan(&st.Status, &st.PaymentStatus, &st.ProofMessageID, &meta)
	if err == sql.ErrNoRows {
		return st, false, nil
	}
	if err != nil {
		return st, false, fmt.Errorf("lock order: %w", err)
	}
	var pm PaymentProofMeta
	applyPaymentProofMetaTo(&pm, meta, &st)
	return st, true, nil
}

func applyPaymentProofMetaTo(pm *PaymentProofMeta, raw []byte, st *orderapprove.State) {
	if len(raw) <= 2 {
		return
	}
	if err := json.Unmarshal(raw, pm); err == nil {
		st.HasProofMeta = pm.Amount != nil || pm.Bank != "" || pm.AccountNumber != "" || pm.FileHash != ""
		st.ProofBlocked = pm.ProofBlocked
	}
}

func (s *approveTxStore) CompleteVerified(ctx context.Context) error {
	o, err := scanOrder(s.tx.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order" SET
			payment_status = 'verified',
			payment_proof_verified_at = COALESCE(payment_proof_verified_at, NOW()),
			payment_proof_verified_by = COALESCE(payment_proof_verified_by, $2),
			status = 'completed',
			updated_at = NOW()
		WHERE id::text = $1 AND deleted_at IS NULL
		RETURNING %s`, s.u.TenantSchema, orderSelectCols("")), s.id, s.u.AccountID).Scan)
	if err != nil {
		return fmt.Errorf("approve order: %w", err)
	}
	s.order = o
	return nil
}

func (s *approveTxStore) RecordIncome(ctx context.Context) error {
	return finance.RecordOrderCompletedIncomeTx(ctx, s.tx, s.u.TenantSchema, s.u.AccountID, s.order.ID, s.order.Total, s.order.IncomeWalletID)
}

func (s *approveTxStore) NotifyBuyer(ctx context.Context) error {
	if s.order.ConversationID == "" {
		return nil
	}
	_, err := s.tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO "%s".message (conversation_id, direction, author, type, body, metadata, status)
		VALUES ($1, 'out', 'system', 'text', $2, '{}'::jsonb, 'sent')`, s.u.TenantSchema),
		s.order.ConversationID,
		fmt.Sprintf("Pembayaran pesanan %s terverifikasi dan pesanan selesai. Terima kasih!", FormatOrderNumber(s.order.ID)))
	return err
}

func (s *approveTxStore) Commit() error   { return s.tx.Commit() }
func (s *approveTxStore) Rollback() error { return s.tx.Rollback() }

// approveStockStore implements orderapprove.StockStore on order.stock_applied_at.
type approveStockStore struct {
	u *types.AuthUser
	o *Order
}

func (s *approveStockStore) hasMarker(ctx context.Context) bool {
	ok, err := tenantschema.ColumnExists(ctx, db.Stdlib(), s.u.TenantSchema, "order", "stock_applied_at")
	return err == nil && ok
}

func (s *approveStockStore) ClaimStock(ctx context.Context) (string, bool, error) {
	if !s.hasMarker(ctx) {
		// Migration not applied yet: fall back to the row-locked reconcile in SyncOrderStock,
		// which is itself safe against double deduction.
		return "", true, nil
	}
	var token string
	err := db.Stdlib().QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order" SET stock_applied_at = now()
		WHERE id::text = $1 AND stock_applied_at IS NULL AND deleted_at IS NULL
		RETURNING stock_applied_at::text`, s.u.TenantSchema), s.o.ID).Scan(&token)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("claim stock marker: %w", err)
	}
	return token, true, nil
}

func (s *approveStockStore) ApplyStock(ctx context.Context) error {
	return inventory.SyncOrderStock(ctx, s.u.TenantSchema, s.o.ID, s.o.Status, orderStockItems(s.o.Items), s.u.AccountID)
}

func (s *approveStockStore) ReleaseStock(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := db.Stdlib().ExecContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order" SET stock_applied_at = NULL
		WHERE id::text = $1 AND stock_applied_at::text = $2`, s.u.TenantSchema), s.o.ID, token)
	return err
}
