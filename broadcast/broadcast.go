package broadcast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/pubsub"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/entitlement"
	"encore.app/wabantu/internal/realtime"
	sim "encore.app/wabantu/internal/simulator"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/whatsappchannel"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/usage"
	"encore.app/wabantu/whatsapp"
)

var db = sqldb.Named("tenant")

const sendBatchSize = 20
const sendDelayMs = 200

// ---------- Pub/Sub ----------

type BroadcastSendRequest struct {
	TenantSchema string `json:"tenantSchema"`
	CampaignID   string `json:"campaignId"`
	// TenantID scopes the Firestore mirror (tenants/{tenantId}/...). Empty on jobs queued
	// before this field existed: those still deliver, only the mirror is skipped.
	TenantID string `json:"tenantId,omitempty"`
}

var BroadcastSendTopic = pubsub.NewTopic[BroadcastSendRequest]("broadcast-send", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(BroadcastSendTopic, "broadcast-sender", pubsub.SubscriptionConfig[BroadcastSendRequest]{
	Handler:     handleBroadcastSend,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// ---------- types ----------

type Campaign struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	MessageBody      string     `json:"messageBody"`
	Status           string     `json:"status"`
	ScheduledAt      *time.Time `json:"scheduledAt,omitempty"`
	TotalRecipients  int        `json:"totalRecipients"`
	SentCount        int        `json:"sentCount"`
	FailedCount      int        `json:"failedCount"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type CreateCampaignRequest struct {
	Name          string     `json:"name"`
	MessageBody   string     `json:"messageBody"`
	ScheduledAt   *time.Time `json:"scheduledAt,omitempty"`
	Recipients    []string   `json:"recipients"`
}

type CreateCampaignResponse struct {
	Campaign Campaign `json:"campaign"`
}

type ListCampaignsResponse struct {
	Campaigns []Campaign `json:"campaigns"`
}

// ---------- API ----------

//encore:api auth method=POST path=/api/v1/broadcast/campaigns tag:owner
func CreateCampaign(ctx context.Context, req *CreateCampaignRequest) (*CreateCampaignResponse, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, err
	}
	if len(req.Recipients) == 0 {
		return nil, appErrs.BadRequest("recipients required")
	}
	if req.MessageBody == "" {
		return nil, appErrs.BadRequest("messageBody required")
	}

	plan := usage.TenantPlan(ctx, u.TenantSchema)
	if !entitlement.HasFeature(plan, entitlement.FeatureBroadcast) {
		return nil, appErrs.Forbidden("broadcast not available on your plan")
	}
	allowed, _, _ := usage.CheckQuota(ctx, u.TenantSchema, "broadcast_contact")
	if !allowed {
		return nil, appErrs.Forbidden("broadcast contact quota exceeded")
	}

	name := req.Name
	if name == "" {
		name = fmt.Sprintf("Broadcast %s", time.Now().Format("2006-01-02 15:04"))
	}

	status := "queued"
	var scheduledAt *time.Time
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		status = "scheduled"
		scheduledAt = req.ScheduledAt
	}

	var camp Campaign
	err = db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO "%s".broadcast_campaign
			(name, message_body, status, scheduled_at, total_recipients, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, name, message_body, status, scheduled_at, total_recipients, sent_count, failed_count, created_at`,
		u.TenantSchema),
		name, req.MessageBody, status, scheduledAt, len(req.Recipients), u.AccountID,
	).Scan(&camp.ID, &camp.Name, &camp.MessageBody, &camp.Status, &camp.ScheduledAt,
		&camp.TotalRecipients, &camp.SentCount, &camp.FailedCount, &camp.CreatedAt)
	if err != nil {
		return nil, appErrs.Internal("create campaign failed")
	}

	piiActive := broadcastPIIActive(ctx, u.TenantSchema)
	for _, phone := range req.Recipients {
		phone = normalizePhone(phone)
		if phone == "" {
			continue
		}
		if piiActive {
			phoneEnc, phoneIdx, store, encErr := encryptBroadcastPhone(phone)
			if encErr != nil {
				return nil, appErrs.Internal("encrypt recipient failed")
			}
			if store == "" && phoneEnc == "" {
				continue
			}
			_, _ = db.Exec(ctx, fmt.Sprintf(`
				INSERT INTO "%s".broadcast_recipient (campaign_id, phone_number, phone_number_enc, phone_number_idx)
				VALUES ($1,$2,$3,$4)`, u.TenantSchema), camp.ID, store, phoneEnc, phoneIdx)
		} else {
			_, _ = db.Exec(ctx, fmt.Sprintf(`
				INSERT INTO "%s".broadcast_recipient (campaign_id, phone_number)
				VALUES ($1,$2)`, u.TenantSchema), camp.ID, phone)
		}
	}

	_ = usage.RecordEvent(ctx, u.TenantSchema, "broadcast_contact", len(req.Recipients), nil)

	if status == "queued" {
		_, _ = BroadcastSendTopic.Publish(ctx, BroadcastSendRequest{
			TenantSchema: u.TenantSchema,
			CampaignID:   camp.ID,
			TenantID:     u.TenantID,
		})
	}

	return &CreateCampaignResponse{Campaign: camp}, nil
}

//encore:api auth method=GET path=/api/v1/broadcast/campaigns
func ListCampaigns(ctx context.Context) (*ListCampaignsResponse, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT id, name, message_body, status, scheduled_at, total_recipients,
		       sent_count, failed_count, created_at
		FROM "%s".broadcast_campaign
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 50`, u.TenantSchema))
	if err != nil {
		return nil, appErrs.Internal("list campaigns failed")
	}
	defer rows.Close()

	campaigns := make([]Campaign, 0)
	for rows.Next() {
		var c Campaign
		if err := rows.Scan(&c.ID, &c.Name, &c.MessageBody, &c.Status, &c.ScheduledAt,
			&c.TotalRecipients, &c.SentCount, &c.FailedCount, &c.CreatedAt); err != nil {
			return nil, appErrs.Internal("scan campaign failed")
		}
		campaigns = append(campaigns, c)
	}
	return &ListCampaignsResponse{Campaigns: campaigns}, nil
}

//encore:api auth method=POST path=/api/v1/broadcast/campaigns/:id/send tag:owner
func SendCampaign(ctx context.Context, id string) error {
	u, err := authUser(ctx)
	if err != nil {
		return err
	}
	plan := usage.TenantPlan(ctx, u.TenantSchema)
	if !entitlement.HasFeature(plan, entitlement.FeatureBroadcast) {
		return appErrs.Forbidden("broadcast not available on your plan")
	}
	_, err = BroadcastSendTopic.Publish(ctx, BroadcastSendRequest{
		TenantSchema: u.TenantSchema,
		CampaignID:   id,
		TenantID:     u.TenantID,
	})
	return err
}

// ---------- worker ----------

func handleBroadcastSend(ctx context.Context, req BroadcastSendRequest) error {
	var body, status string
	err := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT message_body, status FROM "%s".broadcast_campaign WHERE id=$1 AND deleted_at IS NULL`,
		req.TenantSchema), req.CampaignID).Scan(&body, &status)
	if err != nil {
		return fmt.Errorf("load campaign: %w", err)
	}
	if status == "completed" || status == "cancelled" {
		return nil
	}

	_, _ = db.Exec(ctx, fmt.Sprintf(`
		UPDATE "%s".broadcast_campaign SET status='sending', updated_at=NOW() WHERE id=$1`,
		req.TenantSchema), req.CampaignID)

	piiActive := broadcastPIIActive(ctx, req.TenantSchema)
	recipientSQL := fmt.Sprintf(`
		SELECT id, COALESCE(phone_number_enc,''), COALESCE(phone_number,'')
		FROM "%s".broadcast_recipient
		WHERE campaign_id=$1 AND status='pending'
		ORDER BY created_at ASC LIMIT $2`, req.TenantSchema)
	if !piiActive {
		recipientSQL = fmt.Sprintf(`
		SELECT id, '', COALESCE(phone_number,'')
		FROM "%s".broadcast_recipient
		WHERE campaign_id=$1 AND status='pending'
		ORDER BY created_at ASC LIMIT $2`, req.TenantSchema)
	}
	rows, err := db.Query(ctx, recipientSQL, req.CampaignID, sendBatchSize)
	if err != nil {
		return err
	}
	defer rows.Close()

	var pendingLeft bool
	for rows.Next() {
		var recID, phoneEnc, phoneLegacy string
		if err := rows.Scan(&recID, &phoneEnc, &phoneLegacy); err != nil {
			continue
		}
		phone, err := decryptBroadcastPhone(phoneEnc, phoneLegacy)
		if err != nil || phone == "" {
			continue
		}
		var sendErr error
		if whatsappchannel.IsSimulatorPhone(phone) {
			// Simulator buyer: store in its simulator conversation, never Meta.
			sendErr = deliverSimulatorBroadcast(ctx, req.TenantSchema, req.TenantID, phone, body, piiActive)
		} else {
			sendErr = sendToPhone(ctx, req.TenantSchema, phone, body)
		}
		if sendErr != nil {
			_, _ = db.Exec(ctx, fmt.Sprintf(`
				UPDATE "%s".broadcast_recipient SET status='failed', last_error=$1 WHERE id=$2`,
				req.TenantSchema), sendErr.Error(), recID)
			_, _ = db.Exec(ctx, fmt.Sprintf(`
				UPDATE "%s".broadcast_campaign SET failed_count = failed_count + 1, updated_at=NOW() WHERE id=$1`,
				req.TenantSchema), req.CampaignID)
		} else {
			_, _ = db.Exec(ctx, fmt.Sprintf(`
				UPDATE "%s".broadcast_recipient SET status='sent', sent_at=NOW() WHERE id=$1`,
				req.TenantSchema), recID)
			_, _ = db.Exec(ctx, fmt.Sprintf(`
				UPDATE "%s".broadcast_campaign SET sent_count = sent_count + 1, updated_at=NOW() WHERE id=$1`,
				req.TenantSchema), req.CampaignID)
		}
		time.Sleep(sendDelayMs * time.Millisecond)
	}

	// More pending?
	var remaining int
	_ = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM "%s".broadcast_recipient WHERE campaign_id=$1 AND status='pending'`,
		req.TenantSchema), req.CampaignID).Scan(&remaining)
	pendingLeft = remaining > 0

	if pendingLeft {
		_, err = BroadcastSendTopic.Publish(ctx, req)
		return err
	}

	_, _ = db.Exec(ctx, fmt.Sprintf(`
		UPDATE "%s".broadcast_campaign SET status='completed', updated_at=NOW() WHERE id=$1`,
		req.TenantSchema), req.CampaignID)
	rlog.Info("broadcast completed", "campaignId", req.CampaignID)
	return nil
}

// ---------- helpers ----------

func authUser(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	if !u.HasEffectiveTenantContext() {
		return nil, appErrs.Forbidden("tenant context required — pantau tenant dari konsol admin")
	}
	if err := u.RequireModule("sales"); err != nil {
		return nil, err
	}
	return u, nil
}

func sendToPhone(ctx context.Context, schema, phone, body string) error {
	var tokenEnc, tokenLegacy, phoneNumberID string
	err := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COALESCE(access_token_enc,''), COALESCE(access_token,''),
		       COALESCE(meta_phone_number_id,'')
		FROM "%s".whatsapp_channel
		WHERE status = 'connected'
		  AND provider <> 'simulator'
		  AND (
		    NULLIF(TRIM(access_token_enc), '') IS NOT NULL
		    OR (NULLIF(TRIM(access_token), '') IS NOT NULL AND access_token <> $1)
		  )
		ORDER BY connected_at DESC NULLS LAST LIMIT 1`, schema),
		pii.Placeholder,
	).Scan(&tokenEnc, &tokenLegacy, &phoneNumberID)
	if err != nil {
		return fmt.Errorf("no connected whatsapp channel")
	}
	token, err := pii.DecryptOrLegacy(tokenEnc, tokenLegacy, encKey())
	if err != nil || token == "" || phoneNumberID == "" {
		return fmt.Errorf("no connected whatsapp channel")
	}
	_, err = whatsapp.SendText(ctx, token, phoneNumberID, phone, body)
	return err
}

func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "62") && strings.HasPrefix(p, "0") {
		p = "62" + p[1:]
	}
	return p
}

// deliverSimulatorBroadcast persists a broadcast to the buyer's simulator conversation so it
// shows up via GET /api/v1/simulator/messages. It never calls Meta.
func deliverSimulatorBroadcast(ctx context.Context, schema, tenantID, phone, body string, piiActive bool) error {
	var contactID string
	var err error
	if piiActive {
		idx := pii.BlindIndex(pii.NormalizePhone(phone), encKey())
		err = db.QueryRow(ctx, fmt.Sprintf(`SELECT id::text FROM "%s".contact WHERE phone_number_idx=$1 AND deleted_at IS NULL LIMIT 1`, schema), idx).Scan(&contactID)
	} else {
		err = db.QueryRow(ctx, fmt.Sprintf(`SELECT id::text FROM "%s".contact WHERE phone_number=$1 AND deleted_at IS NULL LIMIT 1`, schema), phone).Scan(&contactID)
	}
	if err != nil {
		return fmt.Errorf("simulator contact not found")
	}
	var convID string
	if err := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT c.id::text FROM "%[1]s".conversation c
		JOIN "%[1]s".whatsapp_channel ch ON ch.id = c.channel_id
		WHERE c.contact_id=$1 AND ch.provider=$2
		ORDER BY c.last_message_at DESC NULLS LAST LIMIT 1`, schema),
		contactID, whatsappchannel.SimulatorProvider).Scan(&convID); err != nil {
		return fmt.Errorf("simulator conversation not found")
	}
	var msgID string
	var createdAt time.Time
	if err := db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO "%s".message (conversation_id, external_id, direction, author, type, body, metadata, status)
		VALUES ($1, $2, 'out', 'human', 'text', $3, '{"source":"broadcast"}'::jsonb, 'sent')
		RETURNING id::text, created_at`, schema),
		convID, whatsappchannel.SimulatorExternalID("bc"), body).Scan(&msgID, &createdAt); err != nil {
		return fmt.Errorf("simulator store failed: %w", err)
	}
	sim.MirrorBroadcast(ctx, realtime.Default(realtime.LogfTo(rlog.Warn)), sim.SQLDB(db.Stdlib()),
		sim.SchemaQualifier(schema), tenantID, convID, msgID, body, createdAt)
	_, _ = db.Exec(ctx, fmt.Sprintf(`UPDATE "%s".conversation SET last_message_at=NOW(), last_message_preview=LEFT($1,280) WHERE id=$2`, schema), body, convID)
	return nil
}
