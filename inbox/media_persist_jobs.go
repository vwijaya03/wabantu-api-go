package inbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/shared/mediastorage"
	"encore.app/wabantu/usage"
)

// InboxMediaPersistJob persists inbound WhatsApp media to S3 asynchronously.
type InboxMediaPersistJob struct {
	TenantSchema string `json:"tenantSchema"`
	MessageID    string `json:"messageId"`
	MessageType  string `json:"messageType"`
}

// InboxMediaPersistTopic queues background S3 persistence for inbox media.
var InboxMediaPersistTopic = pubsub.NewTopic[*InboxMediaPersistJob]("inbox-media-persist", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(InboxMediaPersistTopic, "inbox-media-persist-handler", pubsub.SubscriptionConfig[*InboxMediaPersistJob]{
	Handler:     handleInboxMediaPersistJob,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

// IsPersistableMediaType reports whether a message type should be persisted to S3.
func IsPersistableMediaType(msgType string) bool {
	return mediaDownloadTypes[strings.ToLower(strings.TrimSpace(msgType))]
}

// PublishInboxMediaPersistJob enqueues S3 persistence for an inbound media message.
func PublishInboxMediaPersistJob(ctx context.Context, job *InboxMediaPersistJob) error {
	if job == nil || job.TenantSchema == "" || job.MessageID == "" {
		return fmt.Errorf("invalid inbox media persist job")
	}
	if !IsPersistableMediaType(job.MessageType) {
		return nil
	}
	_, err := InboxMediaPersistTopic.Publish(ctx, job)
	return err
}

func handleInboxMediaPersistJob(ctx context.Context, job *InboxMediaPersistJob) error {
	if job == nil || job.TenantSchema == "" || job.MessageID == "" {
		return fmt.Errorf("invalid inbox media persist job")
	}
	if !mediastorage.Configured() {
		rlog.Debug("inbox media persist skipped: s3 not configured",
			"schema", job.TenantSchema,
			"messageId", job.MessageID,
		)
		return nil
	}

	conn, err := tConn(ctx, job.TenantSchema)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	row, err := loadMessageMediaRow(ctx, conn, job.MessageID)
	if err != nil {
		return err
	}
	if extractS3KeyFromMetadata(row.Metadata) != "" {
		return nil
	}
	if !IsPersistableMediaType(row.Type) {
		return nil
	}

	data, mime, err := fetchMessageMediaBytes(ctx, job.TenantSchema, row)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty media bytes")
	}

	allowed, remaining, limit := usage.CheckQuota(ctx, job.TenantSchema, "storage_byte")
	if !allowed {
		rlog.Warn("inbox media persist skipped: storage quota exceeded",
			"schema", job.TenantSchema,
			"messageId", job.MessageID,
			"limit", limit,
		)
		return nil
	}
	if remaining >= 0 && len(data) > remaining {
		rlog.Warn("inbox media persist skipped: file exceeds remaining storage quota",
			"schema", job.TenantSchema,
			"messageId", job.MessageID,
			"bytes", len(data),
			"remaining", remaining,
		)
		return nil
	}

	key := mediastorage.BuildInboxMediaKey(job.TenantSchema, job.MessageID, data, mime)
	if err := mediastorage.Put(ctx, key, data, mime); err != nil {
		return err
	}

	if err := usage.RecordEvent(ctx, job.TenantSchema, "storage_byte", len(data), nil); err != nil {
		rlog.Warn("inbox media persist: record storage usage failed",
			"schema", job.TenantSchema,
			"messageId", job.MessageID,
			"err", err,
		)
	}

	patched, err := mergePersistMetadata(row.Metadata, key, mime, len(data))
	if err != nil {
		return fmt.Errorf("merge persist metadata: %w", err)
	}
	if err := updateMessageMetadata(ctx, conn, job.MessageID, patched); err != nil {
		return err
	}

	rlog.Info("inbox media persisted to s3",
		"schema", job.TenantSchema,
		"messageId", job.MessageID,
		"s3Key", key,
		"bytes", len(data),
	)
	return nil
}

func updateMessageMetadata(ctx context.Context, conn *sql.Conn, messageID string, metadata json.RawMessage) error {
	res, err := conn.ExecContext(ctx,
		`UPDATE message SET metadata = $1::jsonb WHERE id = $2`,
		string(metadata), messageID)
	if err != nil {
		return fmt.Errorf("update message metadata: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("message not found: %s", messageID)
	}
	return nil
}

func extractS3KeyFromMetadata(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var meta struct {
		S3Key string `json:"s3Key"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.S3Key)
}

func extractPersistedMimeFromMetadata(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var meta struct {
		MimeType string `json:"mimeType"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fallback
	}
	mime := strings.TrimSpace(meta.MimeType)
	if mime == "" {
		return fallback
	}
	return mime
}

func isMediaPersisted(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var meta struct {
		Persisted bool   `json:"persisted"`
		S3Key     string `json:"s3Key"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return false
	}
	return meta.Persisted && strings.TrimSpace(meta.S3Key) != ""
}

func mergePersistMetadata(existing json.RawMessage, s3Key, mime string, size int) (json.RawMessage, error) {
	m := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &m); err != nil {
			return nil, err
		}
	}
	m["persisted"] = true
	m["s3Key"] = strings.TrimSpace(s3Key)
	m["mimeType"] = strings.TrimSpace(mime)
	m["bytes"] = size
	m["persistedAt"] = time.Now().UTC().Format(time.RFC3339)
	return json.Marshal(m)
}
