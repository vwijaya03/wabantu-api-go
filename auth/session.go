package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"encore.app/wabantu/shared/redisurl"
)

const defaultSessionTTL = 7 * 24 * time.Hour // 7 days

type SessionData struct {
	AccountID    string `json:"accountId"`
	TenantID     string `json:"tenantId"`
	TenantSchema string `json:"tenantSchema"`
	Role         string `json:"role"`
	Email        string `json:"email"`
	Name         string `json:"name"`

	Impersonating     bool   `json:"impersonating,omitempty"`
	ActAsTenantID     string `json:"actAsTenantId,omitempty"`
	ActAsTenantSchema string `json:"actAsTenantSchema,omitempty"`
	ActAsTenantName   string `json:"actAsTenantName,omitempty"`
	ActAsTenantSlug   string `json:"actAsTenantSlug,omitempty"`
	ActAsScope        string `json:"actAsScope,omitempty"`
	ActAsModules      []string `json:"actAsModules,omitempty"`
	ActAsGrantID      string `json:"actAsGrantId,omitempty"`
	ActAsGrantExpiresAt int64  `json:"actAsGrantExpiresAt,omitempty"` // unix sec; 0 = permanent
}

type Session struct {
	SessionID string      `json:"sessionId"`
	Data      SessionData `json:"data"`
}

var (
	redisOnce sync.Once
	rdb       *redis.Client
)

func getRedis() *redis.Client {
	redisOnce.Do(func() {
		client, err := redisurl.NewClient(secrets.RedisURL)
		if err != nil {
			panic("auth: invalid RedisURL: " + err.Error())
		}
		rdb = client
	})
	return rdb
}

func sessionKey(accountID, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", accountID, sessionID)
}

func createSession(ctx context.Context, data SessionData) (*Session, error) {
	sid := uuid.New().String()
	key := sessionKey(data.AccountID, sid)

	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal session: %w", err)
	}
	if err := getRedis().Set(ctx, key, raw, defaultSessionTTL).Err(); err != nil {
		return nil, fmt.Errorf("redis set session: %w", err)
	}
	return &Session{SessionID: sid, Data: data}, nil
}

func getSession(ctx context.Context, accountID, sessionID string) (*SessionData, error) {
	key := sessionKey(accountID, sessionID)
	val, err := getRedis().Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get session: %w", err)
	}
	var data SessionData
	if err := json.Unmarshal(val, &data); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &data, nil
}

func destroySession(ctx context.Context, accountID, sessionID string) error {
	key := sessionKey(accountID, sessionID)
	return getRedis().Del(ctx, key).Err()
}

func touchSession(ctx context.Context, accountID, sessionID string) error {
	key := sessionKey(accountID, sessionID)
	if err := getRedis().Expire(ctx, key, defaultSessionTTL).Err(); err != nil {
		return fmt.Errorf("redis expire session: %w", err)
	}
	return nil
}

func updateSession(ctx context.Context, accountID, sessionID string, data SessionData) error {
	key := sessionKey(accountID, sessionID)
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	ttl, err := getRedis().TTL(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("redis ttl session: %w", err)
	}
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	if err := getRedis().Set(ctx, key, raw, ttl).Err(); err != nil {
		return fmt.Errorf("redis set session: %w", err)
	}
	return nil
}
