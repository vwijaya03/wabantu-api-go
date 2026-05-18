package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("system")

// ---------- types ----------

type AuditLog struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenantId"`
	UserID     string         `json:"userId"`
	Action     string         `json:"action"`
	EntityType string         `json:"entityType"`
	EntityID   string         `json:"entityId"`
	Changes    json.RawMessage `json:"changes,omitempty"`
	IPAddress  string         `json:"ipAddress"`
	UserAgent  string         `json:"userAgent"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type RecordAuditParams struct {
	TenantID   string         `json:"tenantId"`
	UserID     string         `json:"userId,omitempty"`
	Action     string         `json:"action"`
	EntityType string         `json:"entityType"`
	EntityID   string         `json:"entityId,omitempty"`
	Changes    json.RawMessage `json:"changes,omitempty"`
	IPAddress  string         `json:"ipAddress,omitempty"`
	UserAgent  string         `json:"userAgent,omitempty"`
}

type ListAuditParams struct {
	TenantID string `query:"tenantId"`
	Action   string `query:"action"`
	DateFrom string `query:"dateFrom"`
	DateTo   string `query:"dateTo"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListAuditResponse struct {
	Logs  []AuditLog `json:"logs"`
	Total int        `json:"total"`
}

// ---------- endpoints ----------

//encore:api private method=POST path=/api/v1/audit/log
func RecordAudit(ctx context.Context, p *RecordAuditParams) error {
	changesJSON := p.Changes
	if len(changesJSON) == 0 {
		changesJSON = []byte("{}")
	}

	_, err := db.Exec(ctx,
		`INSERT INTO audit_log
			(tenant_id, user_id, action, entity_type, entity_id, changes, ip_address, user_agent)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		p.TenantID, p.UserID, p.Action, p.EntityType, p.EntityID,
		changesJSON, p.IPAddress, p.UserAgent,
	)
	return err
}

//encore:api auth method=GET path=/api/v1/audit/logs
func ListAuditLogs(ctx context.Context, p *ListAuditParams) (*ListAuditResponse, error) {
	u, _ := auth.Data().(*types.AuthUser)
	if u == nil || u.Role != "super_admin" {
		return nil, appErrs.Forbidden("super_admin access required")
	}

	page := p.Page
	if page < 1 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	where := "WHERE 1=1"
	args := []any{}
	idx := 1

	if p.TenantID != "" {
		where += fmt.Sprintf(" AND tenant_id = $%d", idx)
		args = append(args, p.TenantID)
		idx++
	}
	if p.Action != "" {
		where += fmt.Sprintf(" AND action = $%d", idx)
		args = append(args, p.Action)
		idx++
	}
	if p.DateFrom != "" {
		where += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, p.DateFrom)
		idx++
	}
	if p.DateTo != "" {
		where += fmt.Sprintf(" AND created_at <= $%d", idx)
		args = append(args, p.DateTo)
		idx++
	}

	var total int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log "+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	q := fmt.Sprintf(
		`SELECT id, tenant_id, COALESCE(user_id,''), action, entity_type,
		        COALESCE(entity_id,''), COALESCE(changes,'{}'), COALESCE(ip_address,''),
		        COALESCE(user_agent,''), created_at
		 FROM audit_log %s
		 ORDER BY created_at DESC
		 LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, pageSize, offset)

	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]AuditLog, 0)
	for rows.Next() {
		var l AuditLog
		var changesRaw []byte
		if err := rows.Scan(
			&l.ID, &l.TenantID, &l.UserID, &l.Action, &l.EntityType,
			&l.EntityID, &changesRaw, &l.IPAddress, &l.UserAgent, &l.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(changesRaw) > 0 {
			l.Changes = changesRaw
		}
		logs = append(logs, l)
	}

	return &ListAuditResponse{Logs: logs, Total: total}, nil
}
