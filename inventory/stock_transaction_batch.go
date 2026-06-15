package inventory

import (
	"context"
	"fmt"

	appErrs "encore.app/wabantu/shared/errs"
)

type BatchDeleteStockTransactionsParams struct {
	IDs []string `json:"ids"`
}

type BatchDeleteStockTransactionsResult struct {
	Deleted int      `json:"deleted"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

//encore:api auth method=POST path=/api/v1/inventory/stock-transactions/batch-delete
func BatchDeleteStockTransactions(ctx context.Context, p *BatchDeleteStockTransactionsParams) (*BatchDeleteStockTransactionsResult, error) {
	u, err := getUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(u); err != nil {
		return nil, err
	}
	if len(p.IDs) == 0 {
		return nil, appErrs.BadRequest("ids wajib diisi")
	}
	if len(p.IDs) > 100 {
		return nil, appErrs.BadRequest("maksimal 100 transaksi per batch")
	}

	res := &BatchDeleteStockTransactionsResult{}
	for _, id := range p.IDs {
		if err := DeleteStockTransaction(ctx, id); err != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		res.Deleted++
	}
	return res, nil
}
