package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	"encore.app/wabantu/inbox"
	sim "encore.app/wabantu/internal/simulator"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/ratelimit"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/shared/whatsappchannel"
	"encore.app/wabantu/whatsapp"
)

// ─── Judge chat simulator (BE-09) — contract: docs/API_AI_CONFIG.md §2 ──────
// Same ingest pipeline as the Meta webhook (ingestResolved), on a per-tenant
// whatsapp_channel with provider "simulator": outbound is stored, never sent to Meta.

const simConvTTL = 7 * 24 * time.Hour

type SimulatorImage struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mimeType"`
}

// SimulatorSendParams: buyer message only — no price/status/order fields by design.
type SimulatorSendParams struct {
	SessionID string          `json:"sessionId"`
	BuyerName string          `json:"buyerName"`
	Text      string          `json:"text"`
	Image     *SimulatorImage `json:"image"`
}

type SimulatorSendResponse struct {
	ConversationID string `json:"conversationId"`
	MessageID      string `json:"messageId"`
	Status         string `json:"status"`
}

// SimulatorSend accepts a buyer message for the session tenant.
//
//encore:api auth method=POST path=/api/v1/simulator/messages
func SimulatorSend(ctx context.Context, p *SimulatorSendParams) (*SimulatorSendResponse, error) {
	user, err := simulatorUser()
	if err != nil {
		return nil, err
	}
	raw := sim.Input{SessionID: p.SessionID, BuyerName: p.BuyerName, Text: p.Text}
	if p.Image != nil {
		raw.Image = &sim.Image{Base64: p.Image.Base64, MimeType: p.Image.MimeType}
	}
	in, err := sim.Validate(raw)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	if !ratelimit.Allow(ctx, appauth.RedisClient(), ratelimit.Key("simulator", user.TenantID), sim.RatePerMinute, time.Minute) {
		return nil, &errs.Error{Code: errs.ResourceExhausted, Message: "terlalu banyak pesan simulator, coba lagi sebentar"}
	}
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "database connection failed"}
	}
	channelID, err := ensureSimulatorChannel(ctx, ts)
	if err != nil {
		rlog.Error("simulator channel", "err", err, "tenantId", user.TenantID)
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal menyiapkan channel simulator"}
	}

	extID := whatsappchannel.SimulatorExternalID("in")
	msg := whatsapp.InboundMessage{
		ExternalID:      extID,
		FromPhone:       sim.BuyerPhone(user.TenantID, in.SessionID),
		ToDisplayPhone:  sim.ChannelPhone,
		FromDisplayName: in.BuyerName,
		Type:            in.Type(),
		Body:            in.Text,
		ReceivedAt:      time.Now().UTC(),
	}
	opt := ingestOptions{out: &ingestResult{}}
	if in.Type() == "image" {
		rawJSON, _ := json.Marshal(map[string]any{"image": map[string]string{"id": extID, "mime_type": in.ImageMime, "caption": in.Text}})
		msg.Raw = rawJSON
		opt.skipMediaPersist = true
		opt.afterInsert = func(ctx context.Context, messageID string) error {
			inbox.CacheMessageMedia(ctx, user.TenantSchema, messageID, in.ImageMime, in.ImageData)
			return nil
		}
	} else {
		rawJSON, _ := json.Marshal(map[string]any{"text": map[string]string{"body": in.Text}})
		msg.Raw = rawJSON
	}
	if _, err := ingestResolved(ctx, user.TenantSchema, channelID, msg, opt); err != nil {
		rlog.Error("simulator ingest", "err", err, "tenantId", user.TenantID)
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal memproses pesan simulator"}
	}
	if rdb := appauth.RedisClient(); rdb != nil {
		_ = rdb.Set(ctx, simConvKey(user.TenantID, in.SessionID), opt.out.ConversationID, simConvTTL).Err()
	}
	rlog.Info("simulator message queued", "tenantId", user.TenantID, "actor", user.AccountID, "type", in.Type(), "convoId", opt.out.ConversationID)
	return &SimulatorSendResponse{ConversationID: opt.out.ConversationID, MessageID: opt.out.MessageID, Status: "queued"}, nil
}

type SimulatorListParams struct {
	SessionID string `query:"sessionId"`
	After     string `query:"after"`
}

type SimulatorMessage struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	Author    string `json:"author"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
	Status    string `json:"status"`
	// ImageURL is set for image messages: authenticated GET (Bearer), served from the media cache.
	ImageURL *string `json:"imageUrl"`
}

type SimulatorListResponse struct {
	ConversationID *string            `json:"conversationId"`
	Messages       []SimulatorMessage `json:"messages"`
	// Pricing is the tier the AI quotes to this buyer; null before the first message.
	Pricing *sim.PricingView `json:"pricing"`
	// Order is the latest order of this conversation (status + proof check); null if none.
	Order *sim.OrderView `json:"order"`
}

// SimulatorMessages polls the simulator conversation for the session tenant.
//
//encore:api auth method=GET path=/api/v1/simulator/messages
func SimulatorMessages(ctx context.Context, p *SimulatorListParams) (*SimulatorListResponse, error) {
	user, err := simulatorUser()
	if err != nil {
		return nil, err
	}
	sid := strings.TrimSpace(p.SessionID)
	if err := sim.ValidateSession(sid); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	var after *time.Time
	if a := strings.TrimSpace(p.After); a != "" {
		t, err := time.Parse(time.RFC3339Nano, a)
		if err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "after harus RFC3339"}
		}
		after = &t
	}
	out := &SimulatorListResponse{Messages: []SimulatorMessage{}}
	rdb := appauth.RedisClient()
	if rdb == nil {
		return out, nil
	}
	convoID, err := rdb.Get(ctx, simConvKey(user.TenantID, sid)).Result()
	if err != nil || convoID == "" {
		return out, nil
	}
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "database connection failed"}
	}
	// Defense in depth: the conversation must be on this tenant's simulator channel.
	var ok bool
	if err := ts.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM conversation c JOIN whatsapp_channel w ON w.id = c.channel_id
		              WHERE c.id = $1 AND w.provider = $2)`, convoID, whatsappchannel.SimulatorProvider).Scan(&ok); err != nil || !ok {
		return out, nil
	}
	rows, err := ts.QueryContext(ctx, `
		SELECT id, direction, author, type, COALESCE(body, ''), created_at, status
		FROM message
		WHERE conversation_id = $1 AND ($2::timestamptz IS NULL OR created_at > $2)
		ORDER BY created_at ASC
		LIMIT 200`, convoID, after)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal memuat pesan"}
	}
	defer rows.Close()
	for rows.Next() {
		var m SimulatorMessage
		var created time.Time
		if err := rows.Scan(&m.ID, &m.Direction, &m.Author, &m.Type, &m.Text, &created, &m.Status); err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "gagal membaca pesan"}
		}
		if m.Direction == "in" {
			m.Direction = "inbound"
		} else {
			m.Direction = "outbound"
		}
		if m.Type == "image" && m.Text == "" {
			m.Text = "[gambar]"
		}
		if m.Type == "image" {
			u := sim.ImageURL(m.ID)
			m.ImageURL = &u
		}
		m.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		out.Messages = append(out.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "gagal membaca pesan"}
	}
	out.ConversationID = &convoID
	out.Pricing, out.Order = loadSimulatorState(ctx, ts, convoID)
	return out, nil
}

// loadSimulatorState returns the buyer's quoted price tier and latest order. Best-effort:
// errors leave the fields null so polling never fails on enrichment.
func loadSimulatorState(ctx context.Context, ts appdb.TenantScope, convoID string) (*sim.PricingView, *sim.OrderView) {
	var pricing *sim.PricingView
	var code, label string
	if err := ts.QueryRowContext(ctx, `
		SELECT t.code, t.label FROM conversation c
		JOIN contact ct ON ct.id = c.contact_id
		JOIN business_price_type t ON t.deleted_at IS NULL AND t.is_active = true
		     AND (t.id = ct.price_type_id OR t.is_default = true)
		WHERE c.id = $1
		ORDER BY (t.id = ct.price_type_id) IS TRUE DESC, t.is_default DESC, t.display_order
		LIMIT 1`, convoID).Scan(&code, &label); err == nil {
		pricing = &sim.PricingView{Tier: code, TierLabel: label}
	}
	var id, status, payStatus string
	var total float64
	var items, meta []byte
	var updated time.Time
	if err := ts.QueryRowContext(ctx, `
		SELECT id::text, status, payment_status, COALESCE(total, 0)::float8, COALESCE(items, '[]'::jsonb),
		       COALESCE(payment_proof_meta, '{}'::jsonb), updated_at
		FROM "order" WHERE conversation_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, convoID).Scan(&id, &status, &payStatus, &total, &items, &meta, &updated); err != nil {
		return pricing, nil
	}
	v := sim.BuildOrderView(id, status, payStatus, total, items, meta, updated.UTC().Format(time.RFC3339Nano))
	return pricing, &v
}

func simulatorUser() (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	switch u.Role {
	case "owner", "staff", "super_admin":
	default:
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "role tidak diizinkan"}
	}
	if strings.TrimSpace(u.TenantID) == "" || strings.TrimSpace(u.TenantSchema) == "" {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "sesi tidak punya tenant (superadmin: pilih tenant dulu)"}
	}
	return u, nil
}

func simConvKey(tenantID, sessionID string) string {
	return "simulator:conv:" + tenantID + ":" + sessionID
}

// ensureSimulatorChannel returns (creating once) the tenant's simulator channel.
func ensureSimulatorChannel(ctx context.Context, ts appdb.TenantScope) (string, error) {
	var id string
	err := ts.QueryRowContext(ctx, `
		SELECT id FROM whatsapp_channel WHERE provider = $1 ORDER BY created_at ASC LIMIT 1`,
		whatsappchannel.SimulatorProvider).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	err = ts.QueryRowContext(ctx, `
		INSERT INTO whatsapp_channel (provider, display_name, phone_number, status, connected_at)
		VALUES ($1, $2, $3, 'connected', now())
		RETURNING id`, whatsappchannel.SimulatorProvider, sim.ChannelName, sim.ChannelPhone).Scan(&id)
	return id, err
}
