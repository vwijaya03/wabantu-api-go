package inbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"

	appauth "encore.app/wabantu/auth"
	appdb "encore.app/wabantu/shared/db"
	apperr "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/mediastorage"
	"encore.app/wabantu/whatsapp"
)

const inboxMediaCacheTTL = time.Hour

var mediaDownloadTypes = map[string]bool{
	"image": true, "video": true, "audio": true, "document": true, "sticker": true,
}

type MessageMediaInfo struct {
	URL      string `json:"url"`
	MimeType string `json:"mimeType,omitempty"`
}

type messageMediaRow struct {
	ID             string
	ConversationID string
	Type           string
	Metadata       json.RawMessage
	ChannelID      string
}

func messageMediaAPIPath(messageID string) string {
	return "/inbox/messages/" + messageID + "/media"
}

func defaultMimeForMessageType(msgType string) string {
	switch strings.ToLower(strings.TrimSpace(msgType)) {
	case "image", "sticker":
		return "image/jpeg"
	case "video":
		return "video/mp4"
	case "audio":
		return "audio/ogg"
	case "document":
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

func enrichMessageMedia(m *MessageItem, metadata json.RawMessage) {
	if m == nil || !mediaDownloadTypes[strings.ToLower(m.Type)] {
		return
	}
	if whatsapp.ExtractMediaIDFromRaw(m.Type, metadata) == "" {
		return
	}
	m.Media = &MessageMediaInfo{
		URL:      messageMediaAPIPath(m.ID),
		MimeType: defaultMimeForMessageType(m.Type),
	}
}

func inboxMediaCacheKey(tenantSchema, messageID string) string {
	return fmt.Sprintf("inbox:media:%s:%s", tenantSchema, messageID)
}

type cachedInboxMedia struct {
	MimeType string `json:"mimeType"`
	Data     []byte `json:"data"`
}

func loadCachedInboxMedia(ctx context.Context, tenantSchema, messageID string) (*cachedInboxMedia, bool) {
	rdb := appauth.RedisClient()
	if rdb == nil {
		return nil, false
	}
	raw, err := rdb.Get(ctx, inboxMediaCacheKey(tenantSchema, messageID)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var entry cachedInboxMedia
	if err := json.Unmarshal(raw, &entry); err != nil || len(entry.Data) == 0 {
		return nil, false
	}
	return &entry, true
}

func storeCachedInboxMedia(ctx context.Context, tenantSchema, messageID string, mime string, data []byte) {
	rdb := appauth.RedisClient()
	if rdb == nil || len(data) == 0 {
		return
	}
	entry := cachedInboxMedia{MimeType: mime, Data: data}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = rdb.Set(ctx, inboxMediaCacheKey(tenantSchema, messageID), raw, inboxMediaCacheTTL).Err()
}

func loadMessageMediaRow(ctx context.Context, conn *sql.Conn, messageID string) (*messageMediaRow, error) {
	var row messageMediaRow
	var meta []byte
	err := conn.QueryRowContext(ctx, `
		SELECT m.id, m.conversation_id, m.type, m.metadata, c.channel_id
		FROM message m
		JOIN conversation c ON c.id = m.conversation_id
		WHERE m.id = $1`, messageID).
		Scan(&row.ID, &row.ConversationID, &row.Type, &meta, &row.ChannelID)
	if err == sql.ErrNoRows {
		return nil, apperr.NotFound("Pesan tidak ditemukan")
	}
	if err != nil {
		return nil, apperr.Internal("gagal memuat pesan")
	}
	row.Metadata = json.RawMessage(meta)
	return &row, nil
}

func loadChannelAccessToken(ctx context.Context, conn *sql.Conn, channelID string) (string, error) {
	var status, token, provider string
	err := conn.QueryRowContext(ctx, `
		SELECT status, COALESCE(access_token,''), provider
		FROM whatsapp_channel WHERE id = $1`, channelID).
		Scan(&status, &token, &provider)
	if err == sql.ErrNoRows {
		return "", apperr.NotFound("Channel tidak ditemukan")
	}
	if err != nil {
		return "", apperr.Internal("gagal memuat channel")
	}
	if status != "connected" || strings.TrimSpace(token) == "" {
		return "", apperr.BadRequest("Channel WhatsApp belum terhubung")
	}
	if provider != "meta_cloud" {
		return "", apperr.BadRequest("Provider channel belum didukung")
	}
	return token, nil
}

func fetchMessageMediaBytes(ctx context.Context, tenantSchema string, row *messageMediaRow) ([]byte, string, error) {
	if row == nil {
		return nil, "", apperr.NotFound("Pesan tidak ditemukan")
	}
	if !mediaDownloadTypes[strings.ToLower(row.Type)] {
		return nil, "", apperr.BadRequest("Pesan ini bukan media")
	}

	if s3Key := extractS3KeyFromMetadata(row.Metadata); s3Key != "" && mediastorage.Configured() {
		data, mime, err := mediastorage.Get(ctx, s3Key)
		if err == nil && len(data) > 0 {
			return data, extractPersistedMimeFromMetadata(row.Metadata, mime), nil
		}
		rlog.Warn("inbox media s3 get failed, fallback to meta proxy",
			"err", err,
			"messageId", row.ID,
			"s3Key", s3Key,
		)
	}

	mediaID := whatsapp.ExtractMediaIDFromRaw(row.Type, row.Metadata)
	if mediaID == "" {
		return nil, "", apperr.NotFound("Media tidak tersedia untuk pesan ini")
	}

	if cached, ok := loadCachedInboxMedia(ctx, tenantSchema, row.ID); ok {
		mime := cached.MimeType
		if mime == "" {
			mime = defaultMimeForMessageType(row.Type)
		}
		return cached.Data, mime, nil
	}

	conn, err := tConn(ctx, tenantSchema)
	if err != nil {
		return nil, "", apperr.Internal("database connection failed")
	}
	defer appdb.CloseTenantConn(conn)

	token, err := loadChannelAccessToken(ctx, conn, row.ChannelID)
	if err != nil {
		return nil, "", err
	}

	dl, err := whatsapp.DownloadMedia(ctx, token, mediaID)
	if err != nil {
		rlog.Warn("inbox media download failed", "err", err, "messageId", row.ID)
		return nil, "", apperr.Unavailable("Gagal mengunduh media WhatsApp")
	}
	mime := dl.MimeType
	if mime == "" {
		mime = defaultMimeForMessageType(row.Type)
	}
	storeCachedInboxMedia(ctx, tenantSchema, row.ID, mime, dl.Data)
	return dl.Data, mime, nil
}

// FetchMessageMediaBytes downloads WhatsApp media for an inbox message (shared with AI jobs).
func FetchMessageMediaBytes(ctx context.Context, tenantSchema, messageID string) ([]byte, string, error) {
	conn, err := tConn(ctx, tenantSchema)
	if err != nil {
		return nil, "", apperr.Internal("database connection failed")
	}
	defer appdb.CloseTenantConn(conn)
	row, err := loadMessageMediaRow(ctx, conn, messageID)
	if err != nil {
		return nil, "", err
	}
	return fetchMessageMediaBytes(ctx, tenantSchema, row)
}

func messageIDFromMediaPath(req *http.Request) string {
	if v := req.PathValue("messageId"); v != "" {
		return v
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	for i, p := range parts {
		if p == "messages" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// GetMessageMedia proxies WhatsApp media for an inbox message (auth required).
//
//encore:api auth raw method=GET path=/api/v1/inbox/messages/:messageId/media
func GetMessageMedia(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	user, err := appauth.AuthenticateHTTP(ctx, req)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	messageID := strings.TrimSpace(messageIDFromMediaPath(req))
	if messageID == "" {
		http.Error(w, "pesan tidak valid", http.StatusBadRequest)
		return
	}

	conn, err := tConn(ctx, user.TenantSchema)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer appdb.CloseTenantConn(conn)

	row, err := loadMessageMediaRow(ctx, conn, messageID)
	if err != nil {
		writeInboxMediaError(w, err)
		return
	}

	data, mime, err := fetchMessageMediaBytes(ctx, user.TenantSchema, row)
	if err != nil {
		writeInboxMediaError(w, err)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeInboxMediaError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	code := http.StatusBadRequest
	switch errs.Code(err) {
	case errs.NotFound:
		code = http.StatusNotFound
	case errs.Unavailable:
		code = http.StatusBadGateway
	case errs.Unauthenticated:
		code = http.StatusUnauthorized
	case errs.PermissionDenied:
		code = http.StatusForbidden
	case errs.Internal:
		code = http.StatusInternalServerError
	}
	http.Error(w, err.Error(), code)
}
