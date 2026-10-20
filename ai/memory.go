package ai

import (
	"context"
	"database/sql"
	"encore.app/wabantu/shared/llm"
	"fmt"
	"strings"
	"time"

	"encore.dev/pubsub"
	"encore.dev/rlog"

	"encore.app/wabantu/internal/salesbrain"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/usage"
)

const (
	summarizeInflightKeyPrefix = "ai:summarize:inflight:"
	summarizeInflightTTL       = 2 * time.Minute
	summarizeFailCountPrefix   = "ai:summarize:fail:"
	summarizeHandlerTimeout    = 45 * time.Second
)

// SummarizeRequest is published after every 20 messages in a conversation, or when a buyer returns after 72h.
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

// Summary trigger thresholds.
const (
	// summaryMessageThreshold: this many unsummarized messages trigger a summary.
	summaryMessageThreshold = 20
	// summaryReturnMinMessages and salesbrain.SummarySessionWindow: a returning buyer (the newest
	// message follows the one before it by at least the window) with at least this many
	// unsummarized messages gets the previous visit summarized, even below the threshold.
	summaryReturnMinMessages = 4
	// summaryLoadLimit is how many of the newest unsummarized messages one run reads.
	summaryLoadLimit = 100
	summaryMaxTokens = 300
)

// summaryRow is one stored conversation_summary.
type summaryRow struct {
	Summary      string
	MessageCount int
	UpdatedAt    time.Time
}

const summarizerSystemPrompt = "Kamu merangkum percakapan customer service WhatsApp sebagai ingatan internal untuk model AI lain. " +
	"Jangan tulis bahwa pelanggan berhasil memesan barang yang tidak ada di katalog bisnis tenant."

// buildSummarizerPrompt builds the user prompt of the rolling summary: the previous summary (when
// there is one) plus the new messages, to be merged into one updated summary.
func buildSummarizerPrompt(businessCtx, prevSummary, conversationText string) string {
	var sb strings.Builder
	sb.WriteString(businessCtx)
	if prev := strings.TrimSpace(prevSummary); prev != "" {
		sb.WriteString("Ringkasan sebelumnya:\n")
		sb.WriteString(prev)
		sb.WriteString("\n\nPesan baru:\n")
		sb.WriteString(conversationText)
		sb.WriteString("\n\nGabungkan ringkasan sebelumnya dengan pesan baru menjadi SATU ringkasan terbaru.")
	} else {
		sb.WriteString("Percakapan:\n")
		sb.WriteString(conversationText)
		sb.WriteString("\n\nRangkum percakapan ini.")
	}
	sb.WriteString(" Maksimal 5 kalimat bahasa Indonesia, tanpa daftar atau markdown. " +
		"Pertahankan preferensi pembeli, pertanyaan yang diajukan, dan produk yang diminati. " +
		"Jangan anggap status pesanan, harga, alamat, atau pembayaran sebagai fakta final (itu dicatat sistem terpisah); " +
		"jangan tulis nomor telepon, alamat, atau jumlah uang. " +
		"Buang hal yang sudah selesai atau tidak relevan lagi. " +
		"Jika pelanggan minta produk di luar katalog, sebutkan itu di luar scope.")
	return sb.String()
}

// SummarizeConversation rolls the conversation summary forward: the previous summary plus the
// messages since it was last updated go to the memory model, which returns one merged summary
// (at most about 5 sentences in Bahasa Indonesia). message_count stays cumulative.
func SummarizeConversation(ctx context.Context, tenantSchema, convoID string) error {
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return fmt.Errorf("get tenant DB: %w", err)
	}

	prev, err := loadSummaryRow(ctx, db, tenantSchema, convoID)
	if err != nil {
		return fmt.Errorf("get previous summary: %w", err)
	}

	messages, err := loadMessagesSince(ctx, db, tenantSchema, convoID, prev.UpdatedAt)
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

	memCfg := AnthropicConfig{Model: DefaultHaikuAPIID(), MaxTokens: summaryMaxTokens}
	userPrompt := buildSummarizerPrompt(businessCtx, prev.Summary, conversationText)

	summary, compUsage, model, err := CompleteViaRegistry(ctx, secrets.AnthropicAPIKey, memCfg, "", llm.FnMemory,
		DefaultHaikuAPIID(), summarizerSystemPrompt, userPrompt, summaryMaxTokens)
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

	total := prev.MessageCount + len(messages)
	err = storeSummary(ctx, db, tenantSchema, convoID, summary, total)
	if err != nil {
		return fmt.Errorf("store summary: %w", err)
	}

	rlog.Info("conversation summarized", "conversationId", convoID, "newMessages", len(messages), "messageCount", total, "summaryLen", len(summary), "rolling", prev.Summary != "")
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

// getSummaryWithTime returns the stored summary with its updated_at. A conversation without a
// summary returns the zero summaryRow and no error.
func getSummaryWithTime(ctx context.Context, tenantSchema, convoID string) (summaryRow, error) {
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return summaryRow{}, fmt.Errorf("get tenant DB: %w", err)
	}
	return loadSummaryRow(ctx, db, tenantSchema, convoID)
}

func loadSummaryRow(ctx context.Context, db *sql.DB, schema, convoID string) (summaryRow, error) {
	q := fmt.Sprintf(`SELECT summary, message_count, updated_at FROM %q.conversation_summary
		WHERE conversation_id = $1 LIMIT 1`, schema)
	var r summaryRow
	err := appdb.PoolQueryRow(ctx, db, q, convoID).Scan(&r.Summary, &r.MessageCount, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return summaryRow{}, nil
	}
	if err != nil {
		return summaryRow{}, fmt.Errorf("query summary: %w", err)
	}
	return r, nil
}

// ShouldTriggerSummary returns true when there are at least 20 messages since the summary was
// last updated, or at least 4 of them and the newest message follows the one before it by 72h or
// more (a returning buyer: the previous visit is summarized).
func ShouldTriggerSummary(ctx context.Context, tenantSchema, convoID string) (bool, error) {
	db, err := getTenantDB(ctx, tenantSchema)
	if err != nil {
		return false, err
	}

	lastTime, err := getLastSummaryTime(ctx, db, tenantSchema, convoID)
	if err != nil {
		return false, err
	}

	// One cheap query: the newest messages since the summary, newest first. Twenty rows are all
	// the decision needs (the threshold, the newest two timestamps).
	q := fmt.Sprintf(`SELECT created_at FROM %q.message
		WHERE conversation_id = $1 AND created_at > $2
		ORDER BY created_at DESC LIMIT %d`, tenantSchema, summaryMessageThreshold)
	rows, err := appdb.QueryContextPool(ctx, db, q, convoID, lastTime)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var times []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return false, err
		}
		times = append(times, t)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return summaryTriggerFromTimes(times), nil
}

// summaryTriggerFromTimes decides from the creation times of the unsummarized messages, newest
// first and capped at summaryMessageThreshold rows.
func summaryTriggerFromTimes(newestFirst []time.Time) bool {
	n := len(newestFirst)
	if n >= summaryMessageThreshold {
		return true
	}
	return n >= summaryReturnMinMessages && newestFirst[0].Sub(newestFirst[1]) >= salesbrain.SummarySessionWindow
}

// getLastSummaryTime is when the summary was last updated (zero when there is none). It reads
// updated_at: created_at never changes after the first upsert, which made every later count start
// at the first summary.
func getLastSummaryTime(ctx context.Context, db *sql.DB, schema, convoID string) (time.Time, error) {
	q := fmt.Sprintf(`SELECT updated_at FROM %q.conversation_summary
		WHERE conversation_id = $1 ORDER BY updated_at DESC LIMIT 1`, schema)
	var t time.Time
	err := appdb.PoolQueryRow(ctx, db, q, convoID).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	return t, err
}

// loadMessagesSince returns the newest summaryLoadLimit messages after since, oldest first.
func loadMessagesSince(ctx context.Context, db *sql.DB, schema, convoID string, since time.Time) ([]HistoryMessage, error) {
	q := fmt.Sprintf(`SELECT author, body, type FROM %q.message
		WHERE conversation_id = $1 AND created_at > $2
		ORDER BY created_at DESC LIMIT %d`, schema, summaryLoadLimit)

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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Newest first from SQL; the summarizer reads them in chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
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
