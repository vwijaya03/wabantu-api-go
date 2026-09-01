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
	"encore.app/wabantu/inbox"
	"encore.app/wabantu/workflow"
	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/leads"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/inboxrealtime"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/strutil"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsapp"
)

var db = sqldb.Named("tenant")

func openTenantScope(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	return appdb.OpenTenantScope(db.Stdlib(), schema), nil
}

var secrets struct {
	WebhookVerifyToken string
	DataEncryptionKey  string
}

// ---------------------------------------------------------------------------
// Raw HTTP handler (Meta requires exact challenge response for GET)
// ---------------------------------------------------------------------------

// HandleWhatsAppWebhook handles both the Meta verification challenge (GET)
// and inbound webhook events (POST).
//
//encore:api public raw path=/api/v1/webhook/whatsapp
func HandleWhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	handleWhatsAppWebhook(w, r)
}

func handleWhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
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

	messages := whatsapp.ParseWebhook(body)
	ctx := r.Context()

	if err := verifyInboundWebhookSignature(ctx, body, r.Header.Get("X-Hub-Signature-256"), messages, nil); err != nil {
		switch err.Error() {
		case "phone_number_id missing":
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
		return
	}
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

	ts, err := openTenantScope(ctx, schema)
	if err != nil {
		return fmt.Errorf("tenant scope: %w", err)
	}

	// Idempotent: skip if message already stored
	var exists bool
	if err := ts.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM message WHERE external_id = $1)`,
		msg.ExternalID).Scan(&exists); err != nil {
		return fmt.Errorf("dup check: %w", err)
	}
	if exists {
		return nil
	}

	contactID, err := upsertContact(ctx, ts, schema, msg)
	if err != nil {
		return fmt.Errorf("upsert contact: %w", err)
	}

	convoID, err := upsertConversation(ctx, ts, channelID, contactID)
	if err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}

	messageID, err := insertMessage(ctx, ts, convoID, msg)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	preview := whatsapp.InboundMessagePreview(msg.Type, msg.Body)
	preview = strutil.TruncateUTF8(preview, 280)
	if _, err := ts.ExecContext(ctx,
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
		publishJobWithRetry(ctx, "ai-inbound", func(ctx context.Context) error {
			return ai.PublishInboundJob(ctx, &ai.InboundAIJob{
				TenantSchema:     schema,
				ConversationID:   convoID,
				InboundMessageID: messageID,
				InboundType:      msg.Type,
			})
		})
	}

	if strings.EqualFold(strings.TrimSpace(msg.Type), "image") {
		tenantID, _ := tenant.TenantIDBySchema(ctx, schema)
		publishJobWithRetry(ctx, "payment-proof", func(ctx context.Context) error {
			return ai.PublishPaymentProofJob(ctx, &ai.PaymentProofJob{
				TenantSchema:     schema,
				TenantID:         tenantID,
				ConversationID:   convoID,
				ContactID:        contactID,
				MessageID:        messageID,
				InboundMessageID: messageID,
			})
		})
	}

	if inbox.IsPersistableMediaType(msg.Type) {
		publishJobWithRetry(ctx, "inbox-media-persist", func(ctx context.Context) error {
			return inbox.PublishInboxMediaPersistJob(ctx, &inbox.InboxMediaPersistJob{
				TenantSchema: schema,
				MessageID:    messageID,
				MessageType:  msg.Type,
			})
		})
	}

	if ok, sErr := ai.ShouldTriggerSummary(ctx, schema, convoID); sErr == nil && ok {
		ai.TryPublishSummarize(ctx, schema, convoID)
	}

	if tenantID, tErr := tenant.TenantIDBySchema(ctx, schema); tErr == nil && tenantID != "" {
		inboxrealtime.Publish(ctx, appauth.RedisClient(), tenantID)
	}

	contactPhone, _ := contactPhoneFromScope(ctx, ts, schema, contactID)

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

func contactPIIReady(ctx context.Context, schema string) bool {
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	if pii.ValidateKey(key) != nil {
		return false
	}
	active, err := tenantschema.ContactPIIActive(ctx, db.Stdlib(), schema)
	if err == nil && active {
		return true
	}
	if err := tenant.RunPIISchemaPatches(ctx, schema); err != nil {
		rlog.Warn("contact PII schema patch failed", "schema", schema, "err", err)
		return false
	}
	tenantschema.InvalidateContactPIICache(schema)
	active, err = tenantschema.TableColumnExists(ctx, db.Stdlib(), schema, "contact", "phone_number_idx")
	if active {
		tenantschema.MarkContactPIIActive(schema)
	}
	return active && err == nil
}

func upsertContact(ctx context.Context, ts appdb.TenantScope, schema string, msg whatsapp.InboundMessage) (string, error) {
	phone := normalizePhone(msg.FromPhone)
	displayName := truncStr(strings.TrimSpace(msg.FromDisplayName), 200)
	usePII := contactPIIReady(ctx, schema)
	if !usePII {
		if pii.ValidateKey(strings.TrimSpace(secrets.DataEncryptionKey)) == nil {
			if active, _ := tenantschema.ContactPIIActive(ctx, db.Stdlib(), schema); !active {
				rlog.Warn("contact PII columns missing — using legacy upsert; run scripts/apply-pii-schema-cloud.sh",
					"schema", schema)
			}
		}
		return upsertContactLegacy(ctx, ts, phone, displayName)
	}
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	phoneIdx := pii.BlindIndex(pii.NormalizePhone(phone), key)
	var id string
	err := ts.QueryRowContext(ctx,
		`SELECT id FROM contact WHERE phone_number_idx = $1 AND deleted_at IS NULL LIMIT 1`, phoneIdx).Scan(&id)
	if err == sql.ErrNoRows {
		phoneEnc, encErr := pii.Encrypt(phone, key)
		if encErr != nil {
			return "", encErr
		}
		var displayEnc *string
		if displayName != "" {
			enc, encErr := pii.Encrypt(displayName, key)
			if encErr != nil {
				return "", encErr
			}
			displayEnc = &enc
		}
		return id, ts.QueryRowContext(ctx, `
			INSERT INTO contact (phone_number, phone_number_enc, phone_number_idx, display_name, display_name_enc, tags)
			VALUES ($1,$2,$3,$1,$4,'["new"]'::jsonb) RETURNING id`,
			pii.Placeholder, phoneEnc, phoneIdx, displayEnc).Scan(&id)
	}
	if err != nil {
		return "", err
	}
	if displayName != "" {
		enc, _ := pii.Encrypt(displayName, key)
		_, _ = ts.ExecContext(ctx, `
			UPDATE contact SET display_name_enc = $1, display_name = $2, updated_at = NOW()
			WHERE id = $3 AND (display_name_enc IS NULL OR TRIM(display_name_enc) = '')`,
			enc, pii.Placeholder, id)
	}
	return id, nil
}

func upsertContactLegacy(ctx context.Context, ts appdb.TenantScope, phone, displayName string) (string, error) {
	var id string
	err := ts.QueryRowContext(ctx,
		`SELECT id FROM contact WHERE phone_number = $1 AND deleted_at IS NULL`, phone).Scan(&id)
	if err == sql.ErrNoRows {
		var dn *string
		if displayName != "" {
			dn = &displayName
		}
		return id, ts.QueryRowContext(ctx,
			`INSERT INTO contact (phone_number, display_name, tags)
			 VALUES ($1, $2, '["new"]'::jsonb) RETURNING id`, phone, dn).Scan(&id)
	}
	if err != nil {
		return "", err
	}
	if displayName != "" {
		_, _ = ts.ExecContext(ctx, `
			UPDATE contact SET display_name = $1, updated_at = NOW()
			WHERE id = $2 AND (display_name IS NULL OR TRIM(display_name) = '')`,
			displayName, id)
	}
	return id, nil
}

func contactPhoneFromScope(ctx context.Context, ts appdb.TenantScope, schema, contactID string) (string, error) {
	active, err := tenantschema.ContactPIIActive(ctx, db.Stdlib(), schema)
	if err != nil || !active {
		var phone string
		err := ts.QueryRowContext(ctx,
			`SELECT COALESCE(phone_number,'') FROM contact WHERE id = $1`, contactID).Scan(&phone)
		return phone, err
	}
	var phoneEnc, phoneLegacy sql.NullString
	err = ts.QueryRowContext(ctx, `
		SELECT COALESCE(phone_number_enc,''), COALESCE(phone_number,'')
		FROM contact WHERE id = $1`, contactID).Scan(&phoneEnc, &phoneLegacy)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	return pii.DecryptOrLegacy(phoneEnc.String, phoneLegacy.String, key)
}

func upsertConversation(ctx context.Context, ts appdb.TenantScope, channelID, contactID string) (string, error) {
	var id string
	err := ts.QueryRowContext(ctx,
		`SELECT id FROM conversation WHERE channel_id = $1 AND contact_id = $2`,
		channelID, contactID).Scan(&id)

	if err == sql.ErrNoRows {
		return id, ts.QueryRowContext(ctx,
			`INSERT INTO conversation (channel_id, contact_id, status, ai_handled, unread_count)
			 VALUES ($1, $2, 'open', true, 0)
			 RETURNING id`,
			channelID, contactID).Scan(&id)
	}
	return id, err
}

func insertMessage(ctx context.Context, ts appdb.TenantScope, convoID string, msg whatsapp.InboundMessage) (string, error) {
	rawJSON := string(msg.Raw)
	var id string
	err := ts.QueryRowContext(ctx,
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
// lookupChannelMetaAppSecret returns meta_app_secret saved during WhatsApp OAuth connect.
// No Encore global secret — each channel stores credentials at onboarding.
func lookupChannelMetaAppSecret(ctx context.Context, phoneNumberID, displayPhone string) (string, error) {
	phoneNumberID = strings.TrimSpace(phoneNumberID)
	if phoneNumberID == "" {
		return "", fmt.Errorf("empty phone_number_id")
	}
	ref, err := tenant.ResolveWhatsAppInbound(ctx, phoneNumberID, displayPhone)
	if err != nil {
		return "", err
	}
	pool := db.Stdlib()
	var secret sql.NullString
	err = pool.QueryRowContext(ctx,
		fmt.Sprintf(
			`SELECT meta_app_secret FROM %s.whatsapp_channel WHERE id = $1::uuid LIMIT 1`,
			appdb.QuoteIdent(ref.Schema)),
		ref.ChannelID,
	).Scan(&secret)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no channel for phone_number_id=%s", phoneNumberID)
	}
	if err != nil {
		return "", err
	}
	if secret.Valid && strings.TrimSpace(secret.String) != "" {
		return strings.TrimSpace(secret.String), nil
	}
	return "", nil
}

func resolveInboundChannel(ctx context.Context, phoneNumberID, displayPhone string) (*inboundChannel, error) {
	ref, err := tenant.ResolveWhatsAppInbound(ctx, phoneNumberID, displayPhone)
	if err != nil {
		return nil, err
	}
	return &inboundChannel{Schema: ref.Schema, ChannelID: ref.ChannelID}, nil
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
