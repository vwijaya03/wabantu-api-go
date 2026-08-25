package inventory

import (
	"context"

	"encore.app/wabantu/finance"
	appErrs "encore.app/wabantu/shared/errs"
)

//encore:api auth method=DELETE path=/api/v1/inventory/invoices/:id
func DeleteInvoice(ctx context.Context, id string) error {
	u, err := getUser()
	if err != nil {
		return err
	}
	if err := requireOwner(u); err != nil {
		return err
	}
	sch, err := prepareTenant(ctx, u.TenantSchema)
	if err != nil {
		return err
	}
	pool := tenantDB()

	inv, err := getInvoice(ctx, sch, pool, id)
	if err != nil {
		return err
	}
	if err := finance.CheckPeriodUnlockedForDate(ctx, u.TenantSchema, inv.TransactionDate); err != nil {
		return err
	}

	tx, terr := pool.BeginTx(ctx, nil)
	if terr != nil {
		return appErrs.Internal(terr.Error())
	}
	defer tx.Rollback()
	if _, err := qexec(ctx, sch, tx, `DELETE FROM inv_invoice WHERE id = $1`, id); err != nil {
		return appErrs.Internal(err.Error())
	}
	return tx.Commit()
}
