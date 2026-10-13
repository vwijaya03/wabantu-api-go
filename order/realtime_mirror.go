package order

import (
	"context"

	"encore.dev/rlog"

	"encore.app/wabantu/internal/realtime"
	sim "encore.app/wabantu/internal/simulator"
	"encore.app/wabantu/shared/types"
)

// mirrorOrderState dual-writes the simulator conversation state (order status + proofCheck)
// to Firestore after an owner action. Best-effort, no-op when unconfigured or not a simulator chat.
func mirrorOrderState(ctx context.Context, u *types.AuthUser, convoID string) {
	if u == nil || convoID == "" {
		return
	}
	m := realtime.Default(realtime.LogfTo(rlog.Warn))
	sim.Mirror(ctx, m, sim.SQLDB(db.Stdlib()), sim.SchemaQualifier(u.TenantSchema), u.TenantID, convoID, nil)
}
