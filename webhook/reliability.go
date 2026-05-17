package webhook

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"encore.dev/rlog"
)

const maxAttempts = 5

// Exponential backoff schedule: 1min, 5min, 30min, 2h, 12h
var retryDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	12 * time.Hour,
}

type WebhookEventStatus string

const (
	StatusPending   WebhookEventStatus = "pending"
	StatusDelivered WebhookEventStatus = "delivered"
	StatusRetrying  WebhookEventStatus = "retrying"
	StatusFailed    WebhookEventStatus = "failed"
)

type WebhookEvent struct {
	ID         string             `json:"id"`
	EventType  string             `json:"eventType"`
	Payload    json.RawMessage    `json:"payload"`
	Status     WebhookEventStatus `json:"status"`
	Attempts   int                `json:"attempts"`
	LastError  string             `json:"lastError,omitempty"`
	CreatedAt  time.Time          `json:"createdAt"`
	UpdatedAt  time.Time          `json:"updatedAt"`
}

// RecordWebhookEvent persists a new event and returns its ID.
func RecordWebhookEvent(ctx context.Context, schema, eventType string, payload json.RawMessage) (string, error) {
	db, err := getTenantDB(ctx, schema)
	if err != nil {
		return "", fmt.Errorf("get tenant DB: %w", err)
	}

	var eventID string
	q := fmt.Sprintf(`INSERT INTO %q.webhook_event
		(event_type, payload, status, attempts, created_at, updated_at)
		VALUES ($1, $2, 'pending', 0, NOW(), NOW())
		RETURNING id`, schema)
	err = db.QueryRowContext(ctx, q, eventType, payload).Scan(&eventID)
	if err != nil {
		return "", fmt.Errorf("insert webhook event: %w", err)
	}

	_, err = WebhookRetryTopic.Publish(ctx, WebhookRetryRequest{
		TenantSchema: schema,
		EventID:      eventID,
		Attempt:      1,
	})
	if err != nil {
		rlog.Error("failed to publish webhook delivery", "eventId", eventID, "err", err)
	}

	return eventID, nil
}

// ProcessWebhookDelivery attempts delivery for a webhook event.
func ProcessWebhookDelivery(ctx context.Context, schema, eventID string) error {
	db, err := getTenantDB(ctx, schema)
	if err != nil {
		return fmt.Errorf("get tenant DB: %w", err)
	}

	var evt WebhookEvent
	q := fmt.Sprintf(`SELECT id, event_type, payload, status, attempts
		FROM %q.webhook_event WHERE id = $1`, schema)
	err = db.QueryRowContext(ctx, q, eventID).Scan(&evt.ID, &evt.EventType, &evt.Payload, &evt.Status, &evt.Attempts)
	if err != nil {
		return fmt.Errorf("load event: %w", err)
	}

	if evt.Status == StatusDelivered {
		return nil
	}
	if evt.Attempts >= maxAttempts {
		return markFailed(ctx, db, schema, eventID, "max attempts exceeded")
	}

	webhookURL, err := getWebhookURL(ctx, db, schema)
	if err != nil || webhookURL == "" {
		return markFailed(ctx, db, schema, eventID, "no webhook URL configured")
	}

	deliveryErr := deliverPayload(ctx, webhookURL, evt.Payload)

	newAttempts := evt.Attempts + 1
	if deliveryErr == nil {
		return markDelivered(ctx, db, schema, eventID, newAttempts)
	}

	rlog.Warn("webhook delivery failed", "eventId", eventID, "attempt", newAttempts, "err", deliveryErr)

	if newAttempts >= maxAttempts {
		return markFailed(ctx, db, schema, eventID, deliveryErr.Error())
	}

	err = markRetrying(ctx, db, schema, eventID, newAttempts, deliveryErr.Error())
	if err != nil {
		return err
	}

	_, err = WebhookRetryTopic.Publish(ctx, WebhookRetryRequest{
		TenantSchema: schema,
		EventID:      eventID,
		Attempt:      newAttempts + 1,
	})
	if err != nil {
		rlog.Error("failed to schedule retry", "eventId", eventID, "err", err)
	}

	return nil
}

func deliverPayload(ctx context.Context, url string, payload json.RawMessage) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "WABantu-Webhook/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("HTTP %d from webhook endpoint", resp.StatusCode)
}

func getWebhookURL(ctx context.Context, db *sql.DB, schema string) (string, error) {
	q := fmt.Sprintf(`SELECT webhook_url FROM %q.business_setting
		WHERE key = 'webhook_url' LIMIT 1`, schema)
	var url string
	err := db.QueryRowContext(ctx, q).Scan(&url)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return url, err
}

func markDelivered(ctx context.Context, db *sql.DB, schema, eventID string, attempts int) error {
	q := fmt.Sprintf(`UPDATE %q.webhook_event
		SET status = 'delivered', attempts = $1, updated_at = NOW()
		WHERE id = $2`, schema)
	_, err := db.ExecContext(ctx, q, attempts, eventID)
	return err
}

func markFailed(ctx context.Context, db *sql.DB, schema, eventID, lastErr string) error {
	q := fmt.Sprintf(`UPDATE %q.webhook_event
		SET status = 'failed', last_error = $1, updated_at = NOW()
		WHERE id = $2`, schema)
	_, err := db.ExecContext(ctx, q, lastErr, eventID)
	return err
}

func markRetrying(ctx context.Context, db *sql.DB, schema, eventID string, attempts int, lastErr string) error {
	q := fmt.Sprintf(`UPDATE %q.webhook_event
		SET status = 'retrying', attempts = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3`, schema)
	_, err := db.ExecContext(ctx, q, attempts, lastErr, eventID)
	return err
}

func getTenantDB(_ context.Context, _ string) (*sql.DB, error) {
	return db.Stdlib(), nil
}
