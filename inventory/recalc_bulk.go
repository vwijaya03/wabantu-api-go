package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

func applyMovementSnapshots(ctx context.Context, tx *sql.Tx, snaps []ReplaySnapshot) error {
	const chunk = 100
	for i := 0; i < len(snaps); i += chunk {
		end := i + chunk
		if end > len(snaps) {
			end = len(snaps)
		}
		if err := applyMovementSnapshotsChunk(ctx, tx, snaps[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func applyMovementSnapshotsChunk(ctx context.Context, tx *sql.Tx, snaps []ReplaySnapshot) error {
	if len(snaps) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`UPDATE inv_stock_movement AS m SET
		total_cost = d.total_cost,
		unit_cost = d.unit_cost,
		qty_after = d.qty_after,
		avg_cost_after = d.avg_cost_after
	FROM (VALUES `)
	args := make([]any, 0, len(snaps)*5)
	for i, s := range snaps {
		if i > 0 {
			b.WriteString(",")
		}
		base := i*5 + 1
		b.WriteString(fmt.Sprintf("($%d::uuid,$%d::numeric,$%d::numeric,$%d::numeric,$%d::numeric)",
			base, base+1, base+2, base+3, base+4))
		args = append(args, s.MovementID, s.TotalCost, s.UnitCost, s.QtyAfter, s.AvgAfter)
	}
	b.WriteString(`) AS d(id, total_cost, unit_cost, qty_after, avg_cost_after) WHERE m.id = d.id`)
	if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}
