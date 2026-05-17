package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const defaultSessionTTL = 7 * 24 * time.Hour // 7 days

type SessionData struct {
	AccountID    string `json:"accountId"`
	TenantID     string `json:"tenantId"`
	TenantSchema string `json:"tenantSchema"`
	Role         string `json:"role"`
	Email        string `json:"email"`
	Name         string `json:"name"`
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
		opt, err := redis.ParseURL(secrets.RedisURL)
		if err != nil {
			panic("auth: invalid REDIS_URL: " + err.Error())
		}
		rdb = redis.NewClient(opt)
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
