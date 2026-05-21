package ai

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

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
	rlog.Info("summarize triggered", "schema", req.TenantSchema, "conversationId", req.ConversationID)
	return SummarizeConversation(ctx, req.TenantSchema, req.ConversationID)
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

	client := NewAnthropicClient(secrets.AnthropicApiKey, AnthropicConfig{
		Model:     DefaultHaikuAPIID(),
		MaxTokens: 300,
	})
	systemPrompt := "Kamu adalah asisten yang merangkum percakapan customer service."
	userPrompt := fmt.Sprintf(
		"Rangkum percakapan ini dalam 3-5 kalimat bahasa Indonesia. Fokus pada topik utama, keputusan, dan status terakhir.\n\nPercakapan:\n%s",
		conversationText,
	)

	summary, err := client.CompleteText(ctx, DefaultHaikuAPIID(), systemPrompt, userPrompt, 300)
	if err != nil {
		return err
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
	err = db.QueryRowContext(ctx, q, convoID).Scan(&summary)
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
	err = db.QueryRowContext(ctx, q, convoID, lastTime).Scan(&count)
	if err != nil {
		return false, err
	}
	return count >= 20, nil
}

func getLastSummaryTime(ctx context.Context, db *sql.DB, schema, convoID string) (time.Time, error) {
	q := fmt.Sprintf(`SELECT created_at FROM %q.conversation_summary
		WHERE conversation_id = $1 ORDER BY created_at DESC LIMIT 1`, schema)
	var t time.Time
	err := db.QueryRowContext(ctx, q, convoID).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	return t, err
}

func loadMessagesSince(ctx context.Context, db *sql.DB, schema, convoID string, since time.Time) ([]HistoryMessage, error) {
	q := fmt.Sprintf(`SELECT author, body, type FROM %q.message
		WHERE conversation_id = $1 AND created_at > $2
		ORDER BY created_at ASC LIMIT 100`, schema)

	rows, err := db.QueryContext(ctx, q, convoID, since)
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
	_, err := db.ExecContext(ctx, q, convoID, summary, msgCount)
	return err
}

func getTenantDB(_ context.Context, _ string) (*sql.DB, error) {
	return aiDB.Stdlib(), nil
}
