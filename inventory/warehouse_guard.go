package inventory

import (
	"context"
	"fmt"

	appErrs "encore.app/wabantu/shared/errs"
)

type warehouseUsage struct {
	StockBalance   int `json:"stockBalanceRows"`
	Movements      int `json:"movementRows"`
	POLines        int `json:"poLines"`
	BillLines      int `json:"billLines"`
	ReturnLines    int `json:"returnLines"`
	TxnLines       int `json:"transactionLines"`
}

func (u warehouseUsage) inUse() bool {
	return u.StockBalance > 0 || u.Movements > 0 || u.POLines > 0 ||
		u.BillLines > 0 || u.ReturnLines > 0 || u.TxnLines > 0
}

func (u warehouseUsage) message() string {
	var parts []string
	if u.StockBalance > 0 {
		parts = append(parts, fmt.Sprintf("%d saldo stok", u.StockBalance))
	}
	if u.Movements > 0 {
		parts = append(parts, fmt.Sprintf("%d pergerakan", u.Movements))
	}
	if u.POLines > 0 {
		parts = append(parts, fmt.Sprintf("%d baris PO", u.POLines))
	}
	if u.BillLines > 0 {
		parts = append(parts, fmt.Sprintf("%d baris penerimaan", u.BillLines))
	}
	if u.ReturnLines > 0 {
		parts = append(parts, fmt.Sprintf("%d baris retur", u.ReturnLines))
	}
	if u.TxnLines > 0 {
		parts = append(parts, fmt.Sprintf("%d baris operasi stok", u.TxnLines))
	}
	if len(parts) == 0 {
		return ""
	}
	return "gudang masih dipakai: " + stringsJoin(parts)
}

func stringsJoin(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += ", " + parts[i]
	}
	return out
}

func loadWarehouseUsage(ctx context.Context, q querier, warehouseID string) (warehouseUsage, error) {
	var u warehouseUsage
	err := q.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*)::int FROM inv_stock_balance WHERE warehouse_id = $1 AND on_hand > 0.0001),
		  (SELECT COUNT(*)::int FROM inv_stock_movement WHERE warehouse_id = $1),
		  (SELECT COUNT(*)::int FROM pur_purchase_order_line WHERE warehouse_id = $1),
		  (SELECT COUNT(*)::int FROM pur_bill_line WHERE warehouse_id = $1),
		  (SELECT COUNT(*)::int FROM inv_sales_return_line WHERE warehouse_id = $1),
		  (SELECT COUNT(*)::int FROM inv_stock_transaction_line WHERE warehouse_id = $1)
		   + (SELECT COUNT(*)::int FROM inv_stock_transaction WHERE warehouse_id = $1 OR from_warehouse_id = $1 OR to_warehouse_id = $1)
	`, warehouseID).Scan(&u.StockBalance, &u.Movements, &u.POLines, &u.BillLines, &u.ReturnLines, &u.TxnLines)
	if err != nil {
		return u, appErrs.Internal(err.Error())
	}
	return u, nil
}
