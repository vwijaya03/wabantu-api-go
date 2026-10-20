package ai

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"encore.dev/rlog"
	"github.com/redis/go-redis/v9"
)

const (
	// inboundQuietBase is the quiet window after a message that looks finished; inboundQuiet
	// (3s, the former fixed window) applies only when the latest message looks unfinished
	// (see adaptiveQuiet). inboundMaxWait caps the whole burst.
	inboundQuietBase     = 1200 * time.Millisecond
	inboundQuiet         = 3 * time.Second
	inboundMaxWait       = 8 * time.Second
	inboundBurstGap      = 2 * time.Second
	inboundMaxBurst      = 20
	coalesceKeyTTL       = 2 * time.Minute
	coalesceLockTTL      = 30 * time.Second
	coalesceWatermarkTTL = 24 * time.Hour
)

var (
	errCoalesceBusy   = errors.New("coalesce busy")
	errDropSuperseded = errors.New("outbound superseded")
)

// Kompleksitas: waktu worst-case O(B), ruang O(B). Asumsi B≤20 (cap burst).
func stitchInboundBodies(bodies []string) string {
	var b strings.Builder
	first := true
	for _, s := range bodies {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(s)
	}
	return b.String()
}

// Kompleksitas: waktu worst-case O(n), ruang O(n). Membership id = map (rata-rata O(1) amortized).
func appendBurstIDs(ids []string, id string, max int) []string {
	if max <= 0 {
		max = inboundMaxBurst
	}
	seen := make(map[string]struct{}, len(ids)+1)
	out := make([]string, 0, min(len(ids)+1, max))
	for _, x := range ids {
		if x == "" {
			continue
		}
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
		if len(out) >= max {
			return out
		}
	}
	if id != "" {
		if _, ok := seen[id]; !ok && len(out) < max {
			out = append(out, id)
		}
	}
	return out
}

func idsAfterWatermark(ids []string, wm string) []string {
	if wm == "" {
		return append([]string(nil), ids...)
	}
	for i, id := range ids {
		if id == wm {
			return append([]string(nil), ids[i+1:]...)
		}
	}
	return append([]string(nil), ids...)
}

func flushAt(burstStart, lastInbound time.Time, quiet, maxWait time.Duration) time.Time {
	trailing := lastInbound.Add(quiet)
	if burstStart.IsZero() {
		burstStart = lastInbound
	}
	capAt := burstStart.Add(maxWait)
	if capAt.Before(trailing) {
		return capAt
	}
	return trailing
}

func remainingWait(now, burstStart, lastInbound time.Time, quiet, maxWait time.Duration) time.Duration {
	if lastInbound.IsZero() {
		return 0
	}
	d := flushAt(burstStart, lastInbound, quiet, maxWait).Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

func jobSuperseded(jobInbound, lastInbound time.Time) bool {
	return lastInbound.After(jobInbound)
}

func effectiveInboundText(body, override string) string {
	if s := strings.TrimSpace(override); s != "" {
		return s
	}
	return body
}

func shouldCoalesceInboundType(msgType string) bool {
	return strings.EqualFold(strings.TrimSpace(msgType), "text")
}

type coalesceFlush struct {
	Skip     bool
	Stitched string
	LastID   string
	WaitMs   int64 // time this job spent in the quiet window (coalesceWaitMs in the metadata)
}

// bareGreetings are openers that are usually followed by the real question in a second message.
var bareGreetings = map[string]struct{}{
	"halo": {}, "hallo": {}, "hello": {}, "hai": {}, "hi": {}, "hey": {}, "hei": {}, "kak": {}, "kakak": {},
	"min": {}, "admin": {}, "permisi": {}, "pagi": {}, "siang": {}, "sore": {}, "malam": {},
	"selamat pagi": {}, "selamat siang": {}, "selamat sore": {}, "selamat malam": {},
	"assalamualaikum": {}, "assalamu'alaikum": {}, "halo kak": {}, "hai kak": {}, "hi kak": {}, "hello kak": {},
	"p": {}, "ping": {},
}

// adaptiveQuiet picks the quiet window from the latest message of the burst: inboundQuietBase
// when it looks finished, inboundQuiet (the long window) when the buyer is likely still typing:
//   - it ends with "," ":" "-" or the word "dan" / "and";
//   - it is a bare greeting ("halo", "hi kak") that usually precedes the question;
//   - it is very short (3 runes or fewer);
//   - a burst is in progress: this is at least the 2nd pending message and it came within
//     inboundBurstGap of the previous one.
//
// Pure function: no clock, no I/O.
func adaptiveQuiet(latest string, pendingCount int, gapSincePrev time.Duration) time.Duration {
	text := strings.TrimSpace(latest)
	if text == "" {
		return inboundQuietBase
	}
	if pendingCount >= 2 && gapSincePrev > 0 && gapSincePrev <= inboundBurstGap {
		return inboundQuiet
	}
	if utf8.RuneCountInString(text) <= 3 {
		return inboundQuiet
	}
	if strings.HasSuffix(text, ",") || strings.HasSuffix(text, ":") || strings.HasSuffix(text, "-") {
		return inboundQuiet
	}
	low := strings.ToLower(text)
	words := strings.Fields(low)
	if n := len(words); n > 0 && (words[n-1] == "dan" || words[n-1] == "and") {
		return inboundQuiet
	}
	greeting := strings.Trim(low, " .!?~,")
	if _, ok := bareGreetings[greeting]; ok {
		return inboundQuiet
	}
	return inboundQuietBase
}

func coalesceKey(schema, convo, suffix string) string {
	return "ai:coalesce:v1:" + schema + ":" + convo + ":" + suffix
}

func coalesceInboundTurn(ctx context.Context, job *InboundAIJob) (*coalesceFlush, error) {
	if job == nil || !shouldCoalesceInboundType(job.InboundType) {
		return nil, nil
	}
	started := time.Now()
	if svc == nil || svc.rdb == nil {
		return nil, nil
	}

	ts, err := openTenantScope(ctx, job.TenantSchema)
	if err != nil {
		rlog.Warn("coalesce: tenant scope failed, process immediately", "err", err)
		return nil, nil
	}
	inbound, err := loadMessage(ctx, ts, job.InboundMessageID)
	if err != nil || inbound == nil {
		rlog.Warn("coalesce: load inbound failed, process immediately", "err", err)
		return nil, nil
	}
	jobAt := inbound.CreatedAt
	if jobAt.IsZero() {
		jobAt = time.Now()
	}

	if err := recordCoalesceInbound(ctx, job, inbound.ID, jobAt); err != nil {
		rlog.Warn("coalesce: redis record failed, process immediately", "err", err)
		return nil, nil
	}

	if err := waitCoalesceQuiet(ctx, ts, job, jobAt); err != nil {
		return nil, err
	}

	st, err := loadCoalesceState(ctx, job.TenantSchema, job.ConversationID)
	if err != nil {
		rlog.Warn("coalesce: load state failed, process immediately", "err", err)
		return nil, nil
	}
	if jobSuperseded(jobAt, st.lastAt) {
		rlog.Info("coalesce: superseded",
			"conversationId", job.ConversationID,
			"inboundId", job.InboundMessageID,
			"lastId", st.lastID,
		)
		return &coalesceFlush{Skip: true}, nil
	}

	lockKey := coalesceKey(job.TenantSchema, job.ConversationID, "lock")
	ok, err := svc.rdb.SetNX(ctx, lockKey, job.InboundMessageID, coalesceLockTTL).Result()
	if err != nil {
		rlog.Warn("coalesce: lock failed, process immediately", "err", err)
		return nil, nil
	}
	if !ok {
		return nil, errCoalesceBusy
	}
	defer func() { _ = svc.rdb.Del(ctx, lockKey).Err() }()

	st, err = loadCoalesceState(ctx, job.TenantSchema, job.ConversationID)
	if err != nil {
		return nil, nil
	}
	if jobSuperseded(jobAt, st.lastAt) {
		return &coalesceFlush{Skip: true}, nil
	}

	pending := idsAfterWatermark(st.ids, st.watermark)
	if len(pending) == 0 {
		return &coalesceFlush{Skip: true}, nil
	}

	bodies, err := loadInboundBodiesInOrder(ctx, ts, pending)
	if err != nil {
		rlog.Warn("coalesce: load bodies failed, process immediately", "err", err)
		return nil, nil
	}
	stitched := stitchInboundBodies(bodies)
	if stitched == "" {
		return &coalesceFlush{Skip: true}, nil
	}
	lastID := pending[len(pending)-1]
	return &coalesceFlush{Stitched: stitched, LastID: lastID, WaitMs: time.Since(started).Milliseconds()}, nil
}

type coalesceState struct {
	ids       []string
	lastID    string
	lastAt    time.Time
	burstAt   time.Time
	watermark string
	gap       time.Duration // time between the latest message and the one before it
}

func recordCoalesceInbound(ctx context.Context, job *InboundAIJob, messageID string, at time.Time) error {
	idsKey := coalesceKey(job.TenantSchema, job.ConversationID, "ids")
	lastKey := coalesceKey(job.TenantSchema, job.ConversationID, "last")
	lastAtKey := coalesceKey(job.TenantSchema, job.ConversationID, "lastAt")
	burstKey := coalesceKey(job.TenantSchema, job.ConversationID, "burst")
	gapKey := coalesceKey(job.TenantSchema, job.ConversationID, "gap")
	nano := strconv.FormatInt(at.UnixNano(), 10)
	// A message arriving after the long quiet window starts a new burst: without this reset the
	// burst start (and so the max-wait cap) would stay stale for a whole active chat.
	prevAt, _ := svc.rdb.Get(ctx, lastAtKey).Result()
	prev := parseUnixNano(prevAt)
	gap := time.Duration(0)
	newBurst := prev.IsZero() || at.Sub(prev) > inboundQuiet
	if !prev.IsZero() && at.After(prev) {
		gap = at.Sub(prev)
	}
	pipe := svc.rdb.Pipeline()
	pipe.Set(ctx, gapKey, strconv.FormatInt(gap.Milliseconds(), 10), coalesceKeyTTL)
	if newBurst {
		pipe.Del(ctx, burstKey)
	}
	pipe.RPush(ctx, idsKey, messageID)
	pipe.LTrim(ctx, idsKey, -int64(inboundMaxBurst), -1)
	pipe.Expire(ctx, idsKey, coalesceKeyTTL)
	pipe.Set(ctx, lastKey, messageID, coalesceKeyTTL)
	pipe.Set(ctx, lastAtKey, nano, coalesceKeyTTL)
	pipe.SetNX(ctx, burstKey, nano, coalesceKeyTTL)
	pipe.Expire(ctx, burstKey, coalesceKeyTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func loadCoalesceState(ctx context.Context, schema, convo string) (coalesceState, error) {
	var st coalesceState
	idsKey := coalesceKey(schema, convo, "ids")
	lastKey := coalesceKey(schema, convo, "last")
	lastAtKey := coalesceKey(schema, convo, "lastAt")
	burstKey := coalesceKey(schema, convo, "burst")
	wmKey := coalesceKey(schema, convo, "wm")
	gapKey := coalesceKey(schema, convo, "gap")
	pipe := svc.rdb.Pipeline()
	gapCmd := pipe.Get(ctx, gapKey)
	idsCmd := pipe.LRange(ctx, idsKey, 0, -1)
	lastCmd := pipe.Get(ctx, lastKey)
	lastAtCmd := pipe.Get(ctx, lastAtKey)
	burstCmd := pipe.Get(ctx, burstKey)
	wmCmd := pipe.Get(ctx, wmKey)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		// pipeline still fills cmds; ignore redis.Nil
	}
	rawIDs, _ := idsCmd.Result()
	st.ids = appendBurstIDs(nil, "", inboundMaxBurst)
	for _, id := range rawIDs {
		st.ids = appendBurstIDs(st.ids, id, inboundMaxBurst)
	}
	st.lastID, _ = lastCmd.Result()
	st.watermark, _ = wmCmd.Result()
	st.lastAt = parseUnixNano(lastAtCmd.Val())
	st.burstAt = parseUnixNano(burstCmd.Val())
	if ms, err := strconv.ParseInt(gapCmd.Val(), 10, 64); err == nil && ms > 0 {
		st.gap = time.Duration(ms) * time.Millisecond
	}
	if st.lastAt.IsZero() {
		st.lastAt = time.Now()
	}
	if st.burstAt.IsZero() {
		st.burstAt = st.lastAt
	}
	return st, nil
}

func parseUnixNano(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func waitCoalesceQuiet(ctx context.Context, ts tenantScope, job *InboundAIJob, jobAt time.Time) error {
	var bodyID, body string
	for {
		st, err := loadCoalesceState(ctx, job.TenantSchema, job.ConversationID)
		if err != nil {
			return nil
		}
		if jobSuperseded(jobAt, st.lastAt) {
			return nil
		}
		if st.lastID != "" && st.lastID != bodyID {
			bodyID = st.lastID
			body = ""
			if m, err := loadMessage(ctx, ts, st.lastID); err == nil && m != nil {
				body = m.Body
			}
		}
		pending := len(idsAfterWatermark(st.ids, st.watermark))
		quiet := adaptiveQuiet(body, pending, st.gap)
		wait := remainingWait(time.Now(), st.burstAt, st.lastAt, quiet, inboundMaxWait)
		if wait <= 0 {
			return nil
		}
		if dl, ok := ctx.Deadline(); ok {
			budget := time.Until(dl) - 8*time.Second
			if budget <= 0 {
				return nil
			}
			if wait > budget {
				wait = budget
			}
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func markCoalesceWatermark(ctx context.Context, schema, convo, lastID string) {
	if svc == nil || svc.rdb == nil || lastID == "" {
		return
	}
	_ = svc.rdb.Set(ctx, coalesceKey(schema, convo, "wm"), lastID, coalesceWatermarkTTL).Err()
}

func coalesceLastID(ctx context.Context, job *InboundAIJob) string {
	if svc == nil || svc.rdb == nil || job == nil {
		return ""
	}
	id, _ := svc.rdb.Get(ctx, coalesceKey(job.TenantSchema, job.ConversationID, "last")).Result()
	return id
}

func shouldDropCoalescedSend(ctx context.Context) bool {
	ac, ok := ActivityContextFrom(ctx)
	if !ok || !ac.CoalesceFlush || ac.ConversationID == "" || ac.InboundMessageID == "" {
		return false
	}
	if svc == nil || svc.rdb == nil {
		return false
	}
	last, err := svc.rdb.Get(ctx, coalesceKey(ac.TenantSchema, ac.ConversationID, "last")).Result()
	if err != nil || last == "" {
		return false
	}
	return last != ac.InboundMessageID
}

func loadInboundBodiesInOrder(ctx context.Context, ts tenantScope, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Kompleksitas: waktu worst-case O(B) + 1 query, ruang O(B). B≤20.
	args := make([]any, len(ids))
	ph := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		ph[i] = fmt.Sprintf("$%d", i+1)
	}
	q := fmt.Sprintf(
		`SELECT id, COALESCE(body,'') FROM %s WHERE id IN (%s)`,
		ts.T("message"), strings.Join(ph, ","),
	)
	rows, err := ts.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]string, len(ids))
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		byID[id] = body
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}
