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
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/shared/strutil"
	"encore.app/wabantu/shared/tenantctx"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsapp"
	"encore.dev"
)

var db = sqldb.Named("tenant")

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// ---- Conversations ----

type ConversationItem struct {
	ID                 string       `json:"id"`
	Status             string       `json:"status"`
	AIHandled          bool         `json:"aiHandled"`
	UnreadCount        int          `json:"unreadCount"`
	LastMessageAt      *time.Time   `json:"lastMessageAt"`
	LastMessagePreview *string      `json:"lastMessagePreview"`
	AssignedToName     *string      `json:"assignedToName"`
	HandoffReason      *string      `json:"handoffReason"`
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
	ID        string `json:"id"`
	Status    string `json:"status"`
	AIHandled bool   `json:"aiHandled"`
}

// ---- Messages ----

type MessageItem struct {
	ID             string            `json:"id"`
	ConversationID string            `json:"conversationId"`
	ExternalID     *string           `json:"externalId"`
	Direction      string            `json:"direction"`
	Author         string            `json:"author"`
	Type           string            `json:"type"`
	Body           *string           `json:"body"`
	Media          *MessageMediaInfo `json:"media,omitempty"`
	LinkedOrderID  *string           `json:"linkedOrderId,omitempty"`
	Status         string            `json:"status"`
	CreatedAt      time.Time         `json:"createdAt"`
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
	BirthDate   *string  `json:"birthDate,omitempty"`
	Notes       *string  `json:"notes"`
	Status      string   `json:"status"`
	PriceTypeID *string  `json:"priceTypeId,omitempty"`
	Tags        []string `json:"tags"`
}

type GetContactResponse struct {
	Contact ContactDetail `json:"contact"`
}

type ListContactsParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type ListContactsResponse struct {
	Items    []ContactDetail `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

type CreateContactParams struct {
	PhoneNumber string   `json:"phoneNumber"`
	DisplayName *string  `json:"displayName,omitempty"`
	BirthDate   *string  `json:"birthDate,omitempty"`
	Notes       *string  `json:"notes,omitempty"`
	Status      string   `json:"status,omitempty"`
	PriceTypeID *string  `json:"priceTypeId,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type UpdateContactParams struct {
	DisplayName *string  `json:"displayName"`
	BirthDate   *string  `json:"birthDate,omitempty"`
	Notes       *string  `json:"notes"`
	Status      *string  `json:"status,omitempty"`
	PriceTypeID *string  `json:"priceTypeId,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type UpdateContactResponse struct {
	Contact ContactDetail `json:"contact"`
}

type BatchContactStatusParams struct {
	IDs    []string `json:"ids"`
	Status string   `json:"status"`
}

type BatchContactStatusResponse struct {
	Updated int `json:"updated"`
}

type BatchContactDeleteParams struct {
	IDs []string `json:"ids"`
}

type BatchContactDeleteResponse struct {
	Deleted int `json:"deleted"`
}

var validContactStatuses = map[string]bool{
	"active":   true,
	"inactive": true,
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
		piiActive, _ := tenantschema.ContactPIIActiveConn(ctx, conn, user.TenantSchema)
		like := "%" + strings.ToLower(strings.TrimSpace(p.Search)) + "%"
		searchParts := []string{fmt.Sprintf(`LOWER(COALESCE(c.last_message_preview,'')) LIKE $%d`, idx)}
		args = append(args, like)
		idx++
		if tenantschema.UseBlindIndexSearch(encKey(), piiActive) {
			key := encKey()
			args = append(args, pii.BlindIndex(pii.NormalizePhone(p.Search), key))
			searchParts = append(searchParts, fmt.Sprintf(`ct.phone_number_idx = $%d`, idx))
			idx++
			args = append(args, pii.BlindIndex(pii.NormalizeName(p.Search), key))
			searchParts = append(searchParts, fmt.Sprintf(`ct.display_name_idx = $%d`, idx))
			idx++
		} else {
			searchParts = append(searchParts,
				fmt.Sprintf(`LOWER(ct.phone_number) LIKE $%d`, idx-1),
				fmt.Sprintf(`LOWER(COALESCE(ct.display_name,'')) LIKE $%d`, idx-1),
			)
		}
		conds = append(conds, "("+strings.Join(searchParts, " OR ")+")")
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
		id, status, contactID, channelID              string
		aiHandled                                     bool
		unreadCount                                   int
		lastMsgAt                                     *time.Time
		lastMsgPreview, assignedToName, handoffReason *string
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
			AssignedToName:     r.assignedToName, HandoffReason: r.handoffReason,
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
			`SELECT m.id, m.conversation_id, m.external_id, m.direction, m.author, m.type, m.body, m.status, m.created_at, m.metadata,
			        (SELECT o.id::text FROM "order" o WHERE o.payment_proof_message_id = m.id AND o.deleted_at IS NULL LIMIT 1)
			 FROM message m
			 WHERE m.conversation_id = $1
			   AND ((m.created_at < $2) OR (m.created_at = $2 AND m.id < $3::uuid))
			 ORDER BY m.created_at DESC, m.id DESC
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
			`SELECT m.id, m.conversation_id, m.external_id, m.direction, m.author, m.type, m.body, m.status, m.created_at, m.metadata,
			        (SELECT o.id::text FROM "order" o WHERE o.payment_proof_message_id = m.id AND o.deleted_at IS NULL LIMIT 1)
			 FROM message m
			 WHERE m.conversation_id = $1
			 ORDER BY m.created_at DESC, m.id DESC
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
		var meta []byte
		var linkedOrder sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.ExternalID,
			&m.Direction, &m.Author, &m.Type, &m.Body, &m.Status, &m.CreatedAt, &meta, &linkedOrder); err != nil {
			continue
		}
		if linkedOrder.Valid && strings.TrimSpace(linkedOrder.String) != "" {
			v := linkedOrder.String
			m.LinkedOrderID = &v
		}
		enrichMessageMedia(&m, json.RawMessage(meta))
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

	contactPhone, err := contactPhoneByID(ctx, conn, convoContactID)
	if err != nil {
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

	preview := strutil.TruncateUTF8(text, 280)
	_, _ = conn.ExecContext(ctx,
		`UPDATE conversation SET last_message_at = $1, last_message_preview = $2, status = 'open' WHERE id = $3`,
		m.CreatedAt, preview, id)

	return &SendMessageResponse{Message: m}, nil
}

// ---------------------------------------------------------------------------
// Endpoints — Contacts
// ---------------------------------------------------------------------------

// ListContacts returns contacts with search and pagination.
//
//encore:api auth method=GET path=/api/v1/inbox/contacts
func ListContacts(ctx context.Context, p *ListContactsParams) (*ListContactsResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 25
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		rlog.Error("ensure contact schema failed", "err", err)
		return nil, apperr.Internal("prepare contacts failed")
	}
	if err := syncContactsFromLeadAndConversation(ctx, conn); err != nil {
		rlog.Warn("sync contacts from lead/conversation failed", "err", err)
	}

	piiActive, _ := tenantschema.ContactPIIActiveConn(ctx, conn, user.TenantSchema)
	args := []any{}
	where := buildContactSearchWhere(strings.TrimSpace(p.Q), piiActive, &args)

	var total int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM contact WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, apperr.Internal("count contacts failed")
	}

	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, p.PageSize, (p.Page-1)*p.PageSize)
	limitParam := len(queryArgs) - 1
	offsetParam := len(queryArgs)
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		`+contactSelectFor(ctx, conn)+`
		WHERE %s
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $%d OFFSET $%d`, where, limitParam, offsetParam), queryArgs...)
	if err != nil {
		return nil, apperr.Internal("list contacts failed")
	}
	defer rows.Close()

	items := []ContactDetail{}
	for rows.Next() {
		c, err := scanContactPII(rows)
		if err != nil {
			return nil, apperr.Internal("scan contact failed")
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Internal("read contacts failed")
	}
	return &ListContactsResponse{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// CreateContact creates or restores a contact by phone number.
//
//encore:api auth method=POST path=/api/v1/inbox/contacts
func CreateContact(ctx context.Context, p *CreateContactParams) (*UpdateContactResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	phone := strings.TrimSpace(p.PhoneNumber)
	if phone == "" {
		return nil, apperr.BadRequest("phoneNumber is required")
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare contacts failed")
	}

	displayName := nullableTrimmed(p.DisplayName)
	notes := nullableTrimmed(p.Notes)
	status := strings.ToLower(strings.TrimSpace(p.Status))
	if status == "" {
		status = "active"
	}
	if !validContactStatuses[status] {
		return nil, apperr.BadRequest("invalid contact status")
	}
	tagsJSON, _ := json.Marshal(cleanTags(p.Tags))
	var birthPtr *string
	if p.BirthDate != nil {
		birthPtr = p.BirthDate
	}
	displayStr := ""
	if displayName != nil {
		displayStr = *displayName
	}
	c, err := upsertContactPII(ctx, conn, phone, displayStr, birthPtr, notes, status, p.PriceTypeID, string(tagsJSON))
	if err != nil {
		return nil, apperr.Internal("create contact failed")
	}
	return &UpdateContactResponse{Contact: c}, nil
}

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
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare contacts failed")
	}

	c, err := scanContactPII(conn.QueryRowContext(ctx, contactSelectFor(ctx, conn)+` WHERE id = $1 AND deleted_at IS NULL`, id))
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
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare contacts failed")
	}

	sets := []string{}
	args := []interface{}{}
	idx := 1

	var piiDisplay, piiBirth *string
	if p.DisplayName != nil {
		piiDisplay = p.DisplayName
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
	if p.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*p.Status))
		if !validContactStatuses[status] {
			return nil, apperr.BadRequest("invalid contact status")
		}
		sets = append(sets, fmt.Sprintf("status = $%d", idx))
		args = append(args, status)
		idx++
	}
	if p.Tags != nil {
		tagsJSON, _ := json.Marshal(cleanTags(p.Tags))
		sets = append(sets, fmt.Sprintf("tags = $%d::jsonb", idx))
		args = append(args, string(tagsJSON))
		idx++
	}
	if p.PriceTypeID != nil {
		sets = append(sets, fmt.Sprintf("price_type_id = $%d", idx))
		args = append(args, nullableUUID(p.PriceTypeID))
		idx++
	}
	if p.BirthDate != nil {
		piiBirth = p.BirthDate
	}
	if len(sets) == 0 && piiDisplay == nil && piiBirth == nil {
		return nil, apperr.BadRequest("no fields to update")
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = NOW()")
		args = append(args, id)
		q := fmt.Sprintf(`UPDATE contact SET %s WHERE id = $%d AND deleted_at IS NULL`,
			strings.Join(sets, ", "), idx)
		res, err := conn.ExecContext(ctx, q, args...)
		if err != nil {
			return nil, apperr.Internal("update failed")
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil, apperr.NotFound("Kontak tidak ditemukan")
		}
	}
	if piiDisplay != nil || piiBirth != nil {
		piiActive, _ := tenantschema.ContactPIIActiveConn(ctx, conn, user.TenantSchema)
		if piiActive && pii.ValidateKey(encKey()) == nil {
			if err := applyContactFieldPII(ctx, conn, id, piiDisplay, piiBirth); err != nil {
				return nil, apperr.Internal("update contact fields failed")
			}
		} else {
			legSets := []string{}
			legArgs := []any{}
			li := 1
			if piiDisplay != nil {
				legSets = append(legSets, fmt.Sprintf("display_name = $%d", li))
				legArgs = append(legArgs, nullableTrimmed(piiDisplay))
				li++
			}
			if piiBirth != nil {
				legSets = append(legSets, fmt.Sprintf("birth_date = $%d", li))
				legArgs = append(legArgs, nullableTrimmed(piiBirth))
				li++
			}
			if len(legSets) > 0 {
				legSets = append(legSets, "updated_at = NOW()")
				legArgs = append(legArgs, id)
				q := fmt.Sprintf(`UPDATE contact SET %s WHERE id = $%d AND deleted_at IS NULL`,
					strings.Join(legSets, ", "), li)
				if _, err := conn.ExecContext(ctx, q, legArgs...); err != nil {
					return nil, apperr.Internal("update contact fields failed")
				}
			}
		}
	}
	c, err := scanContactPII(conn.QueryRowContext(ctx, contactSelectFor(ctx, conn)+` WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return nil, apperr.NotFound("Kontak tidak ditemukan")
	}
	return &UpdateContactResponse{Contact: c}, nil
}

// DeleteContact soft-deletes a contact.
//
//encore:api auth method=DELETE path=/api/v1/inbox/contacts/:id
func DeleteContact(ctx context.Context, id string) error {
	user, err := currentUser()
	if err != nil {
		return err
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return apperr.Internal("database connection failed")
	}
	defer conn.Close()
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		return apperr.Internal("prepare contacts failed")
	}

	uid, _ := auth.UserID()
	res, err := conn.ExecContext(ctx, `
		UPDATE contact
		SET deleted_at = NOW(), deleted_by = $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL`, string(uid), id)
	if err != nil {
		return apperr.Internal("delete contact failed")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperr.NotFound("Kontak tidak ditemukan")
	}
	return nil
}

// BatchUpdateContactStatus updates status for multiple contacts.
//
//encore:api auth method=PATCH path=/api/v1/inbox-contact-status/batch
func BatchUpdateContactStatus(ctx context.Context, p *BatchContactStatusParams) (*BatchContactStatusResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	status := strings.ToLower(strings.TrimSpace(p.Status))
	if !validContactStatuses[status] {
		return nil, apperr.BadRequest("invalid contact status")
	}
	placeholders, args := contactBatchIDs(p.IDs, 2)
	if len(placeholders) == 0 {
		return nil, apperr.BadRequest("ids are required")
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare contacts failed")
	}

	execArgs := []any{status}
	execArgs = append(execArgs, args...)
	res, err := conn.ExecContext(ctx, fmt.Sprintf(`
		UPDATE contact
		SET status = $1, updated_at = NOW()
		WHERE id IN (%s) AND deleted_at IS NULL`, strings.Join(placeholders, ", ")), execArgs...)
	if err != nil {
		return nil, apperr.Internal("batch update contact status failed")
	}
	n, _ := res.RowsAffected()
	return &BatchContactStatusResponse{Updated: int(n)}, nil
}

// BatchDeleteContacts soft-deletes multiple contacts.
//
//encore:api auth method=PATCH path=/api/v1/inbox-contacts/batch-delete
func BatchDeleteContacts(ctx context.Context, p *BatchContactDeleteParams) (*BatchContactDeleteResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	placeholders, args := contactBatchIDs(p.IDs, 2)
	if len(placeholders) == 0 {
		return nil, apperr.BadRequest("ids are required")
	}
	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}
	defer conn.Close()
	if err := ensureContactRuntimeSchema(ctx, conn); err != nil {
		return nil, apperr.Internal("prepare contacts failed")
	}

	uid, _ := auth.UserID()
	execArgs := []any{string(uid)}
	execArgs = append(execArgs, args...)
	res, err := conn.ExecContext(ctx, fmt.Sprintf(`
		UPDATE contact
		SET deleted_at = NOW(), deleted_by = $1, updated_at = NOW()
		WHERE id IN (%s) AND deleted_at IS NULL`, strings.Join(placeholders, ", ")), execArgs...)
	if err != nil {
		return nil, apperr.Internal("batch delete contacts failed")
	}
	n, _ := res.RowsAffected()
	return &BatchContactDeleteResponse{Deleted: int(n)}, nil
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
		`SELECT id,
		        COALESCE(phone_number_enc,''), COALESCE(phone_number,''),
		        COALESCE(display_name_enc,''), COALESCE(display_name,''),
		        tags
		 FROM contact WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return m
	}
	defer rows.Close()
	key := encKey()
	for rows.Next() {
		var id string
		var phoneEnc, phoneLegacy, displayEnc, displayLegacy sql.NullString
		var tagsJSON []byte
		if err := rows.Scan(&id, &phoneEnc, &phoneLegacy, &displayEnc, &displayLegacy, &tagsJSON); err != nil {
			continue
		}
		phone, _ := pii.DecryptOrLegacy(phoneEnc.String, phoneLegacy.String, key)
		display, _ := pii.DecryptOrLegacy(displayEnc.String, displayLegacy.String, key)
		var name *string
		if display != "" && display != pii.Placeholder {
			name = &display
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

func nullableTrimmed(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cleanTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func ensureContactRuntimeSchema(ctx context.Context, conn *sql.Conn) error {
	ready, err := tenantschema.ContactRuntimeReady(ctx, conn)
	if err != nil {
		return err
	}
	if !ready {
		if encore.Meta().Environment.Cloud != encore.CloudLocal {
			var schemaName string
			if err := conn.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schemaName); err != nil {
				return err
			}
			if err := tenant.EnsureCloudAdminTenantDDL(ctx, schemaName); err != nil {
				return err
			}
		} else {
			_, err = conn.ExecContext(ctx, `
				ALTER TABLE contact ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active';
				UPDATE contact SET status = 'active' WHERE status IS NULL OR TRIM(status) = '';
				ALTER TABLE contact ADD COLUMN IF NOT EXISTS price_type_id UUID;
				ALTER TABLE contact ADD COLUMN IF NOT EXISTS birth_date DATE;
			`)
			if err != nil {
				return err
			}
		}
	}
	return ensurePIISchema(ctx, conn)
}

func nullableUUID(value *string) any {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil
	}
	return v
}

func nullableDate(value *string) any {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil
	}
	return v
}

func syncContactsFromLeadAndConversation(ctx context.Context, conn *sql.Conn) error {
	if err := pii.ValidateKey(encKey()); err == nil {
		return nil
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO contact (phone_number, display_name, notes, status, tags, created_at, updated_at)
		SELECT DISTINCT ON (l.phone_number)
		       l.phone_number,
		       NULLIF(TRIM(l.name), ''),
		       NULLIF(TRIM(l.notes), ''),
		       'active',
		       '["lead"]'::jsonb,
		       l.created_at,
		       l.updated_at
		FROM lead l
		WHERE l.deleted_at IS NULL
		  AND NULLIF(TRIM(l.phone_number), '') IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM contact c WHERE c.phone_number = l.phone_number
		  )
		ORDER BY l.phone_number, l.updated_at DESC
	`); err != nil {
		return err
	}

	_, err := conn.ExecContext(ctx, `
		UPDATE contact c
		SET deleted_at = NULL,
		    deleted_by = NULL,
		    status = COALESCE(NULLIF(TRIM(c.status), ''), 'active'),
		    updated_at = NOW()
		WHERE c.deleted_at IS NOT NULL
		  AND EXISTS (
		      SELECT 1
		      FROM conversation conv
		      WHERE conv.contact_id = c.id
		        AND conv.deleted_at IS NULL
		  )
	`)
	return err
}

func contactBatchIDs(ids []string, start int) ([]string, []any) {
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+len(args)))
		args = append(args, id)
	}
	return placeholders, args
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
