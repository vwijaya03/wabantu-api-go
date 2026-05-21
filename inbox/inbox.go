// Package inbox is an Encore service providing conversation, message, and
// contact management for the WABantu inbox.
package inbox

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/tenantctx"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/whatsapp"
)

var db = sqldb.Named("tenant")

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// ---- Conversations ----

type ConversationItem struct {
	ID                 string     `json:"id"`
	Status             string     `json:"status"`
	AIHandled          bool       `json:"aiHandled"`
	UnreadCount        int        `json:"unreadCount"`
	LastMessageAt      *time.Time `json:"lastMessageAt"`
	LastMessagePreview *string    `json:"lastMessagePreview"`
	AssignedToName     *string    `json:"assignedToName"`
	HandoffReason      *string    `json:"handoffReason"`
	Contact            ContactBrief `json:"contact"`
	Channel            ChannelBrief `json:"channel"`
}

type ContactBrief struct {
	ID          string   `json:"id"`
	DisplayName *string  `json:"displayName"`
	PhoneNumber string   `json:"phoneNumber"`
	Tags        []string `json:"tags"`
}

type ChannelBrief struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	PhoneNumber string `json:"phoneNumber"`
}

type ListConversationsParams struct {
	Search     string `query:"search"`
	UnreadOnly string `query:"unreadOnly"` // "true" to filter unread only
	AIHandled  string `query:"aiHandled"`  // "true" | "false"; empty = no filter
	Limit      int    `query:"limit"`
	Cursor     string `query:"cursor"`
}

type ListConversationsResponse struct {
	Items      []ConversationItem `json:"items"`
	NextCursor *string            `json:"nextCursor"`
}

type UpdateConversationParams struct {
	Status     *string `json:"status"`
	AIHandled  *bool   `json:"aiHandled"`
	AssignedTo *string `json:"assignedTo"`
}

type UpdateConversationResponse struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	AIHandled bool    `json:"aiHandled"`
}

// ---- Messages ----

type MessageItem struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	ExternalID     *string   `json:"externalId"`
	Direction      string    `json:"direction"`
	Author         string    `json:"author"`
	Type           string    `json:"type"`
	Body           *string   `json:"body"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
}

type GetMessagesParams struct {
	Limit  int    `query:"limit"`
	Offset int    `query:"offset"`
	Cursor string `query:"cursor"`
}

type GetMessagesResponse struct {
	Messages   []MessageItem `json:"messages"`
	NextCursor *string       `json:"nextCursor"`
	NextOffset *int          `json:"nextOffset"`
}

type SendMessageParams struct {
	Body string `json:"body"`
}

type SendMessageResponse struct {
	Message MessageItem `json:"message"`
}

// ---- Contacts ----

type ContactDetail struct {
	ID          string   `json:"id"`
	PhoneNumber string   `json:"phoneNumber"`
	DisplayName *string  `json:"displayName"`
	Notes       *string  `json:"notes"`
	Tags        []string `json:"tags"`
}

type GetContactResponse struct {
	Contact ContactDetail `json:"contact"`
}

type UpdateContactParams struct {
	DisplayName *string `json:"displayName"`
	Notes       *string `json:"notes"`
}

type UpdateContactResponse struct {
	Contact ContactDetail `json:"contact"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func currentUser() (*types.AuthUser, error) {
	data, ok := auth.Data().(*types.AuthUser)
	if !ok || data == nil {
		return nil, apperr.Unauthenticated("not authenticated")
	}
	return data, nil
}

func tConn(ctx context.Context, schema string) (*sql.Conn, error) {
	return appdb.TenantConn(ctx, db.Stdlib(), schema)
}

func tenantConn(ctx context.Context, user *types.AuthUser) (*sql.Conn, error) {
	return tenantctx.Conn(ctx, db.Stdlib(), user)
}

func queryBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func queryBoolOptional(s string) (bool, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, false
	}
	return queryBool(s), true
}

func clampLimit(raw int, fallback, max int) int {
	if raw == 0 {
		return fallback
	}
	if raw < 1 {
		return 1
	}
	if raw > max {
		return max
	}
	return raw
}

// ---------------------------------------------------------------------------
// Cursor helpers (base64-encoded JSON)
// ---------------------------------------------------------------------------

type cursorData struct {
	LastMessageAt *string `json:"lm,omitempty"`
	CreatedAt     string  `json:"ca,omitempty"`
	ID            string  `json:"id"`
}

func encodeCursor(d cursorData) string {
	b, _ := json.Marshal(d)
	return base64.URLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursorData, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return cursorData{}, err
	}
	var d cursorData
	return d, json.Unmarshal(b, &d)
}

// ---------------------------------------------------------------------------
// Endpoints — Conversations
// ---------------------------------------------------------------------------

// ListConversations returns a paginated list of conversations with last-message
// preview, contact, and channel info.
//
//encore:api auth method=GET path=/api/v1/inbox/conversations
func ListConversations(ctx context.Context, p *ListConversationsParams) (*ListConversationsResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	limit := clampLimit(p.Limit, 30, 100)
	hasSearch := strings.TrimSpace(p.Search) != ""

	q := `SELECT c.id, c.status, c.ai_handled, c.unread_count,
	             c.last_message_at, c.last_message_preview,
	             c.assigned_to_name, c.handoff_reason,
	             c.contact_id, c.channel_id
	      FROM conversation c`

	if hasSearch {
		q += ` LEFT JOIN contact ct ON ct.id = c.contact_id
		       LEFT JOIN whatsapp_channel ch ON ch.id = c.channel_id`
	}

	var conds []string
	var args []interface{}
	idx := 1

	if hasSearch {
		like := "%" + strings.ToLower(strings.TrimSpace(p.Search)) + "%"
		conds = append(conds, fmt.Sprintf(`(
			LOWER(ct.phone_number) LIKE $%d OR
			LOWER(COALESCE(ct.display_name,'')) LIKE $%d OR
			LOWER(COALESCE(c.last_message_preview,'')) LIKE $%d
		)`, idx, idx, idx))
		args = append(args, like)
		idx++
	}
	if queryBool(p.UnreadOnly) {
		conds = append(conds, "c.unread_count > 0")
	}
	if ai, ok := queryBoolOptional(p.AIHandled); ok {
		conds = append(conds, fmt.Sprintf("c.ai_handled = $%d", idx))
		args = append(args, ai)
		idx++
	}
	if strings.TrimSpace(p.Cursor) != "" {
		if cur, cErr := decodeCursor(p.Cursor); cErr == nil && cur.ID != "" {
			conds = append(conds, fmt.Sprintf(
				`(COALESCE(c.last_message_at, '-infinity'::timestamptz), c.id) < `+
					`(COALESCE($%d::timestamptz, '-infinity'::timestamptz), $%d::uuid)`,
				idx, idx+1))
			args = append(args, cur.LastMessageAt, cur.ID)
			idx += 2
		}
	}

	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += fmt.Sprintf(` ORDER BY c.last_message_at DESC NULLS LAST, c.id DESC LIMIT $%d`, idx)
	args = append(args, limit+1)

	rows, qErr := conn.QueryContext(ctx, q, args...)
	if qErr != nil {
		rlog.Error("list conversations failed", "err", qErr)
		return nil, apperr.Internal("failed to list conversations")
	}
	defer rows.Close()

	type row struct {
		id, status, contactID, channelID                        string
		aiHandled                                               bool
		unreadCount                                             int
		lastMsgAt                                               *time.Time
		lastMsgPreview, assignedToName, handoffReason           *string
	}
	var rr []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.status, &r.aiHandled, &r.unreadCount,
			&r.lastMsgAt, &r.lastMsgPreview, &r.assignedToName,
			&r.handoffReason, &r.contactID, &r.channelID); err != nil {
			continue
		}
		rr = append(rr, r)
	}

	hasMore := len(rr) > limit
	if hasMore {
		rr = rr[:limit]
	}
	if len(rr) == 0 {
		return &ListConversationsResponse{Items: []ConversationItem{}}, nil
	}

	contactMap := loadContacts(ctx, conn, uniqueIDs(rr, func(r row) string { return r.contactID }))
	channelMap := loadChannels(ctx, conn, uniqueIDs(rr, func(r row) string { return r.channelID }))

	items := make([]ConversationItem, len(rr))
	for i, r := range rr {
		items[i] = ConversationItem{
			ID: r.id, Status: r.status, AIHandled: r.aiHandled,
			UnreadCount: r.unreadCount, LastMessageAt: r.lastMsgAt,
			LastMessagePreview: r.lastMsgPreview,
			AssignedToName: r.assignedToName, HandoffReason: r.handoffReason,
			Contact: contactMap[r.contactID],
			Channel: channelMap[r.channelID],
		}
	}

	var nextCursor *string
	if hasMore {
		tail := rr[len(rr)-1]
		var lm *string
		if tail.lastMsgAt != nil {
			s := tail.lastMsgAt.Format(time.RFC3339Nano)
			lm = &s
		}
		c := encodeCursor(cursorData{LastMessageAt: lm, ID: tail.id})
		nextCursor = &c
	}
	return &ListConversationsResponse{Items: items, NextCursor: nextCursor}, nil
}

// UpdateConversation patches a conversation's status, aiHandled, or assignment.
//
//encore:api auth method=PATCH path=/api/v1/inbox/conversations/:id
func UpdateConversation(ctx context.Context, id string, p *UpdateConversationParams) (*UpdateConversationResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	sets := []string{}
	args := []interface{}{}
	idx := 1

	if p.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", idx))
		args = append(args, *p.Status)
		idx++
	}
	if p.AIHandled != nil {
		sets = append(sets, fmt.Sprintf("ai_handled = $%d", idx))
		args = append(args, *p.AIHandled)
		idx++
		if !*p.AIHandled {
			sets = append(sets, "ai_paused_at = NOW()")
			sets = append(sets, fmt.Sprintf("assigned_to_user_id = $%d", idx))
			args = append(args, user.AccountID)
			idx++
			sets = append(sets, fmt.Sprintf("assigned_to_name = $%d", idx))
			args = append(args, user.Email)
			idx++
		} else {
			sets = append(sets, "ai_paused_at = NULL", "assigned_to_user_id = NULL",
				"assigned_to_name = NULL", "handoff_reason = NULL")
		}
	}
	if p.AssignedTo != nil {
		sets = append(sets, fmt.Sprintf("assigned_to_name = $%d", idx))
		args = append(args, *p.AssignedTo)
		idx++
	}

	if len(sets) == 0 {
		return nil, apperr.BadRequest("no fields to update")
	}

	args = append(args, id)
	q := fmt.Sprintf(`UPDATE conversation SET %s WHERE id = $%d RETURNING id, status, ai_handled`,
		strings.Join(sets, ", "), idx)

	var resp UpdateConversationResponse
	if err := conn.QueryRowContext(ctx, q, args...).Scan(&resp.ID, &resp.Status, &resp.AIHandled); err != nil {
		if err == sql.ErrNoRows {
			return nil, apperr.NotFound("Percakapan tidak ditemukan")
		}
		return nil, apperr.Internal("update failed")
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Endpoints — Messages
// ---------------------------------------------------------------------------

// GetMessages returns paginated messages for a conversation (newest-first fetch,
// reversed to chronological order in response).
//
//encore:api auth method=GET path=/api/v1/inbox/conversations/:id/messages
func GetMessages(ctx context.Context, id string, p *GetMessagesParams) (*GetMessagesResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	take := clampLimit(p.Limit, 50, 100)

	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation WHERE id = $1)`, id,
	).Scan(&exists); err != nil || !exists {
		return nil, apperr.NotFound("Percakapan tidak ditemukan")
	}

	useKeyset := strings.TrimSpace(p.Cursor) != ""

	var rows *sql.Rows
	var queryErr error
	offset := 0

	if useKeyset {
		cur, cErr := decodeCursor(p.Cursor)
		if cErr != nil || cur.ID == "" || cur.CreatedAt == "" {
			return nil, apperr.BadRequest("Cursor pesan tidak valid.")
		}
		cursorAt, tErr := time.Parse(time.RFC3339Nano, cur.CreatedAt)
		if tErr != nil {
			return nil, apperr.BadRequest("Cursor pesan tidak valid.")
		}
		rows, queryErr = conn.QueryContext(ctx,
			`SELECT id, conversation_id, external_id, direction, author, type, body, status, created_at
			 FROM message
			 WHERE conversation_id = $1
			   AND ((created_at < $2) OR (created_at = $2 AND id < $3::uuid))
			 ORDER BY created_at DESC, id DESC
			 LIMIT $4`,
			id, cursorAt, cur.ID, take+1)
	} else {
		if p.Offset > 0 {
			offset = p.Offset
			if offset > 500_000 {
				offset = 500_000
			}
		}
		rows, queryErr = conn.QueryContext(ctx,
			`SELECT id, conversation_id, external_id, direction, author, type, body, status, created_at
			 FROM message
			 WHERE conversation_id = $1
			 ORDER BY created_at DESC, id DESC
			 LIMIT $2 OFFSET $3`,
			id, take+1, offset)
	}
	if queryErr != nil {
		rlog.Error("get messages failed", "err", queryErr)
		return nil, apperr.Internal("failed to load messages")
	}
	defer rows.Close()

	var msgs []MessageItem
	for rows.Next() {
		var m MessageItem
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.ExternalID,
			&m.Direction, &m.Author, &m.Type, &m.Body, &m.Status, &m.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}

	hasMore := len(msgs) > take
	if hasMore {
		msgs = msgs[:take]
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	var nextCursor *string
	var nextOffset *int
	if hasMore && len(msgs) > 0 {
		oldest := msgs[0]
		c := encodeCursor(cursorData{CreatedAt: oldest.CreatedAt.Format(time.RFC3339Nano), ID: oldest.ID})
		nextCursor = &c
		if !useKeyset {
			n := offset + take
			nextOffset = &n
		}
	}

	if msgs == nil {
		msgs = []MessageItem{}
	}
	return &GetMessagesResponse{Messages: msgs, NextCursor: nextCursor, NextOffset: nextOffset}, nil
}

// SendMessage sends a human (staff/owner) reply through the WhatsApp channel
// and persists it.
//
//encore:api auth method=POST path=/api/v1/inbox/conversations/:id/messages
func SendMessage(ctx context.Context, id string, p *SendMessageParams) (*SendMessageResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(p.Body)
	if text == "" {
		return nil, apperr.BadRequest("Isi pesan tidak boleh kosong")
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	var convoContactID, convoChannelID string
	if err := conn.QueryRowContext(ctx,
		`SELECT contact_id, channel_id FROM conversation WHERE id = $1`, id,
	).Scan(&convoContactID, &convoChannelID); err != nil {
		return nil, apperr.NotFound("Percakapan tidak ditemukan")
	}

	var contactPhone string
	if err := conn.QueryRowContext(ctx,
		`SELECT phone_number FROM contact WHERE id = $1`, convoContactID,
	).Scan(&contactPhone); err != nil {
		return nil, apperr.NotFound("Kontak tidak ditemukan")
	}

	var chStatus, chAccessToken, chProvider string
	var chMetaPhoneNumberID *string
	if err := conn.QueryRowContext(ctx,
		`SELECT status, COALESCE(access_token,''), provider, meta_phone_number_id
		 FROM whatsapp_channel WHERE id = $1`, convoChannelID,
	).Scan(&chStatus, &chAccessToken, &chProvider, &chMetaPhoneNumberID); err != nil {
		return nil, apperr.NotFound("Channel tidak ditemukan")
	}
	if chStatus != "connected" || chAccessToken == "" {
		return nil, apperr.BadRequest("Channel WhatsApp belum terhubung. Silakan reconnect.")
	}
	if chProvider != "meta_cloud" {
		return nil, apperr.BadRequest("Provider channel belum didukung")
	}
	if chMetaPhoneNumberID == nil || *chMetaPhoneNumberID == "" {
		return nil, apperr.BadRequest("Channel belum memiliki meta_phone_number_id")
	}

	extID, err := whatsapp.SendText(ctx, chAccessToken, *chMetaPhoneNumberID, contactPhone, text)
	if err != nil {
		rlog.Error("send whatsapp message failed", "err", err)
		return nil, apperr.Unavailable("Gagal mengirim pesan WhatsApp")
	}

	var m MessageItem
	if err := conn.QueryRowContext(ctx,
		`INSERT INTO message (conversation_id, external_id, direction, author, type, body, metadata, status)
		 VALUES ($1, $2, 'out', 'human', 'text', $3, '{}'::jsonb, 'sent')
		 RETURNING id, conversation_id, external_id, direction, author, type, body, status, created_at`,
		id, extID, text,
	).Scan(&m.ID, &m.ConversationID, &m.ExternalID,
		&m.Direction, &m.Author, &m.Type, &m.Body, &m.Status, &m.CreatedAt); err != nil {
		return nil, apperr.Internal("failed to save message")
	}

	preview := text
	if len(preview) > 280 {
		preview = preview[:280]
	}
	_, _ = conn.ExecContext(ctx,
		`UPDATE conversation SET last_message_at = $1, last_message_preview = $2, status = 'open' WHERE id = $3`,
		m.CreatedAt, preview, id)

	return &SendMessageResponse{Message: m}, nil
}

// ---------------------------------------------------------------------------
// Endpoints — Contacts
// ---------------------------------------------------------------------------

// GetContact returns a single contact's details.
//
//encore:api auth method=GET path=/api/v1/inbox/contacts/:id
func GetContact(ctx context.Context, id string) (*GetContactResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	c, err := scanContact(conn.QueryRowContext(ctx,
		`SELECT id, phone_number, display_name, notes, tags FROM contact WHERE id = $1`, id))
	if err != nil {
		return nil, apperr.NotFound("Kontak tidak ditemukan")
	}
	return &GetContactResponse{Contact: c}, nil
}

// UpdateContact patches a contact's display name or notes.
//
//encore:api auth method=PATCH path=/api/v1/inbox/contacts/:id
func UpdateContact(ctx context.Context, id string, p *UpdateContactParams) (*UpdateContactResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()

	sets := []string{}
	args := []interface{}{}
	idx := 1

	if p.DisplayName != nil {
		v := strings.TrimSpace(*p.DisplayName)
		var store *string
		if v != "" {
			store = &v
		}
		sets = append(sets, fmt.Sprintf("display_name = $%d", idx))
		args = append(args, store)
		idx++
	}
	if p.Notes != nil {
		v := strings.TrimSpace(*p.Notes)
		var store *string
		if v != "" {
			store = &v
		}
		sets = append(sets, fmt.Sprintf("notes = $%d", idx))
		args = append(args, store)
		idx++
	}
	if len(sets) == 0 {
		return nil, apperr.BadRequest("no fields to update")
	}

	args = append(args, id)
	q := fmt.Sprintf(`UPDATE contact SET %s WHERE id = $%d
		RETURNING id, phone_number, display_name, notes, tags`,
		strings.Join(sets, ", "), idx)

	c, err := scanContact(conn.QueryRowContext(ctx, q, args...))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperr.NotFound("Kontak tidak ditemukan")
		}
		return nil, apperr.Internal("update failed")
	}
	return &UpdateContactResponse{Contact: c}, nil
}

// ---------------------------------------------------------------------------
// Batch loaders
// ---------------------------------------------------------------------------

func loadContacts(ctx context.Context, conn *sql.Conn, ids []string) map[string]ContactBrief {
	m := make(map[string]ContactBrief, len(ids))
	if len(ids) == 0 {
		return m
	}
	placeholders, args := inClause(ids)
	rows, err := conn.QueryContext(ctx,
		`SELECT id, phone_number, display_name, tags FROM contact WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id, phone string
		var name *string
		var tagsJSON []byte
		if err := rows.Scan(&id, &phone, &name, &tagsJSON); err != nil {
			continue
		}
		var tags []string
		_ = json.Unmarshal(tagsJSON, &tags)
		if tags == nil {
			tags = []string{}
		}
		m[id] = ContactBrief{ID: id, PhoneNumber: phone, DisplayName: name, Tags: tags}
	}
	return m
}

func loadChannels(ctx context.Context, conn *sql.Conn, ids []string) map[string]ChannelBrief {
	m := make(map[string]ChannelBrief, len(ids))
	if len(ids) == 0 {
		return m
	}
	placeholders, args := inClause(ids)
	rows, err := conn.QueryContext(ctx,
		`SELECT id, display_name, phone_number FROM whatsapp_channel WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var c ChannelBrief
		if err := rows.Scan(&c.ID, &c.DisplayName, &c.PhoneNumber); err != nil {
			continue
		}
		m[c.ID] = c
	}
	return m
}

func scanContact(scanner interface{ Scan(...any) error }) (ContactDetail, error) {
	var c ContactDetail
	var tagsJSON []byte
	err := scanner.Scan(&c.ID, &c.PhoneNumber, &c.DisplayName, &c.Notes, &tagsJSON)
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal(tagsJSON, &c.Tags)
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// SQL helpers
// ---------------------------------------------------------------------------

func inClause(ids []string) (string, []interface{}) {
	parts := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	return strings.Join(parts, ","), args
}

func uniqueIDs[T any](items []T, fn func(T) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		id := fn(item)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
