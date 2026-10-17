package ai

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/whatsappchannel"

	"encore.app/wabantu/internal/salesbrain"
)

func TestShouldAutoResume(t *testing.T) {
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour)
	recent := now.Add(-30 * time.Minute)
	exactly := now.Add(-AutoResumeAfter)
	cases := []struct {
		name         string
		reason       string
		pausedAt     *time.Time
		staffReplied bool
		want         bool
	}{
		{"ai no answer, old", AiNoAnswerPauseReason, &old, false, true},
		{"human request, old", HandoffReasonHumanRequest, &old, false, true},
		{"ai fallback (legacy name), old", HandoffReasonAIFallback, &old, false, true},
		{"exactly the resume age", HandoffReasonHumanRequest, &exactly, false, true},
		{"too recent", HandoffReasonHumanRequest, &recent, false, false},
		{"staff replied", HandoffReasonHumanRequest, &old, true, false},
		// What the inbox writes when staff take over: the backend default and the reason the
		// dashboard sends, plus a free-text reason and the workflow automation. None may resume.
		{"staff takeover (backend default)", "Diambil alih manual oleh staff", &old, false, false},
		{"staff takeover (inbox UI)", "Takeover manual dari inbox", &old, false, false},
		{"staff takeover (custom text)", "Customer minta refund, saya tangani", &old, false, false},
		{"workflow handoff", "workflow", &old, false, false},
		{"empty reason never", "", &old, false, false},
		{"no timestamp", AiNoAnswerPauseReason, nil, false, false},
	}
	for _, c := range cases {
		if got := shouldAutoResume(c.reason, c.pausedAt, now, c.staffReplied); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// Reasons the pre-change code wrote (rows are still in the DB) count as the AI's own pause, so
// those conversations also stop being silent after AutoResumeAfter.
func TestIsAIInitiatedPause(t *testing.T) {
	for _, r := range []string{
		AiNoAnswerPauseReason, HandoffReasonHumanRequest, HandoffReasonAIFallback,
		"buyer_requested_human", "Auto fallback setelah retry AI gagal", "Sensitive/escalate detected",
		"  human_request  ",
	} {
		if !isAIInitiatedPause(r) {
			t.Errorf("%q must be an AI-initiated pause", r)
		}
	}
	for _, r := range []string{
		"", "   ", "workflow", "image_unhandled", "Diambil alih manual oleh staff", "Takeover manual dari inbox",
	} {
		if isAIInitiatedPause(r) {
			t.Errorf("%q must not be an AI-initiated pause", r)
		}
	}
}

// ── test doubles ─────────────────────────────────────────────────────────────

type recordedExec struct {
	query string
	args  []any
}

type rowFunc func(dest ...any) error

func (f rowFunc) Scan(dest ...any) error { return f(dest...) }

// recordingTS records every Exec and answers the staff-involvement query from its fields.
type recordingTS struct {
	mu           sync.Mutex
	execs        []recordedExec
	staffReplied bool
	assigned     bool
	queryErr     error
	rowsAffected int64
	staffQueries int
}

func (r *recordingTS) T(table string) string { return table }

func (r *recordingTS) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.execs = append(r.execs, recordedExec{query: q, args: args})
	return execResult(r.rowsAffected), nil
}

func (r *recordingTS) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("recordingTS: QueryContext not supported")
}

func (r *recordingTS) QueryRowContext(_ context.Context, _ string, _ ...any) appdb.Scannable {
	r.staffQueries++
	return rowFunc(func(dest ...any) error {
		if r.queryErr != nil {
			return r.queryErr
		}
		if len(dest) != 2 {
			return fmt.Errorf("recordingTS: want 2 scan targets, got %d", len(dest))
		}
		*(dest[0].(*bool)) = r.staffReplied
		*(dest[1].(*bool)) = r.assigned
		return nil
	})
}

type execResult int64

func (n execResult) LastInsertId() (int64, error) { return 0, nil }
func (n execResult) RowsAffected() (int64, error) { return int64(n), nil }

func (r *recordingTS) pauseExecs() []recordedExec {
	var out []recordedExec
	for _, e := range r.execs {
		if strings.Contains(e.query, "ai_paused_at = NOW()") {
			out = append(out, e)
		}
	}
	return out
}

type sentMessage struct {
	text, author, inboundID string
	meta                    AiReplyMeta
}

// captureOutbound swaps the simulator outbound seam and the owner-notification seams.
type notifyCall struct {
	ids                       []string
	kind, title, body, linkTo string
}

type harness struct {
	t     *testing.T
	sent  []sentMessage
	notes []notifyCall
	ts    *recordingTS
}

func newHarness(t *testing.T, owners []string) *harness {
	t.Helper()
	h := &harness{t: t, ts: &recordingTS{rowsAffected: 1}}
	oldSim, oldList, oldCreate := simulatorOutbound, listOwnerAccountIDsFn, createOwnerNotificationFn
	t.Cleanup(func() {
		simulatorOutbound, listOwnerAccountIDsFn, createOwnerNotificationFn = oldSim, oldList, oldCreate
	})
	simulatorOutbound = func(_ *AutoReplyService, _ context.Context, _ tenantScopedQuerier, _ string, _ *dbConversation, text, author, inboundID string, meta AiReplyMeta) error {
		h.sent = append(h.sent, sentMessage{text: text, author: author, inboundID: inboundID, meta: meta})
		return nil
	}
	listOwnerAccountIDsFn = func(context.Context, string) ([]string, error) { return owners, nil }
	createOwnerNotificationFn = func(_ context.Context, ids []string, kind, title, body, link string) error {
		h.notes = append(h.notes, notifyCall{ids: ids, kind: kind, title: title, body: body, linkTo: link})
		return nil
	}
	return h
}

func simChannel() *dbChannel {
	return &dbChannel{ID: "ch1", Provider: whatsappchannel.SimulatorProvider, Status: "connected"}
}

func contactWithLang(lang string) *dbContact {
	c := &dbContact{ID: "k1", PhoneNumber: "6281234567890"}
	if lang != "" {
		c.PreferredLanguage = &lang
	}
	return c
}

func omahProfileWithHours(hours string) *dbBusinessProfile {
	p := &dbBusinessProfile{BusinessName: "Omah Apparel"}
	if hours != "" {
		p.OpeningHours = &hours
	}
	return p
}

func testPayload() AiReplyJobPayload {
	return AiReplyJobPayload{TenantID: "t1", TenantSchema: "tenant_t1", ConversationID: "c1", InboundMessageID: "in-1"}
}

// ── handlers ─────────────────────────────────────────────────────────────────

// A technical failure is never the buyer's problem and never a reason to silence the AI: honest
// reply in the buyer's language, an owner notification, and no pause.
func TestHandleAiSystemErrorNeverPauses(t *testing.T) {
	cases := []struct {
		name, lang, text string
		want             salesbrain.Lang
	}{
		{"english", "en", "how much is the tuna?", salesbrain.LangEN},
		{"indonesian", "id", "tuna berapa harganya kak?", salesbrain.LangID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, []string{"owner-1", "owner-2"})
			s := &AutoReplyService{}
			convo := &dbConversation{ID: "c1", AIHandled: true}
			handled, err := s.handleAiSystemError(context.Background(), h.ts, testPayload(), convo, simChannel(),
				contactWithLang(c.lang), omahProfileWithHours(""), c.text,
				metaNoLLM(reasonAIGenerated, PathHumanHandoff), "llm_error")
			if err != nil || !handled {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			if len(h.sent) != 1 {
				t.Fatalf("want exactly one message, got %d", len(h.sent))
			}
			if want := salesbrain.T(salesbrain.TplSystemBusy, c.want, "Omah Apparel"); h.sent[0].text != want {
				t.Fatalf("text = %q, want %q", h.sent[0].text, want)
			}
			if h.sent[0].meta.Path != PathHumanHandoff || h.sent[0].meta.Reason != reasonAIGenerated {
				t.Fatalf("existing metadata path must be kept for dashboards, got %+v", h.sent[0].meta)
			}
			if len(h.ts.execs) != 0 {
				t.Fatalf("a system error must not touch the conversation (no pause): %+v", h.ts.execs)
			}
			if len(h.notes) != 1 || h.notes[0].kind != notifyKindSystemError {
				t.Fatalf("want one %q notification, got %+v", notifyKindSystemError, h.notes)
			}
			n := h.notes[0]
			if len(n.ids) != 2 || n.linkTo != "/dashboard/inbox" {
				t.Fatalf("owners/link wrong: %+v", n)
			}
			if !strings.Contains(n.body, c.text) {
				t.Fatalf("notification body must carry the buyer message preview, got %q", n.body)
			}
			if strings.Count(strings.TrimSpace(n.body), "\n") != 1 {
				t.Fatalf("notification body must be exactly two lines, got %q", n.body)
			}
		})
	}
}

// Soft handoff (the AI has no grounded answer): tell the buyer, tell the owner, keep serving.
func TestHandleAiNoAnswerHandoffIsSoft(t *testing.T) {
	h := newHarness(t, []string{"owner-1"})
	s := &AutoReplyService{}
	convo := &dbConversation{ID: "c1", AIHandled: true}
	handled, err := s.handleAiNoAnswerHandoff(context.Background(), h.ts, testPayload(), convo, simChannel(),
		contactWithLang("en"), "do you sell wagyu tomahawk?")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(h.sent) != 1 || h.sent[0].text != salesbrain.T(salesbrain.TplCheckWithTeam, salesbrain.LangEN) {
		t.Fatalf("sent = %+v", h.sent)
	}
	if h.sent[0].meta.Path != PathHumanHandoff {
		t.Fatalf("existing metadata path must be kept, got %+v", h.sent[0].meta)
	}
	if len(h.ts.execs) != 0 {
		t.Fatalf("soft handoff must not pause: %+v", h.ts.execs)
	}
	if len(h.notes) != 1 || h.notes[0].kind != notifyKindHandoff {
		t.Fatalf("want one %q notification, got %+v", notifyKindHandoff, h.notes)
	}
}

// Hard handoff: honest takeover text with the opening hours, pause as human_request (auto-resumable),
// owner notification.
func TestHandleBuyerHumanHandoffPausesAsHumanRequest(t *testing.T) {
	h := newHarness(t, []string{"owner-1"})
	s := &AutoReplyService{}
	convo := &dbConversation{ID: "c1", AIHandled: true}
	handled, err := s.handleBuyerHumanHandoff(context.Background(), h.ts, testPayload(), convo, simChannel(),
		contactWithLang("en"), omahProfileWithHours("08:00 - 17:00"), "I want to talk to the admin please")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	want := salesbrain.T(salesbrain.TplHandoffTakeover, salesbrain.LangEN, "Omah Apparel",
		salesbrain.OpeningHoursSentence("08:00 - 17:00", salesbrain.LangEN))
	if len(h.sent) != 1 || h.sent[0].text != want {
		t.Fatalf("sent = %+v, want text %q", h.sent, want)
	}
	if !strings.Contains(h.sent[0].text, "08:00 - 17:00") {
		t.Fatalf("takeover text must carry the opening hours: %q", h.sent[0].text)
	}
	pauses := h.ts.pauseExecs()
	if len(pauses) != 1 {
		t.Fatalf("want exactly one pause, got %+v", h.ts.execs)
	}
	if got := fmt.Sprint(pauses[0].args...); !strings.Contains(got, HandoffReasonHumanRequest) || !strings.Contains(got, "c1") {
		t.Fatalf("pause args = %v, want reason %q for conversation c1", pauses[0].args, HandoffReasonHumanRequest)
	}
	if len(h.notes) != 1 || h.notes[0].kind != notifyKindHandoff {
		t.Fatalf("want one %q notification, got %+v", notifyKindHandoff, h.notes)
	}
	if !isAIInitiatedPause(HandoffReasonHumanRequest) {
		t.Fatal("a human_request pause must be auto-resumable")
	}
}

func TestHandleBuyerHumanHandoffWithoutOpeningHoursHasNoTrailingSpace(t *testing.T) {
	h := newHarness(t, nil)
	s := &AutoReplyService{}
	_, err := s.handleBuyerHumanHandoff(context.Background(), h.ts, testPayload(), &dbConversation{ID: "c1", AIHandled: true},
		simChannel(), contactWithLang("id"), omahProfileWithHours(""), "tolong sambungkan ke cs")
	if err != nil || len(h.sent) != 1 {
		t.Fatalf("err=%v sent=%+v", err, h.sent)
	}
	if got := h.sent[0].text; got != strings.TrimSpace(got) || !strings.Contains(got, "Omah Apparel") {
		t.Fatalf("text = %q", got)
	}
	if len(h.notes) != 0 {
		t.Fatalf("no owner accounts means no notification, got %+v", h.notes)
	}
}

// ── auto-resume ──────────────────────────────────────────────────────────────

func pausedConvo(reason string, pausedAgo time.Duration, now time.Time) *dbConversation {
	at := now.Add(-pausedAgo)
	return &dbConversation{ID: "c1", AIHandled: false, AIPausedAt: &at, HandoffReason: &reason}
}

func TestPausedConversationMayResume(t *testing.T) {
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	oldNow := autoResumeNow
	autoResumeNow = func() time.Time { return now }
	t.Cleanup(func() { autoResumeNow = oldNow })

	cases := []struct {
		name      string
		convo     *dbConversation
		ts        *recordingTS
		want      bool
		wantErr   bool
		wantQuery bool // the staff-involvement query ran
	}{
		{"AI pause, 3h, staff silent", pausedConvo(AiNoAnswerPauseReason, 3*time.Hour, now), &recordingTS{}, true, false, true},
		{"AI pause but staff replied", pausedConvo(AiNoAnswerPauseReason, 3*time.Hour, now), &recordingTS{staffReplied: true}, false, false, true},
		{"AI pause but assigned to staff (PATCH takeover keeps the old reason)", pausedConvo(AiNoAnswerPauseReason, 3*time.Hour, now), &recordingTS{assigned: true}, false, false, true},
		{"too recent: no query at all", pausedConvo(AiNoAnswerPauseReason, 20*time.Minute, now), &recordingTS{}, false, false, false},
		{"staff takeover: no query at all", pausedConvo("Takeover manual dari inbox", 5*time.Hour, now), &recordingTS{}, false, false, false},
		{"query error keeps it paused", pausedConvo(AiNoAnswerPauseReason, 3*time.Hour, now), &recordingTS{queryErr: errors.New("db down")}, false, true, true},
	}
	for _, c := range cases {
		got, err := pausedConversationMayResume(context.Background(), c.ts, c.convo)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("%s: got (%v, %v) want (%v, err=%v)", c.name, got, err, c.want, c.wantErr)
		}
		if ran := c.ts.staffQueries > 0; ran != c.wantQuery {
			t.Errorf("%s: staff query ran=%v, want %v", c.name, ran, c.wantQuery)
		}
	}
	// A conversation without handoff_reason or timestamp (e.g. paused by a workflow rule).
	if got, _ := pausedConversationMayResume(context.Background(), &recordingTS{}, &dbConversation{ID: "c1"}); got {
		t.Error("no reason / no timestamp must never resume")
	}
}

func TestResumeAIAfterPauseApologisesOnce(t *testing.T) {
	h := newHarness(t, nil)
	s := &AutoReplyService{}
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	convo := pausedConvo(AiNoAnswerPauseReason, 3*time.Hour, now)

	if err := s.resumeAIAfterPause(context.Background(), h.ts, testPayload(), convo, simChannel(), contactWithLang("en"), "hello?"); err != nil {
		t.Fatal(err)
	}
	if !convo.AIHandled || convo.AIPausedAt != nil || convo.HandoffReason != nil {
		t.Fatalf("conversation must be resumed in memory too: %+v", convo)
	}
	if len(h.ts.execs) != 1 || !strings.Contains(h.ts.execs[0].query, "ai_handled = true") ||
		!strings.Contains(h.ts.execs[0].query, "handoff_reason = NULL") ||
		!strings.Contains(h.ts.execs[0].query, "ai_handled = false") {
		t.Fatalf("resume must be one UPDATE that only matches a still-paused row: %+v", h.ts.execs)
	}
	if len(h.sent) != 1 || h.sent[0].text != salesbrain.T(salesbrain.TplHandoffResumed, salesbrain.LangEN) {
		t.Fatalf("sent = %+v", h.sent)
	}
	if h.sent[0].author != "ai" || h.sent[0].meta.Path != PathHumanHandoff || h.sent[0].meta.Reason != reasonNonQuestion {
		t.Fatalf("resume notice must be an ai message with human_handoff metadata: %+v", h.sent[0])
	}
	// The real reply to this inbound goes out right after it; sendAiMessage is idempotent per
	// inbound id, so the notice must not claim it.
	if h.sent[0].inboundID != "" {
		t.Fatalf("resume notice must not use the inbound id (it would swallow the real reply), got %q", h.sent[0].inboundID)
	}
}

// When another job already resumed the conversation (UPDATE matches no row) nobody apologises twice.
func TestResumeAIAfterPauseNoDoubleApology(t *testing.T) {
	h := newHarness(t, nil)
	h.ts.rowsAffected = 0
	s := &AutoReplyService{}
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	convo := pausedConvo(AiNoAnswerPauseReason, 3*time.Hour, now)
	if err := s.resumeAIAfterPause(context.Background(), h.ts, testPayload(), convo, simChannel(), contactWithLang("id"), "halo"); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 0 {
		t.Fatalf("no apology when the row was already resumed: %+v", h.sent)
	}
	if !convo.AIHandled {
		t.Fatal("processing continues with an active conversation")
	}
}

// ── owner notification ───────────────────────────────────────────────────────

func TestNotifyOwnerWithoutRedisSendsAndToleratesErrors(t *testing.T) {
	h := newHarness(t, []string{"owner-1"})
	s := &AutoReplyService{} // rdb == nil: send without dedupe
	s.notifyOwner(context.Background(), "t1", "c1", notifyKindHandoff, "T", "B")
	s.notifyOwner(context.Background(), "t1", "c1", notifyKindHandoff, "T", "B")
	if len(h.notes) != 2 {
		t.Fatalf("without Redis there is no dedupe, got %d notifications", len(h.notes))
	}
	if n := h.notes[0]; n.kind != "ai.handoff" || n.title != "T" || n.body != "B" || n.linkTo != "/dashboard/inbox" || len(n.ids) != 1 {
		t.Fatalf("notification = %+v", n)
	}

	listOwnerAccountIDsFn = func(context.Context, string) ([]string, error) { return nil, errors.New("system db down") }
	s.notifyOwner(context.Background(), "t1", "c1", notifyKindHandoff, "T", "B") // must not panic or return anything
	listOwnerAccountIDsFn = func(context.Context, string) ([]string, error) { return nil, nil }
	s.notifyOwner(context.Background(), "t1", "c1", notifyKindHandoff, "T", "B")
	listOwnerAccountIDsFn = func(context.Context, string) ([]string, error) { return []string{"o"}, nil }
	createOwnerNotificationFn = func(context.Context, []string, string, string, string, string) error {
		return errors.New("insert failed")
	}
	s.notifyOwner(context.Background(), "t1", "c1", notifyKindHandoff, "T", "B")
	s.notifyOwner(context.Background(), "", "c1", notifyKindHandoff, "T", "B")
	s.notifyOwner(context.Background(), "t1", "", notifyKindHandoff, "T", "B")
	if len(h.notes) != 2 {
		t.Fatalf("error paths and empty ids must not create notifications, got %d", len(h.notes))
	}
}

func TestNotifyDedupeKey(t *testing.T) {
	if got := notifyDedupeKey("ai.handoff", "t1", "c9"); got != "ai:notify:ai.handoff:t1:c9" {
		t.Fatalf("key = %q", got)
	}
}

func TestNotifyOwnerDedupesPerKindAndConversation(t *testing.T) {
	h := newHarness(t, []string{"owner-1"})
	rdb, srv := newFakeRedis(t)
	s := &AutoReplyService{rdb: rdb}
	ctx := context.Background()

	s.notifyOwner(ctx, "t1", "c1", notifyKindHandoff, "T", "B")
	s.notifyOwner(ctx, "t1", "c1", notifyKindHandoff, "T", "B")     // same kind + conversation: deduped
	s.notifyOwner(ctx, "t1", "c1", notifyKindSystemError, "T", "B") // other kind: sent
	s.notifyOwner(ctx, "t1", "c2", notifyKindHandoff, "T", "B")     // other conversation: sent
	if len(h.notes) != 3 {
		t.Fatalf("want 3 notifications (1 deduped), got %d: %+v", len(h.notes), h.notes)
	}
	if ttl := srv.ttlOf("ai:notify:ai.handoff:t1:c1"); ttl != 3600 {
		t.Fatalf("dedupe key TTL = %d seconds, want 3600", ttl)
	}

	// A failed send must not keep the slot: the next attempt within the hour has to go through.
	createOwnerNotificationFn = func(context.Context, []string, string, string, string, string) error {
		return errors.New("insert failed")
	}
	s.notifyOwner(ctx, "t1", "c3", notifyKindHandoff, "T", "B")
	createOwnerNotificationFn = func(_ context.Context, ids []string, kind, title, body, link string) error {
		h.notes = append(h.notes, notifyCall{ids: ids, kind: kind})
		return nil
	}
	s.notifyOwner(ctx, "t1", "c3", notifyKindHandoff, "T", "B")
	if len(h.notes) != 4 {
		t.Fatalf("retry after a failed send must be delivered, got %d notifications", len(h.notes))
	}
}

// ── minimal in-process Redis (RESP2) for the SETNX dedupe test ───────────────

type fakeRedisServer struct {
	mu   sync.Mutex
	data map[string]string
	ttls map[string]int
}

func (f *fakeRedisServer) ttlOf(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ttls[key]
}

func newFakeRedis(t *testing.T) (*redis.Client, *fakeRedisServer) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	srv := &fakeRedisServer{data: map[string]string{}, ttls: map[string]int{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn)
		}
	}()
	client := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), Protocol: 2, DisableIdentity: true, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close(); _ = ln.Close() })
	return client, srv
}

func (f *fakeRedisServer) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		args, err := readRESPArray(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		switch strings.ToUpper(args[0]) {
		case "SET":
			key, val := args[1], args[2]
			nx, ex := false, 0
			for i := 3; i < len(args); i++ {
				switch strings.ToUpper(args[i]) {
				case "NX":
					nx = true
				case "EX":
					i++
					ex, _ = strconv.Atoi(args[i])
				}
			}
			f.mu.Lock()
			_, exists := f.data[key]
			if nx && exists {
				f.mu.Unlock()
				_, _ = io.WriteString(conn, "$-1\r\n")
				continue
			}
			f.data[key] = val
			f.ttls[key] = ex
			f.mu.Unlock()
			_, _ = io.WriteString(conn, "+OK\r\n")
		case "DEL":
			n := 0
			f.mu.Lock()
			for _, k := range args[1:] {
				if _, ok := f.data[k]; ok {
					delete(f.data, k)
					delete(f.ttls, k)
					n++
				}
			}
			f.mu.Unlock()
			_, _ = io.WriteString(conn, fmt.Sprintf(":%d\r\n", n))
		default:
			_, _ = io.WriteString(conn, "-ERR unknown command\r\n")
		}
	}
}

func readRESPArray(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("unexpected RESP line %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimRight(hdr, "\r\n")[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		out = append(out, string(buf[:size]))
	}
	return out, nil
}
