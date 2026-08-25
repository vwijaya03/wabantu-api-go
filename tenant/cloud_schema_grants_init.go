package tenant

import (
	"context"
	"time"

	"encore.dev"
	"encore.dev/rlog"
)

func init() {
	if encore.Meta().Environment.Cloud == encore.CloudLocal {
		return
	}
	go func() {
		time.Sleep(3 * time.Second)
		prep, err := RunCloudMigrationPrep(context.Background())
		if err != nil {
			rlog.Error("cloud deploy prep on startup failed", "err", err)
			return
		}
		if prep.RepairFnMissing {
			rlog.Warn("cloud deploy prep: repair_tenant_schema_grants belum terpasang — deploy migration 4/5")
		}
		if len(prep.OrphansPruned) > 0 {
			rlog.Info("cloud startup pruned orphan schemas", "schemas", prep.OrphansPruned)
		}
		if !prep.DeployReady {
			rlog.Warn("cloud deploy not ready after startup prep", "blockers", prep.DeployBlockers)
			return
		}
		rlog.Info("cloud deploy prep OK on startup", "grantsRepaired", prep.GrantsRepaired)
	}()
}
