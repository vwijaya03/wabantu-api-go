package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/system"
	"encore.dev/rlog"
)

const (
	migrationBusyMaxAttempts      = 12
	migrationStaleRunningInterval = "15 minutes"
)

// tenantSchemaMigrationUpToDate reports whether a tenant can skip heavy migration work.
func tenantSchemaMigrationUpToDate(ctx context.Context, tenantID, schemaName string) (bool, error) {
	ver, err := getTenantSchemaPatchVersion(ctx, tenantID)
	if err != nil {
		return false, err
	}
	if ver < CurrentSchemaPatchVersion {
		return false, nil
	}
	if !isEncoreCloud() {
		return true, nil
	}
	return tenantschema.CloudTenantReady(ctx, DataDB.Stdlib(), schemaName)
}

func markMigrationItemSucceeded(ctx context.Context, itemID, jobID string) {
	_, _ = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job_item
		SET status = $2, error_text = NULL, updated_at = now()
		WHERE id = $1::uuid`,
		itemID, migrationItemStatusSucceeded,
	)
	_ = incrementJobCounter(ctx, jobID, true)
}

func insertSkippedMigrationItem(ctx context.Context, jobID, tenantID, schemaName string) error {
	_, err := system.DB.Exec(ctx, `
		INSERT INTO tenant_schema_migration_job_item (job_id, tenant_id, schema_name, status)
		VALUES ($1::uuid, $2::uuid, $3, $4)`,
		jobID, tenantID, schemaName, migrationItemStatusSkipped,
	)
	if err != nil {
		return err
	}
	return incrementJobCounter(ctx, jobID, true)
}

func handleMigrationItemBusy(ctx context.Context, msg *TenantSchemaMigrateMessage) error {
	var attempts int
	err := system.DB.QueryRow(ctx, `
		SELECT attempts FROM tenant_schema_migration_job_item WHERE id = $1::uuid`,
		msg.ItemID,
	).Scan(&attempts)
	if err != nil {
		return err
	}

	_, _ = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job_item
		SET status = $2, updated_at = now()
		WHERE id = $1::uuid`,
		msg.ItemID, migrationItemStatusQueued,
	)

	if attempts >= migrationBusyMaxAttempts {
		busyErr := fmt.Sprintf("%v (attempts=%d)", errSchemaMigrationBusy, attempts)
		_, _ = system.DB.Exec(ctx, `
			UPDATE tenant_schema_migration_job_item
			SET status = $2, error_text = $3, updated_at = now()
			WHERE id = $1::uuid`,
			msg.ItemID, migrationItemStatusFailed, busyErr,
		)
		_ = incrementJobCounter(ctx, msg.JobID, false)
		rlog.Warn("tenant schema migration lock timeout",
			"tenantId", msg.TenantID, "schema", msg.SchemaName, "attempts", attempts)
		return nil
	}

	if _, pubErr := SchemaMigrateTopic.Publish(ctx, msg); pubErr != nil {
		return pubErr
	}
	rlog.Info("tenant schema migration busy, re-enqueued",
		"tenantId", msg.TenantID, "schema", msg.SchemaName, "attempts", attempts)
	return nil
}

// RecoverStaleMigrationJobItems resets long-running items and re-enqueues them.
func RecoverStaleMigrationJobItems(ctx context.Context) error {
	rows, err := system.DB.Query(ctx, `
		SELECT ji.id::text, ji.job_id::text, ji.tenant_id::text, ji.schema_name,
		       j.patch_version, j.started_by::text
		FROM tenant_schema_migration_job_item ji
		JOIN tenant_schema_migration_job j ON j.id = ji.job_id
		WHERE ji.status = $1
		  AND ji.updated_at < now() - $2::interval
		  AND j.status = ANY($3::text[])`,
		migrationItemStatusRunning,
		migrationStaleRunningInterval,
		[]string{migrationJobStatusPending, migrationJobStatusRunning},
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type staleItem struct {
		itemID, jobID, tenantID, schemaName, migratedBy string
		patchVersion                                      int
	}
	var stale []staleItem
	for rows.Next() {
		var it staleItem
		var migratedBy sql.NullString
		if err := rows.Scan(
			&it.itemID, &it.jobID, &it.tenantID, &it.schemaName,
			&it.patchVersion, &migratedBy,
		); err != nil {
			return err
		}
		if migratedBy.Valid {
			it.migratedBy = migratedBy.String
		}
		stale = append(stale, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, it := range stale {
		_, err := system.DB.Exec(ctx, `
			UPDATE tenant_schema_migration_job_item
			SET status = $2, updated_at = now()
			WHERE id = $1::uuid AND status = $3`,
			it.itemID, migrationItemStatusQueued, migrationItemStatusRunning,
		)
		if err != nil {
			return err
		}
		msg := &TenantSchemaMigrateMessage{
			JobID:        it.jobID,
			ItemID:       it.itemID,
			TenantID:     it.tenantID,
			SchemaName:   it.schemaName,
			PatchVersion: it.patchVersion,
			MigratedBy:   it.migratedBy,
		}
		if _, pubErr := SchemaMigrateTopic.Publish(ctx, msg); pubErr != nil {
			return fmt.Errorf("re-enqueue stale migration %s: %w", it.schemaName, pubErr)
		}
		rlog.Warn("recovered stale migration job item",
			"schema", it.schemaName, "itemId", it.itemID, "jobId", it.jobID)
	}
	return nil
}

func maybeRecoverStaleMigrationJobItems(ctx context.Context) {
	if err := RecoverStaleMigrationJobItems(ctx); err != nil {
		rlog.Warn("recover stale migration items failed", "err", err)
	}
	maybeFinalizeCompletedMigrationJobs(ctx)
}

func maybeFinalizeCompletedMigrationJobs(ctx context.Context) {
	_, _ = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job
		SET status = $1, completed_at = COALESCE(completed_at, now())
		WHERE status = $2
		  AND total_count > 0
		  AND done_count + failed_count >= total_count`,
		migrationJobStatusCompleted, migrationJobStatusRunning,
	)
}

func enqueueMigrationTarget(
	ctx context.Context,
	jobID string,
	target SchemaMigrationTarget,
	migratedBy string,
) (int, error) {
	upToDate, err := tenantSchemaMigrationUpToDate(ctx, target.TenantID, target.SchemaName)
	if err != nil {
		return 0, err
	}
	if upToDate {
		if err := insertSkippedMigrationItem(ctx, jobID, target.TenantID, target.SchemaName); err != nil {
			return 0, err
		}
		return 0, nil
	}

	var itemID string
	err = system.DB.QueryRow(ctx, `
		INSERT INTO tenant_schema_migration_job_item (job_id, tenant_id, schema_name, status)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING id::text`,
		jobID, target.TenantID, target.SchemaName, migrationItemStatusQueued,
	).Scan(&itemID)
	if err != nil {
		return 0, err
	}
	msg := &TenantSchemaMigrateMessage{
		JobID:        jobID,
		ItemID:       itemID,
		TenantID:     target.TenantID,
		SchemaName:   target.SchemaName,
		PatchVersion: CurrentSchemaPatchVersion,
		MigratedBy:   migratedBy,
	}
	if _, err := SchemaMigrateTopic.Publish(ctx, msg); err != nil {
		return 0, fmt.Errorf("publish migration job: %w", err)
	}
	return 1, nil
}
