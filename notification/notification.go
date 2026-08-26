package notification

import (
	"context"
	"database/sql"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("system")

// Notification is an in-app notification row.
type Notification struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	LinkPath  string     `json:"linkPath,omitempty"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type ListResponse struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unreadCount"`
}

type ListParams struct {
	Limit int `query:"limit"`
}

type MarkReadResponse struct {
	OK bool `json:"ok"`
}

// CreateForAccounts inserts the same notification for multiple accounts.
func CreateForAccounts(ctx context.Context, accountIDs []string, kind, title, body, linkPath string) error {
	if len(accountIDs) == 0 {
		return nil
	}
	for _, accountID := range accountIDs {
		if accountID == "" {
			continue
		}
		_, err := db.Exec(ctx, `
			INSERT INTO app_notification (account_id, kind, title, body, link_path)
			VALUES ($1, $2, $3, $4, $5)`,
			accountID, kind, title, body, linkPath)
		if err != nil {
			return err
		}
	}
	return nil
}

//encore:api auth method=GET path=/api/v1/notifications
func List(ctx context.Context, p *ListParams) (*ListResponse, error) {
	u, err := requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	limit := 50
	if p != nil && p.Limit > 0 && p.Limit <= 100 {
		limit = p.Limit
	}

	var unread int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM app_notification
		WHERE account_id = $1 AND read_at IS NULL`, u.AccountID,
	).Scan(&unread); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "count notifikasi gagal"}
	}

	rows, err := db.Query(ctx, `
		SELECT id, kind, title, COALESCE(body, ''), COALESCE(link_path, ''),
			read_at, created_at
		FROM app_notification
		WHERE account_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, u.AccountID, limit)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "list notifikasi gagal"}
	}
	defer rows.Close()

	list := make([]Notification, 0)
	for rows.Next() {
		var n Notification
		var readAt sql.NullTime
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.LinkPath, &readAt, &n.CreatedAt); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "scan notifikasi gagal"}
		}
		if readAt.Valid {
			t := readAt.Time
			n.ReadAt = &t
		}
		list = append(list, n)
	}
	if err := rows.Err(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "iterasi notifikasi gagal"}
	}

	return &ListResponse{Notifications: list, UnreadCount: unread}, nil
}

//encore:api auth method=POST path=/api/v1/notifications/:id/read
func MarkRead(ctx context.Context, id string) (*MarkReadResponse, error) {
	u, err := requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(ctx, `
		UPDATE app_notification
		SET read_at = NOW()
		WHERE id = $1 AND account_id = $2 AND read_at IS NULL`,
		id, u.AccountID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "tandai baca gagal"}
	}
	return &MarkReadResponse{OK: true}, nil
}

func requireAuth(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	return u, nil
}
