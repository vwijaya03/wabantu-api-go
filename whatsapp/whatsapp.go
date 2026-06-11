// Package whatsapp provides helpers for the Meta Cloud API (Graph API v21.0).
// This is a library package (no Encore API endpoints) imported by webhook and inbox services.
package whatsapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const graphVersion = "v21.0"

// InboundMessage represents a single parsed inbound WhatsApp message.
type InboundMessage struct {
	ExternalID      string          `json:"externalId"`
	FromPhone       string          `json:"fromPhone"`
	ToPhoneNumberID string          `json:"toPhoneNumberId"`
	ToDisplayPhone  string          `json:"toDisplayPhone"`
	FromDisplayName string          `json:"fromDisplayName"`
	Type            string          `json:"type"` // text | image | audio | video | document | location
	Body            string          `json:"body"`
	Raw             json.RawMessage `json:"raw"`
	ReceivedAt      time.Time       `json:"receivedAt"`
}

// ---------------------------------------------------------------------------
// Send
// ---------------------------------------------------------------------------

// NormalizeRecipient strips non-digits for Meta Cloud API "to" (E.164 without +).
func NormalizeRecipient(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SendText sends a text message via Meta Cloud API and returns the external message ID.
func SendText(ctx context.Context, accessToken, phoneNumberID, to, body string) (string, error) {
	to = NormalizeRecipient(to)
	if to == "" {
		return "", fmt.Errorf("invalid recipient phone")
	}
	accessToken = strings.TrimSpace(accessToken)
	phoneNumberID = strings.TrimSpace(phoneNumberID)
	if accessToken == "" || phoneNumberID == "" {
		return "", fmt.Errorf("missing channel credentials")
	}

	apiURL := fmt.Sprintf("https://graph.facebook.com/%s/%s/messages", graphVersion, phoneNumberID)
	payload, _ := json.Marshal(map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]interface{}{"preview_url": false, "body": body},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("meta API status=%d body=%s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var result struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Messages) == 0 || result.Messages[0].ID == "" {
		return "", fmt.Errorf("meta API returned no message id")
	}
	return result.Messages[0].ID, nil
}

// ---------------------------------------------------------------------------
// Signature verification
// ---------------------------------------------------------------------------

// VerifyWebhookSignature validates the X-Hub-Signature-256 header from Meta.
func VerifyWebhookSignature(payload []byte, signature, appSecret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(payload)
	return hmac.Equal(
		[]byte(strings.ToLower(signature[7:])),
		[]byte(hex.EncodeToString(mac.Sum(nil))),
	)
}

// ---------------------------------------------------------------------------
// Webhook parsing
// ---------------------------------------------------------------------------

// WebhookPhoneNumberID returns metadata.phone_number_id from any Meta WABA webhook payload.
func WebhookPhoneNumberID(payload []byte) string {
	var p webhookPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Object != "whatsapp_business_account" {
		return ""
	}
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			if id := strings.TrimSpace(change.Value.Metadata.PhoneNumberID); id != "" {
				return id
			}
		}
	}
	return ""
}

// ParseWebhook extracts inbound messages from a Meta webhook payload.
func ParseWebhook(payload []byte) []InboundMessage {
	var p webhookPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Object != "whatsapp_business_account" {
		return nil
	}

	var out []InboundMessage
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			v := change.Value
			names := buildContactNameMap(v.Contacts)

			for _, m := range v.Messages {
				msgType, body := classifyMessage(m)
				if msgType == "" {
					continue
				}
				rawBytes, _ := json.Marshal(m)
				tsInt, _ := strconv.ParseInt(m.Timestamp, 10, 64)

				out = append(out, InboundMessage{
					ExternalID:      m.ID,
					FromPhone:       m.From,
					ToPhoneNumberID: v.Metadata.PhoneNumberID,
					ToDisplayPhone:  v.Metadata.DisplayPhoneNumber,
					FromDisplayName: names[m.From],
					Type:            msgType,
					Body:            body,
					Raw:             rawBytes,
					ReceivedAt:      time.Unix(tsInt, 0),
				})
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal webhook types
// ---------------------------------------------------------------------------

type webhookPayload struct {
	Object string         `json:"object"`
	Entry  []webhookEntry `json:"entry"`
}

type webhookEntry struct {
	Changes []webhookChange `json:"changes"`
}

type webhookChange struct {
	Value webhookValue `json:"value"`
}

type webhookValue struct {
	Metadata struct {
		DisplayPhoneNumber string `json:"display_phone_number"`
		PhoneNumberID      string `json:"phone_number_id"`
	} `json:"metadata"`
	Contacts []webhookContact  `json:"contacts"`
	Messages []webhookMessage  `json:"messages"`
}

type webhookContact struct {
	WaID    string `json:"wa_id"`
	Profile struct {
		Name      string `json:"name"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"profile"`
}

type webhookMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      *struct {
		Body string `json:"body"`
	} `json:"text"`
	Image *struct {
		ID      string `json:"id"`
		Caption string `json:"caption"`
	} `json:"image"`
	Audio *struct {
		ID string `json:"id"`
	} `json:"audio"`
	Video *struct {
		ID      string `json:"id"`
		Caption string `json:"caption"`
	} `json:"video"`
	Document *struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Caption  string `json:"caption"`
	} `json:"document"`
	Location *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name"`
	} `json:"location"`
}

func buildContactNameMap(contacts []webhookContact) map[string]string {
	m := make(map[string]string, len(contacts))
	for _, c := range contacts {
		name := c.Profile.Name
		if name == "" {
			name = strings.TrimSpace(c.Profile.FirstName + " " + c.Profile.LastName)
		}
		if c.WaID != "" && name != "" {
			m[c.WaID] = name
		}
	}
	return m
}

func classifyMessage(m webhookMessage) (msgType, body string) {
	switch m.Type {
	case "text":
		body = ""
		if m.Text != nil {
			body = m.Text.Body
		}
		return "text", body
	case "image":
		if m.Image != nil {
			body = m.Image.Caption
		}
		return "image", body
	case "audio":
		return "audio", ""
	case "video":
		if m.Video != nil {
			body = m.Video.Caption
		}
		return "video", body
	case "document":
		if m.Document != nil {
			body = m.Document.Filename
		}
		return "document", body
	case "location":
		if m.Location != nil {
			body = m.Location.Name
		}
		return "location", body
	default:
		return "", ""
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
