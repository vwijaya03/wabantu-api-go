package audit

import (
	"context"
	"encoding/json"

	"encore.dev/rlog"
)

// Log writes an audit record (best-effort; errors are logged only).
func Log(ctx context.Context, tenantID, userID, action, entityType, entityID string, changes any) {
	var changesJSON json.RawMessage
	if changes != nil {
		b, err := json.Marshal(changes)
		if err != nil {
			changesJSON = []byte("{}")
		} else {
			changesJSON = b
		}
	} else {
		changesJSON = []byte("{}")
	}
	err := RecordAudit(ctx, &RecordAuditParams{
		TenantID:   tenantID,
		UserID:     userID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Changes:    changesJSON,
	})
	if err != nil {
		rlog.Warn("audit log failed", "action", action, "err", err)
	}
}
