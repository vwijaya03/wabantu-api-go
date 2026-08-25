package ai

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/usage"
)

const (
	summarizeInflightKeyPrefix = "ai:summarize:inflight:"
	summarizeInflightTTL       = 2 * time.Minute
	summarizeFailCountPrefix   = "ai:summarize:fail:"
	summarizeHandlerTimeout    = 45 * time.Second
)

// SummarizeRequest is published after every 20 messages in a conversation.
type SummarizeRequest struct {
	TenantSchema   string `json:"tenantSchema"`
	ConversationID string `json:"conversationId"`
}

// SummarizeTopic triggers background conversation summarization.
var SummarizeTopic = pubsub.NewTopic[SummarizeRequest]("conversation-summarize", pubsub.TopicConfig{
	DeliveryGuarantee: pubsub.AtLeastOnce,
})

var _ = pubsub.NewSubscription(SummarizeTopic, "summarizer", pubsub.SubscriptionConfig[SummarizeRequest]{
	Handler:     handleSummarize,
	RetryPolicy: &pubsub.RetryPolicy{MaxRetries: 3},
})

func handleSummarize(ctx context.Context, req SummarizeRequest) error {
	if !acquireSummarizeInflight(ctx, req.ConversationID) {
		rlog.Info("summarize skipped: already inflight", "conversationId", req.ConversationID)
		return nil
	}
	defer releaseSummarizeInflight(ctx, req.ConversationID)

	rlog.Info("summarize triggered", "schema", req.TenantSchema, "conversationId", req.ConversationID)

	runCtx, cancel := context.WithTimeout(ctx, summarizeHandlerTimeout)
	defer cancel()

	err := SummarizeConversation(runCtx, req.TenantSchema, req.ConversationID)
	if err == nil {
		return nil
	}
	if IsAnthropicRetryable(err) {
		return err
	}
	recordSummarizeFailure(ctx, req.TenantSchema, req.ConversationID, err)
	rlog.Warn("summarize degraded (acked)",
		"schema", req.TenantSchema,
		"conversationId", req.ConversationID,
		"retryable", IsAnthropicRetryable(err),
		"permanent", IsAnthropicPermanent(err),
		"err", err,
	)
	return nil
}

// TryPublishSummarize enqueues background summarization with inflight dedupe.
func TryPublishSummarize(ctx context.Context, tenantSchema, convoID string) {
	if tenantSchema == "" || convoID == "" {
		return
	}
	if !acquireSummarizeInflight(ctx, convoID) {
		rlog.Info("summarize publish skipped: inflight", "conversationId", convoID)
		return
	}
	if _, err := SummarizeTopic.Publish(ctx, SummarizeRequest{
		TenantSchema:   tenantSchema,
		ConversationID: convoID,
	}); err != nil {
		releaseSummarizeInflight(ctx, convoID)
		rlog.Warn("publish summarize job failed", "err", err, "conversationId", convoID)
	}
}

func acquireSummarizeInflight(ctx context.Context, convoID string) bool {
	if svc == nil || svc.rdb == nil || convoID == "" {
		return true
	}
	ok, err := svc.rdb.SetNX(ctx, summarizeInflightKeyPrefix+convoID, "1", summarizeInflightTTL).Result()
	return err == nil && ok
}

func releaseSummarizeInflight(ctx context.Context, convoID string) {
	if svc == nil || svc.rdb == nil || convoID == "" {
		return
	}
	_ = svc.rdb.Del(ctx, summarizeInflightKeyPrefix+convoID).Err()
}

func recordSummarizeFailure(ctx context.Context, tenantSchema, convoID string, err error) {
	if svc == nil || svc.rdb == nil {
		return
	}
	key := summarizeFailCountPrefix + tenantSchema
	_ = svc.rdb.Incr(ctx, key).Err()
	_ = svc.rdb.Expire(ctx, key, 30*24*time.Hour).Err()
	rlog.Warn("summarize failure recorded",
		"schema", tenantSchema,
		"conversationId", convoID,
		"err", err,
	)
}

// SummarizeConversation loads messages since the last summary, calls Anthropic
// to produce a 3-5 sentence summary in Bahasa Indonesia, and stores the result.
func SummarizeConversation(ctx context.Context, tenantSchema, convoID string) error {
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return fmt.Errorf("get tenant DB: %w", err)
	}

	lastSummaryTime, err := getLastSummaryTime(ctx, db, tenantSchema, convoID)
	if err != nil {
		return fmt.Errorf("get last summary time: %w", err)
	}

	messages, err := loadMessagesSince(ctx, db, tenantSchema, convoID, lastSummaryTime)
	if err != nil {
		return fmt.Errorf("load messages: %w", err)
	}
	if len(messages) == 0 {
		rlog.Info("no new messages to summarize", "conversationId", convoID)
		return nil
	}

	var lines []string
	for _, m := range messages {
		who := "Pelanggan"
		switch m.Author {
		case "ai":
			who = "AI"
		case "human":
			who = "Staff"
		case "system":
			who = "Sistem"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", who, m.Body))
	}
	conversationText := strings.Join(lines, "\n")

	businessCtx := ""
	if profile, pErr := loadBusinessProfile(ctx, tenantScope{q: poolQuerier{pool: db}, sch: appdb.SchemaSQL{Schema: tenantSchema}}); pErr == nil && profile != nil {
		businessCtx = fmt.Sprintf(
			"Konteks bisnis tenant (katalog resmi — pesanan di luar ini TIDAK valid):\n- Nama: %s\n- Produk/layanan: %s\n\n",
			profile.BusinessName,
			strOrEmpty(profile.ProductsServices),
		)
	}

	client := NewAnthropicClient(secrets.AnthropicApiKey, AnthropicConfig{
		Model:     DefaultHaikuAPIID(),
		MaxTokens: 300,
	})
	systemPrompt := "Kamu merangkum percakapan customer service WhatsApp. Jangan tulis bahwa pelanggan berhasil memesan barang yang tidak ada di katalog bisnis tenant."
	userPrompt := fmt.Sprintf(
		"%sRangkum percakapan ini dalam 3-5 kalimat bahasa Indonesia. Fokus topik yang relevan dengan bisnis tenant. Jika pelanggan minta produk di luar katalog, sebutkan itu di luar scope.\n\nPercakapan:\n%s",
		businessCtx,
		conversationText,
	)

	model := DefaultHaikuAPIID()
	summary, compUsage, err := client.CompleteText(ctx, model, systemPrompt, userPrompt, 300)
	if err != nil {
		return err
	}

	_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   tenantSchema,
		ConversationID: convoID,
		Purpose:        usage.PurposeConversationSummary,
		Path:           "conversation_summary",
		Reason:         "ai_generated",
		Model:          model,
		Tier:           "haiku",
		LLMUsed:        true,
		InputTokens:    compUsage.InputTokens,
		OutputTokens:   compUsage.OutputTokens,
	})
	if compUsage.InputTokens+compUsage.OutputTokens > 0 {
		_ = usage.RecordEvent(ctx, tenantSchema, "ai_token", compUsage.InputTokens+compUsage.OutputTokens, nil)
	}

	err = storeSummary(ctx, db, tenantSchema, convoID, summary, len(messages))
	if err != nil {
		return fmt.Errorf("store summary: %w", err)
	}

	rlog.Info("conversation summarized", "conversationId", convoID, "messageCount", len(messages), "summaryLen", len(summary))
	return nil
}

// GetLatestSummary returns the most recent conversation summary, or empty string if none.
func GetLatestSummary(ctx context.Context, tenantSchema, convoID string) (string, error) {
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return "", fmt.Errorf("get tenant DB: %w", err)
	}

	q := fmt.Sprintf(`SELECT summary FROM %q.conversation_summary
		WHERE conversation_id = $1 ORDER BY created_at DESC LIMIT 1`, tenantSchema)
	var summary string
	err = appdb.PoolQueryRow(ctx, db, q, convoID).Scan(&summary)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query summary: %w", err)
	}
	return summary, nil
}

// ShouldTriggerSummary returns true when the message count since last summary >= 20.
func ShouldTriggerSummary(ctx context.Context, tenantSchema, convoID string) (bool, error) {
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return false, err
	}

	lastTime, err := getLastSummaryTime(ctx, db, tenantSchema, convoID)
	if err != nil {
		return false, err
	}

	q := fmt.Sprintf(`SELECT COUNT(*) FROM %q.message
		WHERE conversation_id = $1 AND created_at > $2`, tenantSchema)
	var count int
	err = appdb.PoolQueryRow(ctx, db, q, convoID, lastTime).Scan(&count)
	if err != nil {
		return false, err
	}
	return count >= 20, nil
}

func getLastSummaryTime(ctx context.Context, db *sql.DB, schema, convoID string) (time.Time, error) {
	q := fmt.Sprintf(`SELECT created_at FROM %q.conversation_summary
		WHERE conversation_id = $1 ORDER BY created_at DESC LIMIT 1`, schema)
	var t time.Time
	err := appdb.PoolQueryRow(ctx, db, q, convoID).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	return t, err
}

func loadMessagesSince(ctx context.Context, db *sql.DB, schema, convoID string, since time.Time) ([]HistoryMessage, error) {
	q := fmt.Sprintf(`SELECT author, body, type FROM %q.message
		WHERE conversation_id = $1 AND created_at > $2
		ORDER BY created_at ASC LIMIT 100`, schema)

	rows, err := appdb.QueryContextPool(ctx, db, q, convoID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []HistoryMessage
	for rows.Next() {
		var m HistoryMessage
		if err := rows.Scan(&m.Author, &m.Body, &m.Type); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func storeSummary(ctx context.Context, db *sql.DB, schema, convoID, summary string, msgCount int) error {
	q := fmt.Sprintf(`INSERT INTO %q.conversation_summary
		(conversation_id, summary, message_count, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (conversation_id) DO UPDATE SET
			summary = EXCLUDED.summary,
			message_count = EXCLUDED.message_count,
			updated_at = NOW()`, schema)
	_, err := appdb.ExecPool(ctx, db, q, convoID, summary, msgCount)
	return err
}

func getTenantDB(_ context.Context, _ string) (*sql.DB, error) {
	return aiDB.Stdlib(), nil
}
