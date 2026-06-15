package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/tenant"
)

type UpdateBillParams struct {
	SupplierName    string          `json:"supplierName"`
	ContactID       string          `json:"contactId"`
	WarehouseID     string          `json:"warehouseId"`
	TransactionDate string          `json:"transactionDate"`
	Note            string          `json:"note"`
	Lines           []BillLineInput `json:"lines"`
}

//encore:api auth method=PATCH path=/api/v1/inventory/bills/:id
func UpdateBill(ctx context.Context, id string, p *UpdateBillParams) (*Bill, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.Lines) == 0 {
		return nil, appErrs.BadRequest("minimal 1 baris item")
	}

	conn, err := tenant.TenantConn(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer conn.Close()

	existing, err := getBill(ctx, conn, id)
	if err != nil {
		return nil, err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, existing.TransactionDate); err != nil {
		return nil, err
	}

	setting, err := loadSetting(ctx, conn)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	if setting.PurchasePostsExpense {
		if err := finance.CheckCurrentPeriodUnlocked(ctx, u.TenantSchema); err != nil {
			return nil, err
		}
	}

	txnDate := strings.TrimSpace(p.TransactionDate)
	if txnDate == "" {
		txnDate = existing.TransactionDate
	}
	defaultWarehouse := strings.TrimSpace(p.WarehouseID)
	if defaultWarehouse == "" && existing.PurchaseOrderID != nil {
		// keep existing warehouse from bill header if not provided
	}
	for i := range p.Lines {
		l := &p.Lines[i]
		if l.Qty <= epsilon {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: qty harus lebih dari 0", i+1))
		}
		if l.UnitCost < 0 {
			return nil, appErrs.BadRequest(fmt.Sprintf("baris %d: harga tidak boleh negatif", i+1))
		}
		if strings.TrimSpace(l.WarehouseID) == "" {
			l.WarehouseID = defaultWarehouse
		}
		if err := validateCatalogItem(ctx, conn, l.CatalogItemID); err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
		if err := validateWarehouse(ctx, conn, l.WarehouseID); err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
	}

	if err := finance.RemoveInventoryEntry(ctx, u.TenantSchema, id); err != nil {
		return nil, err
	}
	movs, err := collectMovementsByRef(ctx, conn, "bill", id)
	if err != nil {
		return nil, err
	}
	for _, m := range movs {
		if err := finance.RemoveInventoryEntry(ctx, u.TenantSchema, m.id); err != nil {
			return nil, err
		}
	}

	tx, terr := conn.BeginTx(ctx, nil)
	if terr != nil {
		return nil, appErrs.Internal(terr.Error())
	}
	defer tx.Rollback()

	if err := revertPOReceiptsForBill(ctx, tx, id); err != nil {
		return nil, err
	}
	if _, err := purgeMovementsByRef(ctx, tx, "bill", id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pur_bill_line WHERE bill_id = $1`, id); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	subtotal := sumBillLines(p.Lines)
	if _, err := tx.ExecContext(ctx, `
		UPDATE pur_bill
		SET supplier_name = $2, contact_id = $3, warehouse_id = $4, transaction_date = $5,
		    note = $6, subtotal = $7, updated_at = now()
		WHERE id = $1`,
		id, nullStr(p.SupplierName), nullUUID(p.ContactID), nullUUID(defaultWarehouse),
		txnDate, nullStr(p.Note), subtotal); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	poID := ""
	if existing.PurchaseOrderID != nil {
		poID = *existing.PurchaseOrderID
	}
	if err := postBillLinesTx(ctx, tx, id, existing.BillNo, poID, u.AccountID, p.Lines); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	if setting.PurchasePostsExpense && subtotal > 0 {
		desc := fmt.Sprintf("Pembelian persediaan %s", existing.BillNo)
		if err := finance.RecordInventoryEntry(ctx, u.TenantSchema, u.AccountID,
			id, "expense", finCatPembelianPersediaan, desc, round2(subtotal)); err != nil {
			return nil, err
		}
	}
	return getBill(ctx, conn, id)
}

func revertPOReceiptsForBill(ctx context.Context, tx *sql.Tx, billID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT purchase_order_line_id::text, qty
		FROM pur_bill_line WHERE bill_id = $1 AND purchase_order_line_id IS NOT NULL`, billID)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		var qty float64
		if err := rows.Scan(&lineID, &qty); err != nil {
			return appErrs.Internal(err.Error())
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pur_purchase_order_line SET qty_received = GREATEST(0, qty_received - $2) WHERE id = $1`,
			lineID, qty); err != nil {
			return appErrs.Internal(err.Error())
		}
	}
	return rows.Err()
}

func refreshPOStatusTx(ctx context.Context, tx *sql.Tx, poID string) error {
	var ordered, received float64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(qty_ordered),0), COALESCE(SUM(qty_received),0)
		FROM pur_purchase_order_line WHERE purchase_order_id = $1`, poID).Scan(&ordered, &received); err != nil {
		return appErrs.Internal(err.Error())
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE pur_purchase_order SET status = $2, updated_at = now()
		WHERE id = $1 AND status IN ('open','partial','received')`,
		poID, poStatusFromReceipts(ordered, received))
	return err
}

func postBillLinesTx(ctx context.Context, tx *sql.Tx, billID, billNo, poID, accountID string, lines []BillLineInput) error {
	for _, l := range lines {
		expiry, perr := parseDatePtr(l.ExpiryDate)
		if perr != nil {
			return perr
		}
		var lineID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO pur_bill_line
			  (bill_id, purchase_order_line_id, catalog_item_id, warehouse_id, description, qty, unit_cost, batch_no, expiry_date)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING id`,
			billID, nullUUID(l.PurchaseOrderLineID), l.CatalogItemID, l.WarehouseID,
			nullStr(l.Description), round4(l.Qty), round4(l.UnitCost), nullStr(l.BatchNo), nullTime(expiry)).Scan(&lineID); err != nil {
			return appErrs.Internal(err.Error())
		}
		if err := ensureSku(ctx, tx, l.CatalogItemID); err != nil {
			return appErrs.Internal(err.Error())
		}
		cc, cerr := loadCostingContext(ctx, tx, l.CatalogItemID)
		if cerr != nil {
			return cerr
		}
		res, merr := PostMovement(ctx, tx, MovementInput{
			CatalogItemID: l.CatalogItemID,
			WarehouseID:   l.WarehouseID,
			Type:          MovementPurchaseReceive,
			Direction:     dirIn,
			Qty:           round4(l.Qty),
			UnitCost:      round4(l.UnitCost),
			CostingMethod: cc.method,
			BlockNegative: cc.blockNegative,
			BatchNo:       l.BatchNo,
			ExpiryDate:    expiry,
			RefType:       "bill",
			RefID:         billID,
			RefLineID:     lineID,
			Note:          "Penerimaan barang " + billNo,
			CreatedBy:     accountID,
		})
		if merr != nil {
			return merr
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE pur_bill_line SET movement_id = $2 WHERE id = $1`, lineID, res.MovementID); err != nil {
			return appErrs.Internal(err.Error())
		}
		if strings.TrimSpace(l.PurchaseOrderLineID) != "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE pur_purchase_order_line SET qty_received = qty_received + $2 WHERE id = $1`,
				l.PurchaseOrderLineID, round4(l.Qty)); err != nil {
				return appErrs.Internal(err.Error())
			}
		}
	}
	if strings.TrimSpace(poID) != "" {
		return refreshPOStatusTx(ctx, tx, poID)
	}
	return nil
}
