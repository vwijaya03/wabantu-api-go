package ai

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/order"
	"encore.app/wabantu/shared/whatsappchannel"
)

// Whole-turn tests of the LLM-first shell. The loads are stubbed (llmFirstLoad), the model is a
// fake chat, Redis is an in-process spy, the tenant database is a recorder and the WhatsApp send is
// the simulator seam, so they run without any infrastructure and see every write in order.

const (
	lfFlowTenant = "t1"
	lfFlowSchema = "t_flowtest"
	lfFlowConvo  = "c1"
)

// lfRedisSpy is a minimal in-process Redis (RESP2) that records every command it receives.
type lfRedisSpy struct {
	mu   sync.Mutex
	data map[string]string
	ttl  map[string]int
	log  []string
}

func newLFRedis(t *testing.T) (*redis.Client, *lfRedisSpy) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	spy := &lfRedisSpy{data: map[string]string{}, ttl: map[string]int{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go spy.serve(conn)
		}
	}()
	client := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), Protocol: 2, DisableIdentity: true, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close(); _ = ln.Close() })
	return client, spy
}

func (r *lfRedisSpy) serve(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	for {
		args, err := readRESPArray(br)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		cmd := strings.ToUpper(args[0])
		r.mu.Lock()
		r.log = append(r.log, cmd+" "+strings.Join(args[1:min(2, len(args))], " "))
		switch cmd {
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
			if _, exists := r.data[key]; nx && exists {
				r.mu.Unlock()
				_, _ = io.WriteString(conn, "$-1\r\n")
				continue
			}
			r.data[key], r.ttl[key] = val, ex
			r.mu.Unlock()
			_, _ = io.WriteString(conn, "+OK\r\n")
		case "GET":
			val, ok := r.data[args[1]]
			r.mu.Unlock()
			if !ok {
				_, _ = io.WriteString(conn, "$-1\r\n")
				continue
			}
			_, _ = io.WriteString(conn, fmt.Sprintf("$%d\r\n%s\r\n", len(val), val))
		case "DEL":
			n := 0
			for _, k := range args[1:] {
				if _, ok := r.data[k]; ok {
					delete(r.data, k)
					delete(r.ttl, k)
					n++
				}
			}
			r.mu.Unlock()
			_, _ = io.WriteString(conn, fmt.Sprintf(":%d\r\n", n))
		default:
			r.mu.Unlock()
			_, _ = io.WriteString(conn, "-ERR unknown command\r\n")
		}
	}
}

func (r *lfRedisSpy) commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

func (r *lfRedisSpy) value(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data[key]
}

func (r *lfRedisSpy) ttlOf(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ttl[key]
}

type lfSent struct {
	text, author string
	meta         AiReplyMeta
}

// lfFlow wires one turn: every effect lands in events (in order), sent, notes or the Redis spy.
type lfFlow struct {
	t      *testing.T
	s      *AutoReplyService
	redis  *lfRedisSpy
	events []string
	sent   []lfSent
	notes  []lfNote
	chat   *lfFakeChat
	turn   lfTurn
}

// lfFlowTS records the writes of a turn: the language lock and the pause are the ones the tests watch.
type lfFlowTS struct {
	fakeTS
	f *lfFlow
}

func (ts lfFlowTS) ExecContext(_ context.Context, q string, _ ...any) (sql.Result, error) {
	kind := "other"
	switch {
	case strings.Contains(q, "preferred_language"):
		kind = "language"
	case strings.Contains(q, "ai_paused_at"):
		kind = "pause"
	}
	ts.f.events = append(ts.f.events, "exec:"+kind)
	return fakeResult{}, nil
}

var lfFlowNow = time.Date(2026, 10, 17, 7, 20, 0, 0, time.UTC) // 14:20 WIB

func newLFFlow(t *testing.T, userText string, contact *dbContact, ld lfLoaded, chat *lfFakeChat) *lfFlow {
	t.Helper()
	rdb, spy := newLFRedis(t)
	f := &lfFlow{t: t, s: &AutoReplyService{rdb: rdb}, redis: spy, chat: chat}

	hours, desc := "08:00 - 19:00 GMT +7", "Frozen seafood and steak"
	f.turn = lfTurn{
		ts:       lfFlowTS{f: f},
		payload:  AiReplyJobPayload{TenantID: lfFlowTenant, TenantSchema: lfFlowSchema, ConversationID: lfFlowConvo, InboundMessageID: "in-1"},
		convo:    &dbConversation{ID: lfFlowConvo, ContactID: "k1", AIHandled: true},
		channel:  &dbChannel{Provider: whatsappchannel.SimulatorProvider, Status: "connected"},
		contact:  contact,
		profile:  &dbBusinessProfile{BusinessName: "Omah Apparel", OpeningHours: &hours, Description: &desc, AIEnabled: true},
		userText: userText,
	}

	oldChat, oldNow, oldLoad, oldSim := llmFirstChatFor, llmFirstNow, llmFirstLoad, simulatorOutbound
	oldOwners, oldNotify := listOwnerAccountIDsFn, createOwnerNotificationFn
	t.Cleanup(func() {
		llmFirstChatFor, llmFirstNow, llmFirstLoad, simulatorOutbound = oldChat, oldNow, oldLoad, oldSim
		listOwnerAccountIDsFn, createOwnerNotificationFn = oldOwners, oldNotify
	})
	llmFirstChatFor = func(string) chatFunc { return chat.chat }
	llmFirstNow = func() time.Time { return lfFlowNow }
	llmFirstLoad = func(*AutoReplyService, context.Context, lfTurn) (lfLoaded, error) { return ld, nil }
	simulatorOutbound = func(_ *AutoReplyService, _ context.Context, _ tenantScopedQuerier, _ string, _ *dbConversation, text, author, _ string, meta AiReplyMeta) error {
		f.events = append(f.events, "send")
		f.sent = append(f.sent, lfSent{text: text, author: author, meta: meta})
		return nil
	}
	listOwnerAccountIDsFn = func(context.Context, string) ([]string, error) { return []string{"owner-1"}, nil }
	createOwnerNotificationFn = func(_ context.Context, _ []string, kind, title, body, _ string) error {
		f.events = append(f.events, "notify")
		f.notes = append(f.notes, lfNote{kind, title, body})
		return nil
	}
	// The saved-language stack is process-global: give the turn its own and restore it.
	t.Cleanup(bf.PushSavedBuyerLang(contactPreferredLanguage(contact)))
	return f
}

func (f *lfFlow) run() (bool, error) { return f.s.runLLMFirst(context.Background(), f.turn) }

func lfFlowCatalog() []dbCatalogItem {
	catalog := lfOrdersCatalog()
	for i := range catalog {
		catalog[i].Description = "Sold frozen and vacuum-sealed to lock in freshness."
	}
	return catalog
}

// lfFlowLoaded is a buyer who has chatted before: history ends with the current message.
func lfFlowLoaded(userText string, extra ...dbMessage) lfLoaded {
	history := append([]dbMessage{
		lfMsg("contact", "halo", lfFlowNow.Add(-3*time.Hour)),
		lfMsg("ai", "Halo! Mau cari apa, Kak?", lfFlowNow.Add(-3*time.Hour+time.Minute)),
	}, extra...)
	history = append(history, lfMsg("contact", userText, lfFlowNow))
	return lfLoaded{
		history:  history,
		lastBody: "Halo! Mau cari apa, Kak?",
		catalog:  lfFlowCatalog(),
		kbAll:    []dbKBEntry{{Question: "Berapa lama estimasi pengiriman?", Answer: "Estimasi pengiriman 3-4 hari."}},
	}
}

// ── turns ────────────────────────────────────────────────────────────────────

func TestLLMFirstFlowQuestionIsAnsweredAndSent(t *testing.T) {
	text := "Is it frozen or freshly cut?"
	chat := lfTexts(`{"intent":"question","reply":"It's sold frozen and vacuum-sealed, not freshly cut. Anything else you'd like to know?"}`)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded(text), chat)

	handled, err := f.run()
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(f.sent) != 1 || f.sent[0].author != "ai" || !strings.Contains(f.sent[0].text, "sold frozen") {
		t.Fatalf("sent = %+v", f.sent)
	}
	m := f.sent[0].meta
	if m.Pipeline != "llm_first" || m.Intent != "question" || m.Path != PathLLMFirst || m.Lang != "en" || m.Attempts != 1 ||
		!m.LLMUsed || m.Reason != reasonAIGenerated || m.InboundReplyTo != "in-1" || m.RawOutput == "" {
		t.Fatalf("meta = %+v", m)
	}
	if len(chat.requests) != 1 || !chat.requests[0].JSON || chat.requests[0].MaxTokens != 1024 {
		t.Fatalf("one JSON call with the 1024-token cap expected: %+v", chat.requests)
	}
	if !strings.Contains(chat.requests[0].System, "Sold frozen and vacuum-sealed") {
		t.Error("the product description must reach the model: that was the incident")
	}
	if got := strings.Join(f.events, ","); got != "send" {
		t.Errorf("a plain answer writes nothing but the reply, events = %s", got)
	}
	if cmds := f.redis.commands(); len(cmds) != 0 {
		t.Errorf("no Redis writes for a plain answer: %v", cmds)
	}
}

func TestLLMFirstFlowAmbiguousGreetingAsksTheLanguageWithoutTheModel(t *testing.T) {
	f := newLFFlow(t, "Hi", &dbContact{ID: "k1"}, lfFlowLoaded("Hi"), lfTexts())

	handled, err := f.run()
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(f.sent) != 1 || f.sent[0].text != salesbrain.T(salesbrain.TplLanguageChoice, salesbrain.LangID, "Omah Apparel") || f.sent[0].meta.Path != PathBuyerLanguage {
		t.Fatalf("sent = %+v", f.sent)
	}
	if len(f.chat.requests) != 0 {
		t.Error("the language question costs no tokens")
	}
	if strings.Contains(strings.Join(f.events, ","), "language") {
		t.Error("nothing is locked by an ambiguous greeting")
	}
}

func TestLLMFirstFlowExplicitSwitchIsLockedBeforeTheReply(t *testing.T) {
	text := "Please reply in English, can I get 2 salmon steak 100 gram?"
	chat := lfTexts(`{"intent":"order","items":[{"no":3,"qty":2}],"reply":"Sure, I'll reply in English. Added 2x Salmon Steak 100 Gram."}`)
	contact := &dbContact{ID: "k1", PreferredLanguage: strp("id")}
	f := newLFFlow(t, text, contact, lfFlowLoaded(text), chat)

	handled, err := f.run()
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got := strings.Join(f.events, ","); got != "exec:language,send" {
		t.Fatalf("the language is locked before the reply goes out, events = %s", got)
	}
	if contactPreferredLanguage(contact) != "en" || bf.CurrentSavedBuyerLang() != "en" {
		t.Errorf("legacy state must follow: contact=%q saved=%q", contactPreferredLanguage(contact), bf.CurrentSavedBuyerLang())
	}
	if !strings.HasPrefix(chat.userOf(0), lfLanguageSwitchNote) {
		t.Errorf("the model is told to confirm the switch:\n%s", chat.userOf(0))
	}
	if f.sent[0].meta.Lang != "en" || !strings.Contains(f.sent[0].text, "Order summary") {
		t.Errorf("sent = %+v", f.sent[0])
	}
}

func TestLLMFirstFlowFallbackHandsTheTurnBackAndKeepsLegacyConsistent(t *testing.T) {
	// The model fails twice: nothing is sent, the legacy flow takes the turn.
	text := "mau tanya harga salmon"
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("id")}, lfFlowLoaded(text), lfTexts("not json", "still not json"))
	handled, err := f.run()
	if handled || err != nil {
		t.Fatalf("a fallback is (false, nil): handled=%v err=%v", handled, err)
	}
	if len(f.sent) != 0 || len(f.events) != 0 || len(f.chat.requests) != 2 {
		t.Fatalf("sent=%v events=%v calls=%d", f.sent, f.events, len(f.chat.requests))
	}

	// A first clear Indonesian message locks the language before the model is asked. When the
	// turn then fails open, the legacy templates must already speak Indonesian.
	text = "selamat pagi kak, saya mau pesan salmon steak dua pcs ya"
	if l, ok := salesbrain.ClearLanguage(text); !ok || l != salesbrain.LangID {
		t.Fatalf("test setup: %q must read as clear Indonesian", text)
	}
	contact := &dbContact{ID: "k1"}
	f = newLFFlow(t, text, contact, lfFlowLoaded(text), lfTexts("not json", "not json"))
	if handled, err := f.run(); handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got := strings.Join(f.events, ","); got != "exec:language" {
		t.Fatalf("events = %s", got)
	}
	if contactPreferredLanguage(contact) != "id" || bf.CurrentSavedBuyerLang() != "id" {
		t.Errorf("contact=%q saved=%q", contactPreferredLanguage(contact), bf.CurrentSavedBuyerLang())
	}
}

func TestLLMFirstFlowOrderTurnSavesTheCart(t *testing.T) {
	text := "Okay, i would like to order 1 tuna steak please"
	chat := lfTexts(`{"intent":"order","items":[{"no":5,"qty":1}],"reply":"Sure, added 1x Yellow Tail Tuna Portion 100 Gram."}`)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded(text), chat)

	handled, err := f.run()
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var st orderState
	if err := json.Unmarshal([]byte(f.redis.value("ai:order:t1:c1")), &st); err != nil {
		t.Fatalf("the checkout state must be in Redis: %v", err)
	}
	if st.Step != lfStepAskRecipient || len(st.Items) != 1 || st.Items[0].CatalogItemID != "tuna-100" || st.Items[0].Qty != 1 {
		t.Fatalf("state = %+v", st)
	}
	if got := f.redis.ttlOf("ai:order:t1:c1"); got != int(orderStateTTL/time.Second) {
		t.Errorf("state TTL = %d", got)
	}
	reply := f.sent[0].text
	for _, want := range []string{"Sure, added 1x", "Order summary", "Rp350.000", salesbrain.ResumeLineFor("recipient", salesbrain.LangEN)} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply missing %q:\n%s", want, reply)
		}
	}
	if f.sent[0].meta.Intent != "order" {
		t.Errorf("meta = %+v", f.sent[0].meta)
	}
}

func TestLLMFirstFlowTakeoverSendsThenPausesThenNotifies(t *testing.T) {
	text := "I want to talk to the owner"
	chat := lfTexts(`{"intent":"human_request","handoff":{"level":"takeover","reason":"buyer asked for the owner"},"reply":"Sure, I'm passing this chat to the team now."}`)
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded(text), chat)

	handled, err := f.run()
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got := strings.Join(f.events, ","); got != "send,exec:pause,notify" {
		t.Fatalf("the buyer hears first, then the AI pauses, then the owner is told: %s", got)
	}
	if !strings.Contains(f.sent[0].text, "They usually reply during opening hours (08:00 - 19:00 GMT +7).") {
		t.Errorf("sent = %q", f.sent[0].text)
	}
	if len(f.notes) != 1 || f.notes[0].kind != notifyKindHandoff || f.notes[0].title != notifyTitleHumanRequest || !strings.Contains(f.notes[0].body, "buyer asked for the owner") {
		t.Errorf("notes = %+v", f.notes)
	}
}

func TestLLMFirstFlowReminderAndConfusionBookkeeping(t *testing.T) {
	open := lfTestOrder(t, "f4bfce06-0000-0000-0000-000000000001", "draft", "unpaid", lfFlowNow.Add(-4*time.Hour), []order.OrderItem{lfTunaItem}, order.ShippingAddress{})

	// The reply names the open order while the policy allows a reminder: it is recorded.
	text := "hello"
	chat := lfTexts(`{"intent":"greeting","reply":"Hi! You still have order WB-F4BFCE06 (1x Yellow Tail Tuna Portion 100 Gram, total Rp350.000). Continue, start a new order, or cancel it?"}`)
	ld := lfFlowLoaded(text)
	ld.orders = []persistedOrder{open}
	f := newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, ld, chat)
	if handled, err := f.run(); !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	key := "ai:openorder:remind:t1:c1"
	rs := lfDecodeRemind([]byte(f.redis.value(key)))
	if rs.Count["WB-F4BFCE06"] != 1 || !rs.LastAt.Equal(lfFlowNow) {
		t.Fatalf("reminder state = %+v (raw %q)", rs, f.redis.value(key))
	}
	if got := f.redis.ttlOf(key); got != int(lfRemindTTL/time.Second) {
		t.Errorf("reminder TTL = %d, want 72h", got)
	}

	// A reply that does not mention the order records nothing.
	chat = lfTexts(`{"intent":"greeting","reply":"Hi! What can I get for you today?"}`)
	f = newLFFlow(t, text, &dbContact{ID: "k1", PreferredLanguage: strp("en")}, ld, chat)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if f.redis.value(key) != "" {
		t.Error("no reminder was made, none may be recorded")
	}

	// Intent other counts a misunderstanding for 24h; the next understood turn resets it.
	chat = lfTexts(`{"intent":"other","reply":"Sorry, I did not quite get that. Could you rephrase?"}`)
	f = newLFFlow(t, "asdf qwer", &dbContact{ID: "k1", PreferredLanguage: strp("en")}, lfFlowLoaded("asdf qwer"), chat)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	ckey := "ai:llmfirst:confused:t1:c1"
	if f.redis.value(ckey) != "1" || f.redis.ttlOf(ckey) != int(lfConfusedTTL/time.Second) {
		t.Errorf("confusion counter = %q ttl=%d", f.redis.value(ckey), f.redis.ttlOf(ckey))
	}

	ld = lfFlowLoaded("what do you sell?")
	ld.confused = 1
	chat = lfTexts(`{"intent":"browse","reply":"We sell frozen steak, salmon and tuna. Anything you'd like to see?"}`)
	f = newLFFlow(t, "what do you sell?", &dbContact{ID: "k1", PreferredLanguage: strp("en")}, ld, chat)
	f.redis.mu.Lock()
	f.redis.data[ckey] = "1"
	f.redis.mu.Unlock()
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if f.redis.value(ckey) != "" {
		t.Error("an understood turn resets the counter")
	}
}

// ── shadow ───────────────────────────────────────────────────────────────────

// Run for real: the model is asked, a plan is made, one line is logged, and nothing else happens.
// The same message in live mode would lock a language, save a cart and send a reply.
func TestLLMFirstFlowShadowRunNeverWrites(t *testing.T) {
	text := "selamat pagi kak, saya mau pesan salmon steak dua pcs ya"
	if l, ok := salesbrain.ClearLanguage(text); !ok || l != salesbrain.LangID {
		t.Fatalf("test setup: %q must read as clear Indonesian", text)
	}
	chat := lfTexts(`{"intent":"order","items":[{"no":3,"qty":2}],"handoff":{"level":"takeover","reason":"test"},"reply":"Siap Kak, 2x Salmon Steak 100 Gram sudah saya catat."}`)
	contact := &dbContact{ID: "k1"}
	f := newLFFlow(t, text, contact, lfFlowLoaded(text), chat)
	before := bf.CurrentSavedBuyerLang()

	f.s.shadowLLMFirst(context.Background(), f.turn)

	if len(f.chat.requests) != 1 {
		t.Fatalf("the model is asked once, calls = %d", len(f.chat.requests))
	}
	if len(f.sent) != 0 {
		t.Errorf("shadow must never send: %+v", f.sent)
	}
	if len(f.events) != 0 {
		t.Errorf("no language write, pause or notification: %v", f.events)
	}
	if cmds := f.redis.commands(); len(cmds) != 0 {
		t.Errorf("no Redis traffic at all (loads are stubbed, so any command is a write): %v", cmds)
	}
	if contact.PreferredLanguage != nil || bf.CurrentSavedBuyerLang() != before {
		t.Errorf("the language must stay unlocked: contact=%q stack=%q", contactPreferredLanguage(contact), bf.CurrentSavedBuyerLang())
	}
}

func TestLLMFirstFlowShadowSkipsTheModelForAmbiguousGreetings(t *testing.T) {
	f := newLFFlow(t, "Hi", &dbContact{ID: "k1"}, lfFlowLoaded("Hi"), lfTexts())
	f.s.shadowLLMFirst(context.Background(), f.turn)
	if len(f.chat.requests) != 0 || len(f.sent) != 0 || len(f.events) != 0 {
		t.Errorf("calls=%d sent=%v events=%v", len(f.chat.requests), f.sent, f.events)
	}
}
