// Package inboxrealtime publishes and subscribes to inbox activity events via Redis.
package inboxrealtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const channelPrefix = "wabantu:inbox:"

func Channel(tenantID string) string {
	return channelPrefix + tenantID
}

type ActivityPayload struct {
	Type string `json:"type"`
	At   int64  `json:"at"`
}

// Publish notifies subscribers that inbox data changed.
func Publish(ctx context.Context, rdb *redis.Client, tenantID string) {
	if rdb == nil || tenantID == "" {
		return
	}
	payload, _ := json.Marshal(ActivityPayload{Type: "inbox", At: time.Now().UnixMilli()})
	_ = rdb.Publish(ctx, Channel(tenantID), payload).Err()
}

// PingPayload is sent periodically to keep SSE connections alive.
type PingPayload struct {
	Type string `json:"type"`
}

// WriteSSE writes one Server-Sent Events frame.
func WriteSSE(w interface{ Write([]byte) (int, error) }, data []byte) error {
	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
