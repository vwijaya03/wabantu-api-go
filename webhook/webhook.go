// Package webhook is an Encore service that handles inbound WhatsApp webhooks
// from Meta Cloud API.  It verifies signatures, parses messages, upserts
// contacts/conversations, persists messages, and publishes AI job events.
package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/workflow"
	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/leads"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/inboxrealtime"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsapp"
)

var db = sqldb.Named("tenant")

var secrets struct {
	WebhookVerifyToken string
	MetaAppSecret      string
}

// ---------------------------------------------------------------------------
// Raw HTTP handler (Meta requires exact challenge response for GET)
// ---------------------------------------------------------------------------

// HandleWhatsAppWebhook handles both the Meta verification challenge (GET)
// and inbound webhook events (POST).
//
//encore:api public raw path=/api/v1/webhook/whatsapp
func HandleWhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	handleMetaWebhook(w, r)
}

// HandleMetaWebhook is the Nest-compatible Meta webhook path (/whatsapp/webhook/meta).
//
//encore:api public raw path=/api/v1/whatsapp/webhook/meta
func HandleMetaWebhook(w http.ResponseWriter, r *http.Request) {
	handleMetaWebhook(w, r)
}

// Legacy paths (Meta apps configured before /api/v1 prefix).
//
//encore:api public raw path=/whatsapp/webhook/meta
func HandleMetaWebhookLegacy(w http.ResponseWriter, r *http.Request) {
	handleMetaWebhook(w, r)
}

//encore:api public raw path=/webhook/whatsapp
func HandleWhatsAppWebhookLegacy(w http.ResponseWriter, r *http.Request) {
	handleMetaWebhook(w, r)
}

func handleMetaWebhook(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		verifyChallenge(w, r)
	case http.MethodPost:
		receiveWebhook(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func verifyChallenge(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("hub.mode") == "subscribe" && q.Get("hub.verify_token") == secrets.WebhookVerifyToken {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(q.Get("hub.challenge")))
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

func receiveWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if secrets.MetaAppSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig != "" && !whatsapp.VerifyWebhookSignature(body, sig, secrets.MetaAppSecret) {
			rlog.Warn("webhook signature verification failed")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	messages := whatsapp.ParseWebhook(body)
	ctx := r.Context()
	for _, msg := range messages {
		if err := ingestMessage(ctx, msg); err != nil {
			rlog.Warn("ingest failed", "externalId", msg.ExternalID, "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"received": true})
}

// ---------------------------------------------------------------------------
// Message ingestion
// ---------------------------------------------------------------------------

func ingestMessage(ctx context.Context, msg whatsapp.InboundMessage) error {
	resolved, err := resolveInboundChannel(ctx, msg.ToPhoneNumberID, msg.ToDisplayPhone)
	if err != nil {
		return fmt.Errorf("resolve tenant: %w", err)
	}
	schema := resolved.Schema
	channelID := resolved.ChannelID

	conn, err := appdb.TenantConn(ctx, db.Stdlib(), schema)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	// Idempotent: skip if message already stored
	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM message WHERE external_id = $1)`,
		msg.ExternalID).Scan(&exists); err != nil {
		return fmt.Errorf("dup check: %w", err)
	}
	if exists {
		return nil
	}

	contactID, err := upsertContact(ctx, conn, msg)
	if err != nil {
		return fmt.Errorf("upsert contact: %w", err)
	}

	convoID, err := upsertConversation(ctx, conn, channelID, contactID)
	if err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}

	messageID, err := insertMessage(ctx, conn, convoID, msg)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	preview := msg.Body
	if preview == "" {
		preview = msg.Type
	}
	if len(preview) > 280 {
		preview = preview[:280]
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE conversation
		 SET unread_count = unread_count + 1,
		     last_message_at = NOW(),
		     last_message_preview = $1,
		     status = 'open'
		 WHERE id = $2`,
		preview, convoID); err != nil {
		return fmt.Errorf("update conversation: %w", err)
	}

	handled, wfErr := workflow.TryRun(ctx, schema, convoID, msg.Body)
	if wfErr != nil {
		rlog.Warn("workflow evaluation failed", "err", wfErr)
	}
	if !handled {
		if pubErr := ai.PublishInboundJob(ctx, &ai.InboundAIJob{
			TenantSchema:     schema,
			ConversationID:   convoID,
			InboundMessageID: messageID,
			InboundType:      msg.Type,
		}); pubErr != nil {
			rlog.Warn("publish AI job failed", "err", pubErr, "messageId", messageID)
		}
	}

	if ok, sErr := ai.ShouldTriggerSummary(ctx, schema, convoID); sErr == nil && ok {
		if _, pubErr := ai.SummarizeTopic.Publish(ctx, ai.SummarizeRequest{
			TenantSchema:   schema,
			ConversationID: convoID,
		}); pubErr != nil {
			rlog.Warn("publish summarize job failed", "err", pubErr)
		}
	}

	if tenantID, tErr := tenant.TenantIDBySchema(ctx, schema); tErr == nil && tenantID != "" {
		inboxrealtime.Publish(ctx, appauth.RedisClient(), tenantID)
	}

	var contactPhone string
	_ = conn.QueryRowContext(ctx,
		`SELECT phone_number FROM contact WHERE id = $1`, contactID,
	).Scan(&contactPhone)

	_, _ = leads.CaptureFromMessage(ctx, &leads.CaptureRequest{
		TenantSchema:   schema,
		ContactID:      contactID,
		ConversationID: convoID,
		ContactName:    msg.FromDisplayName,
		PhoneNumber:    contactPhone,
		Body:           msg.Body,
	})

	return nil
}

// ---------------------------------------------------------------------------
// DB helpers
// ---------------------------------------------------------------------------

func upsertContact(ctx context.Context, conn *sql.Conn, msg whatsapp.InboundMessage) (string, error) {
	phone := normalizePhone(msg.FromPhone)
	var id string
	err := conn.QueryRowContext(ctx,
		`SELECT id FROM contact WHERE phone_number = $1`, phone).Scan(&id)

	if err == sql.ErrNoRows {
		var displayName *string
		if n := strings.TrimSpace(msg.FromDisplayName); n != "" {
			displayName = &n
		}
		return id, conn.QueryRowContext(ctx,
			`INSERT INTO contact (phone_number, display_name, tags)
			 VALUES ($1, $2, '["new"]'::jsonb)
			 RETURNING id`,
			phone, displayName).Scan(&id)
	}
	if err != nil {
		return "", err
	}

	if n := strings.TrimSpace(msg.FromDisplayName); n != "" {
		_, _ = conn.ExecContext(ctx,
			`UPDATE contact SET display_name = $1
			 WHERE id = $2 AND (display_name IS NULL OR TRIM(display_name) = '')`,
			truncStr(n, 200), id)
	}
	return id, nil
}

func upsertConversation(ctx context.Context, conn *sql.Conn, channelID, contactID string) (string, error) {
	var id string
	err := conn.QueryRowContext(ctx,
		`SELECT id FROM conversation WHERE channel_id = $1 AND contact_id = $2`,
		channelID, contactID).Scan(&id)

	if err == sql.ErrNoRows {
		return id, conn.QueryRowContext(ctx,
			`INSERT INTO conversation (channel_id, contact_id, status, ai_handled, unread_count)
			 VALUES ($1, $2, 'open', true, 0)
			 RETURNING id`,
			channelID, contactID).Scan(&id)
	}
	return id, err
}

func insertMessage(ctx context.Context, conn *sql.Conn, convoID string, msg whatsapp.InboundMessage) (string, error) {
	rawJSON := string(msg.Raw)
	var id string
	err := conn.QueryRowContext(ctx,
		`INSERT INTO message (conversation_id, external_id, direction, author, type, body, metadata, status)
		 VALUES ($1, $2, 'in', 'contact', $3, $4, $5::jsonb, 'delivered')
		 RETURNING id`,
		convoID, msg.ExternalID, msg.Type, msg.Body, rawJSON).Scan(&id)
	return id, err
}

// ---------------------------------------------------------------------------
// Tenant resolution
// ---------------------------------------------------------------------------

type inboundChannel struct {
	Schema    string
	ChannelID string
}

// resolveInboundChannel finds the tenant schema and whatsapp_channel row for an
// inbound Meta webhook (phone_number_id + optional display_phone_number).
// Matches Nest resolveTenantByInboundAddress: prefer meta_phone_number_id,
// then normalized business phone, and backfill meta_phone_number_id when missing.
func resolveInboundChannel(ctx context.Context, phoneNumberID, displayPhone string) (*inboundChannel, error) {
	schemas, err := tenant.ListSchemaNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant schemas: %w", err)
	}
	if len(schemas) == 0 {
		return nil, fmt.Errorf("no tenant schemas registered")
	}

	pool := db.Stdlib()
	phoneNumberID = strings.TrimSpace(phoneNumberID)
	normDisplay := normalizePhone(displayPhone)

	for _, schema := range schemas {
		var id string
		err := pool.QueryRowContext(ctx,
			fmt.Sprintf(
				`SELECT id FROM %s.whatsapp_channel WHERE meta_phone_number_id = $1 LIMIT 1`,
				appdb.QuoteIdent(schema)),
			phoneNumberID,
		).Scan(&id)
		if err == nil {
			return &inboundChannel{Schema: schema, ChannelID: id}, nil
		}
	}

	for _, schema := range schemas {
		var id string
		var existingMeta sql.NullString
		q := fmt.Sprintf(`
			SELECT id, meta_phone_number_id FROM %s.whatsapp_channel
			WHERE ($1 <> '' AND REGEXP_REPLACE(phone_number, '[^0-9]', '', 'g') = $1)
			   OR ($2 <> '' AND phone_number = $2)
			LIMIT 1`, appdb.QuoteIdent(schema))
		err := pool.QueryRowContext(ctx, q, normDisplay, strings.TrimSpace(displayPhone)).Scan(&id, &existingMeta)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			continue
		}
		if phoneNumberID != "" && (!existingMeta.Valid || strings.TrimSpace(existingMeta.String) == "") {
			_, _ = pool.ExecContext(ctx,
				fmt.Sprintf(
					`UPDATE %s.whatsapp_channel SET meta_phone_number_id = $1, updated_at = NOW() WHERE id = $2`,
					appdb.QuoteIdent(schema)),
				phoneNumberID, id)
			rlog.Info("backfilled meta_phone_number_id from webhook",
				"schema", schema, "channelId", id, "phoneNumberId", phoneNumberID)
		}
		return &inboundChannel{Schema: schema, ChannelID: id}, nil
	}

	return nil, fmt.Errorf(
		"no tenant channel for phone_number_id=%s display=%s — pastikan whatsapp_channel.meta_phone_number_id terisi atau phone_number bisnis cocok dengan OAuth",
		phoneNumberID, displayPhone)
}

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

func normalizePhone(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
