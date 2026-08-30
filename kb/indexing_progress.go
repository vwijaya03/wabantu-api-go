package kb

import (
	"context"
	"database/sql"
	"time"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

// EntityIndexCounts groups embedding_status counts for KB or catalog.
type EntityIndexCounts struct {
	Pending int `json:"pending"`
	Indexed int `json:"indexed"`
	Failed  int `json:"failed"`
	Dlq     int `json:"dlq"`
	Total   int `json:"total"`
}

// OutboxCounts groups retrieval_outbox status counts.
type OutboxCounts struct {
	Pending int `json:"pending"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Dlq     int `json:"dlq"`
	Total   int `json:"total"`
}

// TenantIndexingProgress summarizes indexing state for one tenant schema.
type TenantIndexingProgress struct {
	TenantID          string            `json:"tenantId"`
	KB                EntityIndexCounts `json:"kb"`
	Catalog           EntityIndexCounts `json:"catalog"`
	Outbox            OutboxCounts      `json:"outbox"`
	PercentComplete   int               `json:"percentComplete"`
	OutboxPercentDone int               `json:"outboxPercentDone"`
	IsComplete        bool              `json:"isComplete"`
	OldestPendingAt   *time.Time        `json:"oldestPendingAt,omitempty"`
}

// GetTenantIndexingProgress returns embedding/outbox progress for a tenant schema.
func GetTenantIndexingProgress(ctx context.Context, tenantSchema, tenantID string) (*TenantIndexingProgress, error) {
	if tenantSchema == "" {
		return nil, sql.ErrNoRows
	}
	if err := tenant.EnsureRetrievalSchema(ctx, tenantSchema); err != nil {
		return nil, err
	}
	ts, err := openTenantScope(ctx, tenantSchema)
	if err != nil {
		return nil, err
	}

	kb, err := countEntityStatuses(ctx, ts, `
		SELECT embedding_status, COUNT(*)::int
		FROM knowledge_base_entry
		WHERE deleted_at IS NULL AND is_active = true
		GROUP BY embedding_status`)
	if err != nil {
		return nil, err
	}

	cat, err := countEntityStatuses(ctx, ts, `
		SELECT embedding_status, COUNT(*)::int
		FROM business_catalog_item
		WHERE deleted_at IS NULL AND is_active = true
		GROUP BY embedding_status`)
	if err != nil {
		return nil, err
	}

	outbox, err := countOutboxStatuses(ctx, ts)
	if err != nil {
		return nil, err
	}

	oldest, err := oldestOutboxPending(ctx, ts)
	if err != nil {
		return nil, err
	}

	progress := &TenantIndexingProgress{
		TenantID:        tenantID,
		KB:              kb,
		Catalog:         cat,
		Outbox:          outbox,
		PercentComplete: entityPercentComplete(kb, cat),
		OutboxPercentDone: outboxPercentDone(outbox),
		OldestPendingAt: oldest,
	}
	progress.IsComplete = progress.PercentComplete >= 100 && outbox.Pending == 0 && outbox.Failed == 0
	return progress, nil
}

func countEntityStatuses(ctx context.Context, ts appdb.TenantScope, query string) (EntityIndexCounts, error) {
	var counts EntityIndexCounts
	rows, err := ts.QueryContext(ctx, query)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return counts, err
		}
		counts.Total += n
		switch status {
		case "pending":
			counts.Pending += n
		case "indexed":
			counts.Indexed += n
		case "failed":
			counts.Failed += n
		case "dlq":
			counts.Dlq += n
		}
	}
	return counts, rows.Err()
}

func countOutboxStatuses(ctx context.Context, ts appdb.TenantScope) (OutboxCounts, error) {
	var counts OutboxCounts
	rows, err := ts.QueryContext(ctx, `
		SELECT status, COUNT(*)::int
		FROM retrieval_outbox
		GROUP BY status`)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return counts, err
		}
		counts.Total += n
		switch status {
		case "pending":
			counts.Pending += n
		case "done":
			counts.Done += n
		case "failed":
			counts.Failed += n
		case "dlq":
			counts.Dlq += n
		}
	}
	return counts, rows.Err()
}

func oldestOutboxPending(ctx context.Context, ts appdb.TenantScope) (*time.Time, error) {
	var t sql.NullTime
	err := ts.QueryRowContext(ctx, `
		SELECT MIN(created_at)
		FROM retrieval_outbox
		WHERE status = 'pending'`).Scan(&t)
	if err != nil {
		return nil, err
	}
	if !t.Valid {
		return nil, nil
	}
	utc := t.Time.UTC()
	return &utc, nil
}

func entityPercentComplete(kb, cat EntityIndexCounts) int {
	total := kb.Total + cat.Total
	if total == 0 {
		return 100
	}
	done := kb.Indexed + cat.Indexed
	return int((float64(done) / float64(total)) * 100)
}

func outboxPercentDone(o OutboxCounts) int {
	work := o.Done + o.Pending + o.Failed + o.Dlq
	if work == 0 {
		return 100
	}
	return int((float64(o.Done) / float64(work)) * 100)
}
