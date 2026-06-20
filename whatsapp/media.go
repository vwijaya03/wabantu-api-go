package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxDownloadMediaBytes = 10 << 20 // 10 MiB

// MediaDownload holds bytes fetched from Meta Cloud API.
type MediaDownload struct {
	Data     []byte
	MimeType string
}

type mediaMetaResponse struct {
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// DownloadMedia resolves a WhatsApp media ID and downloads the file content.
func DownloadMedia(ctx context.Context, accessToken, mediaID string) (*MediaDownload, error) {
	mediaID = strings.TrimSpace(mediaID)
	accessToken = strings.TrimSpace(accessToken)
	if mediaID == "" {
		return nil, fmt.Errorf("empty media id")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("missing access token")
	}

	metaURL := fmt.Sprintf("https://graph.facebook.com/%s/%s", graphVersion, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create meta request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch media meta: %w", err)
	}
	defer resp.Body.Close()
	metaBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meta media meta status=%d body=%s", resp.StatusCode, truncate(string(metaBody), 300))
	}

	var meta mediaMetaResponse
	if err := json.Unmarshal(metaBody, &meta); err != nil {
		return nil, fmt.Errorf("parse media meta: %w", err)
	}
	downloadURL := strings.TrimSpace(meta.URL)
	if downloadURL == "" {
		return nil, fmt.Errorf("meta media meta missing url")
	}
	if meta.FileSize > maxDownloadMediaBytes {
		return nil, fmt.Errorf("media too large: %d bytes", meta.FileSize)
	}

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	dlReq.Header.Set("Authorization", "Bearer "+accessToken)

	dlResp, err := client.Do(dlReq)
	if err != nil {
		return nil, fmt.Errorf("download media: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode < 200 || dlResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(dlResp.Body, 4<<10))
		return nil, fmt.Errorf("download media status=%d body=%s", dlResp.StatusCode, truncate(string(body), 300))
	}

	data, err := io.ReadAll(io.LimitReader(dlResp.Body, maxDownloadMediaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read media body: %w", err)
	}
	if len(data) > maxDownloadMediaBytes {
		return nil, fmt.Errorf("media exceeds max size")
	}

	mime := strings.TrimSpace(meta.MimeType)
	if mime == "" {
		mime = strings.TrimSpace(dlResp.Header.Get("Content-Type"))
	}
	if mime == "" {
		mime = "application/octet-stream"
	}

	return &MediaDownload{Data: data, MimeType: mime}, nil
}

// ExtractMediaIDFromRaw reads Meta webhook message JSON stored in message.metadata.
func ExtractMediaIDFromRaw(msgType string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Image    *struct{ ID string `json:"id"` } `json:"image"`
		Video    *struct{ ID string `json:"id"` } `json:"video"`
		Audio    *struct{ ID string `json:"id"` } `json:"audio"`
		Document *struct{ ID string `json:"id"` } `json:"document"`
		Sticker  *struct{ ID string `json:"id"` } `json:"sticker"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(msgType)) {
	case "image", "sticker":
		if payload.Image != nil {
			return strings.TrimSpace(payload.Image.ID)
		}
		if payload.Sticker != nil {
			return strings.TrimSpace(payload.Sticker.ID)
		}
	case "video":
		if payload.Video != nil {
			return strings.TrimSpace(payload.Video.ID)
		}
	case "audio":
		if payload.Audio != nil {
			return strings.TrimSpace(payload.Audio.ID)
		}
	case "document":
		if payload.Document != nil {
			return strings.TrimSpace(payload.Document.ID)
		}
	}
	return ""
}

// InboundMessagePreview formats conversation list preview for non-text inbound.
func InboundMessagePreview(msgType, body string) string {
	body = strings.TrimSpace(body)
	switch strings.ToLower(strings.TrimSpace(msgType)) {
	case "image", "sticker":
		if body != "" {
			return "📷 " + truncate(body, 270)
		}
		return "📷 Gambar"
	case "video":
		if body != "" {
			return "🎬 " + truncate(body, 270)
		}
		return "🎬 Video"
	case "audio":
		return "🎤 Audio"
	case "document":
		if body != "" {
			return "📎 " + truncate(body, 270)
		}
		return "📎 Dokumen"
	case "location":
		if body != "" {
			return "📍 " + truncate(body, 270)
		}
		return "📍 Lokasi"
	default:
		if body != "" {
			return truncate(body, 280)
		}
		return msgType
	}
}
