package inventory

import (
	"context"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

//encore:api auth method=DELETE path=/api/v1/inventory/bills/:id
func DeleteBill(ctx context.Context, id string) error {
	u, err := getUser()
	if err != nil {
		return err
	}
	if err := requireOwner(u); err != nil {
		return err
	}
	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer conn.Close()

	bill, err := getBill(ctx, conn, id)
	if err != nil {
		return err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, bill.TransactionDate); err != nil {
		return err
	}
	if err := finance.RemoveInventoryEntry(ctx, u.TenantSchema, id); err != nil {
		return err
	}

	movs, err := collectMovementsByRef(ctx, conn, "bill", id)
	if err != nil {
		return err
	}
	for _, m := range movs {
		if err := finance.RemoveInventoryEntry(ctx, u.TenantSchema, m.id); err != nil {
			return err
		}
	}

	tx, terr := conn.BeginTx(ctx, nil)
	if terr != nil {
		return appErrs.Internal(terr.Error())
	}
	defer tx.Rollback()

	// Revert PO receipts before deleting movements.
	rows, err := tx.QueryContext(ctx, `
		SELECT purchase_order_line_id::text, qty
		FROM pur_bill_line WHERE bill_id = $1 AND purchase_order_line_id IS NOT NULL`, id)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	type poRev struct{ lineID string; qty float64 }
	var revs []poRev
	for rows.Next() {
		var r poRev
		if err := rows.Scan(&r.lineID, &r.qty); err != nil {
			rows.Close()
			return appErrs.Internal(err.Error())
		}
		revs = append(revs, r)
	}
	rows.Close()

	if _, err := purgeMovementsByRef(ctx, tx, "bill", id); err != nil {
		return err
	}
	for _, r := range revs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE pur_purchase_order_line SET qty_received = GREATEST(0, qty_received - $2) WHERE id = $1`,
			r.lineID, r.qty); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	if bill.PurchaseOrderID != nil && *bill.PurchaseOrderID != "" {
		var ordered, received float64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(qty_ordered),0), COALESCE(SUM(qty_received),0)
			FROM pur_purchase_order_line WHERE purchase_order_id = $1`,
			*bill.PurchaseOrderID).Scan(&ordered, &received); err != nil {
			return appErrs.Internal(err.Error())
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE pur_purchase_order SET status = $2, updated_at = now() WHERE id = $1`,
			*bill.PurchaseOrderID, poStatusFromReceipts(ordered, received)); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pur_bill WHERE id = $1`, id); err != nil {
		return appErrs.Internal(err.Error())
	}
	return tx.Commit()
}
