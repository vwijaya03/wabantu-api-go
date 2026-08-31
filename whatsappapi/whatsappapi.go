// Package whatsappapi exposes WhatsApp channel management and Meta OAuth endpoints.
package whatsappapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.dev/beta/auth"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	appauth "encore.app/wabantu/auth"
	apperr "encore.app/wabantu/shared/errs"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsapp"
	"encore.app/wabantu/shared/types"
)

var db = sqldb.Named("tenant")

// ListChannelsResponse wraps the channel list (Encore requires a named struct).
type ListChannelsResponse struct {
	Items []Channel `json:"items"`
}

type Channel struct {
	ID                 string     `json:"id"`
	Provider           string     `json:"provider"`
	DisplayName        string     `json:"displayName"`
	PhoneNumber        string     `json:"phoneNumber"`
	MetaPhoneNumberID  *string    `json:"metaPhoneNumberId"`
	MetaWabaID         *string    `json:"metaWabaId"`
	MetaAppID          *string    `json:"metaAppId"`
	Status             string     `json:"status"`
	LastError          *string    `json:"lastError"`
	ConnectedAt        *time.Time `json:"connectedAt"`
}

type MetaConnectInitParams struct {
	RedirectURI    string `json:"redirectUri"`
	MetaAppID      string `json:"metaAppId"`
	MetaAppSecret  string `json:"metaAppSecret"`
}

type MetaConnectInitResponse struct {
	State            string `json:"state"`
	OAuthURL         string `json:"oauthUrl"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

type MetaConnectCallbackParams struct {
	Code              string  `json:"code"`
	State             string  `json:"state"`
	DisplayName       string  `json:"displayName"`
	PhoneNumber       string  `json:"phoneNumber"`
	MetaPhoneNumberID *string `json:"metaPhoneNumberId"`
	MetaWabaID        *string `json:"metaWabaId"`
}

type oauthStatePayload struct {
	TenantID      string `json:"tenantId"`
	UserID        string `json:"userId"`
	RedirectURI   string `json:"redirectUri"`
	MetaAppID     string `json:"metaAppId"`
	MetaAppSecret string `json:"metaAppSecret"`
}

const oauthStateTTL = 10 * time.Minute

func currentUser() (*types.AuthUser, error) {
	data, ok := auth.Data().(*types.AuthUser)
	if !ok || data == nil {
		return nil, apperr.Unauthenticated("not authenticated")
	}
	return data, nil
}

func requireOwner(u *types.AuthUser) error {
	if !u.CanPerformOwnerActions() {
		return apperr.Forbidden("owner role required")
	}
	return nil
}

func openTenantScope(ctx context.Context, schema string) (appdb.TenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return appdb.TenantScope{}, err
	}
	return appdb.OpenTenantScope(db.Stdlib(), schema), nil
}

// ListChannels returns all WhatsApp channels for the tenant.
//
//encore:api auth method=GET path=/api/v1/whatsapp/channels
func ListChannels(ctx context.Context) (*ListChannelsResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}

	rows, err := ts.QueryContext(ctx, `
		SELECT id, provider, display_name, phone_number,
		       meta_phone_number_id, meta_waba_id, meta_app_id,
		       status, last_error, connected_at
		FROM whatsapp_channel
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, apperr.Internal("failed to list channels")
	}
	defer rows.Close()

	var out []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(
			&ch.ID, &ch.Provider, &ch.DisplayName, &ch.PhoneNumber,
			&ch.MetaPhoneNumberID, &ch.MetaWabaID, &ch.MetaAppID,
			&ch.Status, &ch.LastError, &ch.ConnectedAt,
		); err != nil {
			continue
		}
		out = append(out, ch)
	}
	if out == nil {
		out = []Channel{}
	}
	return &ListChannelsResponse{Items: out}, rows.Err()
}

// InitMetaConnect starts Meta OAuth and stores state in Redis.
//
//encore:api auth method=POST path=/api/v1/whatsapp/meta/connect/init
func InitMetaConnect(ctx context.Context, p *MetaConnectInitParams) (*MetaConnectInitResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(user); err != nil {
		return nil, err
	}
	if p.MetaAppID == "" || p.MetaAppSecret == "" || p.RedirectURI == "" {
		return nil, apperr.BadRequest("metaAppId, metaAppSecret, and redirectUri are required")
	}

	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, apperr.Internal("failed to generate state")
	}
	state := hex.EncodeToString(stateBytes)

	payload, _ := json.Marshal(oauthStatePayload{
		TenantID:      user.TenantID,
		UserID:        user.AccountID,
		RedirectURI:   p.RedirectURI,
		MetaAppID:     p.MetaAppID,
		MetaAppSecret: p.MetaAppSecret,
	})
	key := oauthStateKey(state)
	rdb := appauth.RedisClient()
	if err := rdb.Set(ctx, key, payload, oauthStateTTL).Err(); err != nil {
		return nil, apperr.Internal("failed to store oauth state")
	}

	oauthURL := url.URL{
		Scheme: "https",
		Host:   "www.facebook.com",
		Path:   "/" + whatsapp.GraphAPIVersion + "/dialog/oauth",
	}
	q := oauthURL.Query()
	q.Set("client_id", p.MetaAppID)
	q.Set("redirect_uri", p.RedirectURI)
	q.Set("state", state)
	q.Set("scope", "whatsapp_business_messaging,whatsapp_business_management,business_management")
	q.Set("response_type", "code")
	oauthURL.RawQuery = q.Encode()

	return &MetaConnectInitResponse{
		State:            state,
		OAuthURL:         oauthURL.String(),
		ExpiresInSeconds: int(oauthStateTTL.Seconds()),
	}, nil
}

// CompleteMetaConnect exchanges the OAuth code and upserts the channel (public callback).
//
//encore:api public method=POST path=/api/v1/whatsapp/meta/connect/callback
func CompleteMetaConnect(ctx context.Context, p *MetaConnectCallbackParams) (*Channel, error) {
	if p.Code == "" || p.State == "" || p.DisplayName == "" || p.PhoneNumber == "" {
		return nil, apperr.BadRequest("code, state, displayName, and phoneNumber are required")
	}

	rdb := appauth.RedisClient()
	key := oauthStateKey(p.State)
	raw, err := rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, apperr.BadRequest("State OAuth invalid/expired. Ulangi proses connect WhatsApp.")
	}
	_ = rdb.Del(ctx, key)

	var st oauthStatePayload
	if err := json.Unmarshal(raw, &st); err != nil || st.TenantID == "" {
		return nil, apperr.BadRequest("State OAuth tidak valid")
	}

	accessToken, err := exchangeMetaCode(ctx, p.Code, st.RedirectURI, st.MetaAppID, st.MetaAppSecret)
	if err != nil {
		return nil, apperr.BadRequest("Gagal menukar authorization code ke access token Meta")
	}

	discovered := fetchMetaWaba(ctx, accessToken, p.PhoneNumber, st.MetaAppID, st.MetaAppSecret)
	metaPhoneID := p.MetaPhoneNumberID
	metaWabaID := p.MetaWabaID
	if metaPhoneID == nil || *metaPhoneID == "" {
		if discovered.PhoneNumberID != "" {
			v := discovered.PhoneNumberID
			metaPhoneID = &v
		}
	}
	if metaWabaID == nil || *metaWabaID == "" {
		if discovered.WabaID != "" {
			v := discovered.WabaID
			metaWabaID = &v
		}
	}

	schema, err := schemaForTenant(ctx, st.TenantID)
	if err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, schema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}

	ch, err := upsertChannel(ctx, ts, channelConnectParams{
		DisplayName:       p.DisplayName,
		PhoneNumber:       p.PhoneNumber,
		AccessToken:       accessToken,
		MetaPhoneNumberID: metaPhoneID,
		MetaWabaID:        metaWabaID,
		MetaAppID:         &st.MetaAppID,
		MetaAppSecret:     &st.MetaAppSecret,
	})
	if err != nil {
		return nil, err
	}
	if ch.MetaPhoneNumberID != nil && strings.TrimSpace(*ch.MetaPhoneNumberID) != "" {
		if regErr := tenant.RegisterWhatsAppInbound(ctx, schema, ch.ID, *ch.MetaPhoneNumberID, ch.PhoneNumber); regErr != nil {
			return nil, apperr.BadRequest(regErr.Error())
		}
	} else {
		rlog.Warn("OAuth connected but meta_phone_number_id empty — webhook akan gagal sampai reconnect atau backfill manual",
			"tenant", schema, "channelId", ch.ID, "phone", ch.PhoneNumber)
	}
	return ch, nil
}

// DisconnectChannel marks a channel as disconnected.
//
//encore:api auth method=DELETE path=/api/v1/whatsapp/channels/:id
func DisconnectChannel(ctx context.Context, id string) (*Channel, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(user); err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}

	var ch Channel
	err = ts.QueryRowContext(ctx, `
		UPDATE whatsapp_channel
		SET status = 'disconnected', connected_at = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING id, provider, display_name, phone_number,
		          meta_phone_number_id, meta_waba_id, meta_app_id,
		          status, last_error, connected_at`,
		id,
	).Scan(
		&ch.ID, &ch.Provider, &ch.DisplayName, &ch.PhoneNumber,
		&ch.MetaPhoneNumberID, &ch.MetaWabaID, &ch.MetaAppID,
		&ch.Status, &ch.LastError, &ch.ConnectedAt,
	)
	if err == sql.ErrNoRows {
		return nil, apperr.NotFound("Channel tidak ditemukan")
	}
	if err != nil {
		return nil, apperr.Internal("failed to disconnect channel")
	}
	if unregErr := tenant.UnregisterWhatsAppInbound(ctx, user.TenantSchema, id); unregErr != nil {
		rlog.Warn("unregister whatsapp inbound map failed", "err", unregErr)
	}
	return &ch, nil
}

type DeleteChannelResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// DeleteChannelPermanent hard-deletes a WhatsApp channel row and related inbox data.
//
//encore:api auth method=DELETE path=/api/v1/whatsapp/channels/:id/permanent tag:owner
func DeleteChannelPermanent(ctx context.Context, id string) (*DeleteChannelResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(user); err != nil {
		return nil, err
	}

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}

	tx, err := ts.BeginTx(ctx, nil)
	if err != nil {
		return nil, apperr.Internal("failed to start transaction")
	}
	defer tx.Rollback()
	tTx := ts.WithQ(tx)

	var exists bool
	if err := tTx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM whatsapp_channel WHERE id = $1)`, id,
	).Scan(&exists); err != nil {
		return nil, apperr.Internal("failed to check channel")
	}
	if !exists {
		return nil, apperr.NotFound("Channel tidak ditemukan")
	}

	if _, err := tTx.ExecContext(ctx, `
		DELETE FROM message
		WHERE conversation_id IN (SELECT id FROM conversation WHERE channel_id = $1)`, id); err != nil {
		return nil, apperr.Internal("failed to delete channel messages")
	}
	if _, err := tTx.ExecContext(ctx, `
		DELETE FROM conversation_summary
		WHERE conversation_id IN (SELECT id FROM conversation WHERE channel_id = $1)`, id); err != nil {
		return nil, apperr.Internal("failed to delete conversation summaries")
	}
	if _, err := tTx.ExecContext(ctx, `DELETE FROM conversation WHERE channel_id = $1`, id); err != nil {
		return nil, apperr.Internal("failed to delete conversations")
	}
	if _, err := tTx.ExecContext(ctx, `DELETE FROM whatsapp_channel WHERE id = $1`, id); err != nil {
		return nil, apperr.Internal("failed to delete channel")
	}
	if err := tx.Commit(); err != nil {
		return nil, apperr.Internal("failed to commit channel delete")
	}

	if unregErr := tenant.UnregisterWhatsAppInbound(ctx, user.TenantSchema, id); unregErr != nil {
		rlog.Warn("unregister whatsapp inbound map failed", "err", unregErr, "channelId", id)
	}

	return &DeleteChannelResponse{
		ID:      id,
		Message: "Channel dihapus permanen",
	}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func oauthStateKey(state string) string {
	return "whatsapp:meta:oauth:" + state
}

type channelConnectParams struct {
	DisplayName       string
	PhoneNumber       string
	AccessToken       string
	MetaPhoneNumberID *string
	MetaWabaID        *string
	MetaAppID         *string
	MetaAppSecret     *string
}

func upsertChannel(ctx context.Context, ts appdb.TenantScope, p channelConnectParams) (*Channel, error) {
	var existingID string
	err := ts.QueryRowContext(ctx,
		`SELECT id FROM whatsapp_channel WHERE phone_number = $1`, p.PhoneNumber,
	).Scan(&existingID)

	var ch Channel
	if err == sql.ErrNoRows {
		err = ts.QueryRowContext(ctx, `
			INSERT INTO whatsapp_channel (
				provider, display_name, phone_number, access_token,
				meta_phone_number_id, meta_waba_id, meta_app_id, meta_app_secret,
				status, connected_at
			) VALUES ('meta_cloud', $1, $2, $3, $4, $5, $6, $7, 'connected', NOW())
			RETURNING id, provider, display_name, phone_number,
			          meta_phone_number_id, meta_waba_id, meta_app_id,
			          status, last_error, connected_at`,
			p.DisplayName, p.PhoneNumber, p.AccessToken,
			p.MetaPhoneNumberID, p.MetaWabaID, p.MetaAppID, p.MetaAppSecret,
		).Scan(
			&ch.ID, &ch.Provider, &ch.DisplayName, &ch.PhoneNumber,
			&ch.MetaPhoneNumberID, &ch.MetaWabaID, &ch.MetaAppID,
			&ch.Status, &ch.LastError, &ch.ConnectedAt,
		)
	} else if err == nil {
		err = ts.QueryRowContext(ctx, `
			UPDATE whatsapp_channel SET
				provider = 'meta_cloud',
				display_name = $1,
				access_token = $2,
				meta_phone_number_id = COALESCE($3, meta_phone_number_id),
				meta_waba_id = COALESCE($4, meta_waba_id),
				meta_app_id = COALESCE($5, meta_app_id),
				meta_app_secret = COALESCE($6, meta_app_secret),
				status = 'connected',
				connected_at = NOW(),
				last_error = NULL,
				updated_at = NOW()
			WHERE id = $7
			RETURNING id, provider, display_name, phone_number,
			          meta_phone_number_id, meta_waba_id, meta_app_id,
			          status, last_error, connected_at`,
			p.DisplayName, p.AccessToken,
			p.MetaPhoneNumberID, p.MetaWabaID, p.MetaAppID, p.MetaAppSecret,
			existingID,
		).Scan(
			&ch.ID, &ch.Provider, &ch.DisplayName, &ch.PhoneNumber,
			&ch.MetaPhoneNumberID, &ch.MetaWabaID, &ch.MetaAppID,
			&ch.Status, &ch.LastError, &ch.ConnectedAt,
		)
	}
	if err != nil {
		rlog.Error("upsert channel failed", "err", err)
		return nil, apperr.Internal("failed to save channel")
	}
	return &ch, nil
}

func schemaForTenant(ctx context.Context, tenantID string) (string, error) {
	var schema string
	err := system.DB.QueryRow(ctx,
		`SELECT schema_name FROM tenant_company WHERE tenant_id = $1 LIMIT 1`,
		tenantID,
	).Scan(&schema)
	if err != nil {
		return "", apperr.Internal("tenant schema lookup failed")
	}
	return schema, nil
}

func exchangeMetaCode(ctx context.Context, code, redirectURI, appID, appSecret string) (string, error) {
	u := url.URL{
		Scheme: "https",
		Host:   "graph.facebook.com",
		Path:   "/" + whatsapp.GraphAPIVersion + "/oauth/access_token",
	}
	q := u.Query()
	q.Set("client_id", appID)
	q.Set("client_secret", appSecret)
	q.Set("redirect_uri", redirectURI)
	q.Set("code", code)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return result.AccessToken, nil
}

type SendTestMessageRequest struct {
	To   string `json:"to"`
	Body string `json:"body"`
}

type SendTestMessageResponse struct {
	ExternalID string `json:"externalId"`
}

// SendTestMessage sends a one-off WhatsApp text via a connected channel (owner).
//
//encore:api auth method=POST path=/api/v1/whatsapp/channels/:id/test-message tag:owner
func SendTestMessage(ctx context.Context, id string, req *SendTestMessageRequest) (*SendTestMessageResponse, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	if err := requireOwner(user); err != nil {
		return nil, err
	}
	to := strings.TrimSpace(req.To)
	body := strings.TrimSpace(req.Body)
	if to == "" || body == "" {
		return nil, apperr.BadRequest("to and body are required")
	}

	ts, err := openTenantScope(ctx, user.TenantSchema)
	if err != nil {
		return nil, apperr.Internal("database connection failed")
	}

	var token, phoneNumberID, status string
	err = ts.QueryRowContext(ctx, `
		SELECT COALESCE(access_token,''), COALESCE(meta_phone_number_id,''), status
		FROM whatsapp_channel WHERE id = $1`, id,
	).Scan(&token, &phoneNumberID, &status)
	if err == sql.ErrNoRows {
		return nil, apperr.NotFound("Channel tidak ditemukan")
	}
	if err != nil {
		return nil, apperr.Internal("channel lookup failed")
	}
	if status != "connected" || token == "" || phoneNumberID == "" {
		return nil, apperr.BadRequest("Channel WhatsApp belum terhubung")
	}

	extID, err := whatsapp.SendText(ctx, token, phoneNumberID, to, body)
	if err != nil {
		rlog.Error("test message failed", "err", err)
		return nil, apperr.Unavailable("Gagal mengirim pesan uji")
	}
	return &SendTestMessageResponse{ExternalID: extID}, nil
}

func normalizePhone(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
