package leads

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	e "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

func withTenantDB(ctx context.Context, schema string) (*sql.DB, error) {
	stdlib := db.Stdlib()
	_, err := stdlib.ExecContext(ctx, fmt.Sprintf(`SET search_path TO %q`, schema))
	if err != nil {
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	return stdlib, nil
}

// ─── Types ───────────────────────────────────────────────────────────────────

type Lead struct {
	ID             string     `json:"id"`
	ContactID      *string    `json:"contactId,omitempty"`
	ConversationID *string    `json:"conversationId,omitempty"`
	Name           string     `json:"name"`
	Status         string     `json:"status"` // "new" | "contacted" | "qualified" | "converted" | "lost"
	Notes          *string    `json:"notes,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ListRequest struct {
	Status   string `query:"status"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListResponse struct {
	Items    []Lead `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type GetResponse struct {
	Lead Lead `json:"lead"`
}

type CaptureRequest struct {
	TenantSchema   string `json:"tenantSchema"`
	ContactID      string `json:"contactId"`
	ConversationID string `json:"conversationId"`
	ContactName    string `json:"contactName"`
}

type CaptureResponse struct {
	Created bool `json:"created"`
	LeadID  string `json:"leadId"`
}

// ─── Auth helper ─────────────────────────────────────────────────────────────

func currentUser(ctx context.Context) (*types.AuthUser, error) {
	uid, ok := auth.UserID()
	data := auth.Data()
	if !ok || uid == "" || data == nil {
		return nil, e.Unauthenticated("not authenticated")
	}
	u, ok := data.(*types.AuthUser)
	if !ok {
		return nil, e.Unauthenticated("invalid auth data")
	}
	return u, nil
}

// ─── Endpoints ───────────────────────────────────────────────────────────────

//encore:api auth method=GET path=/leads
func List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := withTenantDB(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	where := "WHERE 1=1"
	var args []any
	argN := 1

	if status := strings.TrimSpace(req.Status); status != "" {
		where += fmt.Sprintf(" AND l.status = $%d", argN)
		args = append(args, status)
		argN++
	}

	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM leads l %s", where)
	if err := conn.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count leads: %w", err)
	}

	querySQL := fmt.Sprintf(`
		SELECT l.id, l.contact_id, l.conversation_id,
		       COALESCE(NULLIF(TRIM(l.name),''), c.display_name, '') AS name,
		       l.status, l.notes, l.created_at, l.updated_at
		FROM leads l
		LEFT JOIN contacts c ON c.id = l.contact_id
		%s
		ORDER BY l.created_at DESC
		LIMIT $%d OFFSET $%d`, where, argN, argN+1)
	args = append(args, pageSize, offset)

	rows, err := conn.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list leads: %w", err)
	}
	defer rows.Close()

	var items []Lead
	for rows.Next() {
		var l Lead
		if err := rows.Scan(
			&l.ID, &l.ContactID, &l.ConversationID,
			&l.Name, &l.Status, &l.Notes,
			&l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	if items == nil {
		items = []Lead{}
	}
	return &ListResponse{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

//encore:api auth method=GET path=/leads/:id
func Get(ctx context.Context, id string) (*GetResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := withTenantDB(ctx, u.TenantSchema)
	if err != nil {
		return nil, err
	}

	var l Lead
	err = conn.QueryRowContext(ctx, `
		SELECT l.id, l.contact_id, l.conversation_id,
		       COALESCE(NULLIF(TRIM(l.name),''), c.display_name, '') AS name,
		       l.status, l.notes, l.created_at, l.updated_at
		FROM leads l
		LEFT JOIN contacts c ON c.id = l.contact_id
		WHERE l.id = $1`, id).Scan(
		&l.ID, &l.ContactID, &l.ConversationID,
		&l.Name, &l.Status, &l.Notes,
		&l.CreatedAt, &l.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, e.NotFound("Lead tidak ditemukan")
	}
	if err != nil {
		return nil, fmt.Errorf("get lead: %w", err)
	}
	return &GetResponse{Lead: l}, nil
}

// CaptureFromMessage auto-creates a lead if one doesn't already exist for the contact.
// Called internally by the AI pipeline or message handlers.
//
//encore:api private method=POST path=/leads/capture
func CaptureFromMessage(ctx context.Context, req *CaptureRequest) (*CaptureResponse, error) {
	if req.TenantSchema == "" || req.ContactID == "" || req.ConversationID == "" {
		return nil, e.BadRequest("tenantSchema, contactId, conversationId required")
	}

	conn, err := withTenantDB(ctx, req.TenantSchema)
	if err != nil {
		return nil, err
	}

	var existingID string
	err = conn.QueryRowContext(ctx, `
		SELECT id FROM leads WHERE contact_id = $1 LIMIT 1`, req.ContactID).Scan(&existingID)
	if err == nil {
		return &CaptureResponse{Created: false, LeadID: existingID}, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing lead: %w", err)
	}

	name := req.ContactName
	if name == "" {
		name = "Unnamed Lead"
	}

	var newID string
	err = conn.QueryRowContext(ctx, `
		INSERT INTO leads (contact_id, conversation_id, name, status)
		VALUES ($1, $2, $3, 'new')
		RETURNING id`,
		req.ContactID, req.ConversationID, name,
	).Scan(&newID)
	if err != nil {
		return nil, fmt.Errorf("insert lead: %w", err)
	}

	rlog.Info("lead captured from message",
		"leadId", newID,
		"contactId", req.ContactID,
		"conversationId", req.ConversationID,
	)
	return &CaptureResponse{Created: true, LeadID: newID}, nil
}
