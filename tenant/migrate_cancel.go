package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"encore.app/wabantu/system"
	"encore.dev/rlog"
)

// CancelSchemaMigrationJob stops a pending/running job and releases queued tenants.
func CancelSchemaMigrationJob(ctx context.Context, jobID string) (*SchemaMigrationJobSummary, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("jobId required")
	}

	var status string
	err := system.DB.QueryRow(ctx, `
		SELECT status FROM tenant_schema_migration_job WHERE id = $1::uuid`, jobID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("migration job not found")
	}
	if err != nil {
		return nil, err
	}
	if status != migrationJobStatusPending && status != migrationJobStatusRunning {
		return nil, fmt.Errorf("job tidak aktif (status: %s)", status)
	}

	res, err := system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job_item
		SET status = $2, error_text = 'dibatalkan admin', updated_at = now()
		WHERE job_id = $1::uuid AND status = ANY($3::text[])`,
		jobID, migrationItemStatusSkipped,
		[]string{migrationItemStatusQueued, migrationItemStatusRunning},
	)
	if err != nil {
		return nil, err
	}
	if n := res.RowsAffected(); n > 0 {
		rlog.Info("migration job items cancelled", "jobId", jobID, "items", n)
	}

	if err := syncMigrationJobCounters(ctx, jobID); err != nil {
		return nil, err
	}

	_, err = system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job
		SET status = $2, completed_at = now()
		WHERE id = $1::uuid AND status = ANY($3::text[])`,
		jobID, migrationJobStatusCancelled,
		[]string{migrationJobStatusPending, migrationJobStatusRunning},
	)
	if err != nil {
		return nil, err
	}

	return GetSchemaMigrationJob(ctx, jobID)
}

func migrationJobCancelled(ctx context.Context, jobID string) (bool, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return false, nil
	}
	var status string
	err := system.DB.QueryRow(ctx, `
		SELECT status FROM tenant_schema_migration_job WHERE id = $1::uuid`, jobID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return status == migrationJobStatusCancelled, nil
}

func skipMigrationItemIfPending(ctx context.Context, itemID, jobID string) {
	if itemID == "" {
		return
	}
	res, err := system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job_item
		SET status = $2, updated_at = now()
		WHERE id = $1::uuid AND status = ANY($3::text[])`,
		itemID, migrationItemStatusSkipped,
		[]string{migrationItemStatusQueued, migrationItemStatusRunning},
	)
	if err != nil {
		return
	}
	if n := res.RowsAffected(); n > 0 {
		_ = syncMigrationJobCounters(ctx, jobID)
	}
}

func syncMigrationJobCounters(ctx context.Context, jobID string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE tenant_schema_migration_job j
		SET done_count = (
			SELECT COUNT(*)::int FROM tenant_schema_migration_job_item
			WHERE job_id = j.id AND status = ANY($2::text[])
		),
		failed_count = (
			SELECT COUNT(*)::int FROM tenant_schema_migration_job_item
			WHERE job_id = j.id AND status = $3
		)
		WHERE j.id = $1::uuid`,
		jobID,
		[]string{migrationItemStatusSucceeded, migrationItemStatusSkipped},
		migrationItemStatusFailed,
	)
	return err
}
