package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"encore.app/wabantu/internal/salesbrain"
	"encore.app/wabantu/shared/llm"
)

// lfFakeChat returns queued responses in order and records every request it receives.
type lfFakeChat struct {
	replies  []lfFakeReply
	requests []llm.ChatRequest
}

type lfFakeReply struct {
	text  string
	err   error
	usage llm.Usage
}

func (f *lfFakeChat) chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	f.requests = append(f.requests, req)
	i := len(f.requests) - 1
	if i >= len(f.replies) {
		return llm.ChatResponse{}, errors.New("lfFakeChat: no queued response left")
	}
	r := f.replies[i]
	return llm.ChatResponse{Text: r.text, Usage: r.usage}, r.err
}

func lfTexts(texts ...string) *lfFakeChat {
	f := &lfFakeChat{}
	for _, t := range texts {
		f.replies = append(f.replies, lfFakeReply{text: t})
	}
	return f
}

// userOf returns the user prompt (single user message) of request i.
func (f *lfFakeChat) userOf(i int) string {
	if i >= len(f.requests) || len(f.requests[i].Messages) == 0 {
		return ""
	}
	return f.requests[i].Messages[0].Text
}

func lfOmahProfile() salesbrain.ProfileFact {
	return salesbrain.ProfileFact{
		Name: "Omah Apparel", OpeningHours: "08:00 - 19:00 GMT +7",
		DeliveryArea: "Seluruh kota di Indonesia", Tone: "friendly",
	}
}

// lfOmahCatalog is the Omah steak catalog from the 2026-10-16 incident transcripts.
func lfOmahCatalog() []salesbrain.CatalogFact {
	return []salesbrain.CatalogFact{
		{No: 1, ID: "beef-500", Name: "Australian Beef 500 Gram", Price: 450000, Unit: "pcs", Code: "AUSTRALIAN_BEEF_500_GRAM",
			Description: "Premium Australian beef steak, 500 grams per pack. Sold frozen and vacuum-sealed to lock in freshness."},
		{No: 2, ID: "tbone-1500", Name: "Australian Beef T-Bone 1.5 Kg", Price: 1750000, Unit: "pcs", Code: "AUSTRALIAN_BEEF_T_BONE_1_5_KG",
			Description: "Thick-cut Australian T-Bone, 1.5 kg. Sold frozen and vacuum-sealed to lock in freshness."},
		{No: 3, ID: "salmon-100", Name: "Salmon Steak 100 Gram", Price: 75000, Code: "SALMON_STEAK_100_GRAM",
			Description: "Salmon steak, 100 grams per piece. Sold frozen and vacuum-sealed."},
		{No: 4, ID: "salmon-250", Name: "Salmon Steak 250 Gram", Price: 105000, Code: "SALMON_STEAK_250_GRAM_SALINAN_CZED",
			Description: "Salmon steak, 250 grams per piece. Sold frozen and vacuum-sealed."},
		{No: 5, ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Price: 350000, Unit: "pcs", Code: "YELLOW_TAIL_TUNA_PORTION_100_GRAM",
			Description: "Premium cut of frozen yellow tail tuna steak, 100 grams per portion. Lean, firm, and rich in protein and omega-3. Sold frozen and vacuum-sealed to lock in freshness and premium quality. Perfect for sashimi."},
	}
}

// lfOmahKB is the 10-row Omah FAQ: bank accounts, same-day cut-off, ETA, couriers.
func lfOmahKB() []salesbrain.KBFact {
	return []salesbrain.KBFact{
		{Category: "pengiriman", Question: "Berapa lama estimasi pengiriman pesanan saya?", Answer: "Estimasi pengiriman pesanan Anda adalah 3-4 hari."},
		{Category: "pembayaran", Question: "Apakah saya harus membayar lunas pesanan di depan?", Answer: "Ya, setiap pesanan di Omah Apparel wajib melakukan pembayaran penuh (full payment) agar pesanan dapat segera kami proses."},
		{Category: "order", Question: "Berapa minimum order untuk setiap pembelian?", Answer: "Minimum order untuk setiap pembelian di Omah Apparel adalah 1 pcs."},
		{Category: "pembayaran", Question: "Apa saja metode pembayaran yang tersedia?", Answer: "Saat ini kami menerima pembayaran melalui Transfer Bank dan QRIS."},
		{Category: "pembayaran", Question: "Ke rekening mana saya harus transfer?", Answer: "Transfer ke salah satu rekening berikut:\nBCA - 000111222 - Omah Apparel\nMandiri - 333111222 - Omah Apparel\nBNI - 555111222 - Omah Apparel"},
		{Category: "pengiriman", Question: "Metode pengiriman apa yang digunakan?", Answer: "Kami melayani pengiriman ke seluruh Indonesia menggunakan jasa kurir J&T dan JNE."},
		{Category: "pengiriman", Question: "Kapan pesanan akan dikirimkan?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan langsung kami kirimkan di hari yang sama."},
		{Category: "pengiriman", Question: "Kapan pesanan saya akan dikirim?", Answer: "Pesanan dan pembayaran yang kami terima antara pukul 08:00 hingga 12:00 WIB akan dikirim di hari yang sama. Pesanan setelah pukul 12:00 WIB akan dikirim pada hari kerja berikutnya."},
		{Category: "retur", Question: "Bagaimana prosedur pengembalian barang (retur) jika produk bermasalah?", Answer: "Jika terdapat keluhan atau produk tidak sesuai, pelanggan wajib menyertakan video unboxing. Kami akan melakukan inspeksi terlebih dahulu sebelum memutuskan apakah akan diberikan refund atau tidak."},
		{Category: "order", Question: "Berapakah minimum jumlah pesanan di Omah Apparel?", Answer: "Pelanggan dapat melakukan pemesanan dengan minimum order sebanyak 1 buah produk."},
	}
}

// lfPlanBase is an English-locked Omah buyer with an empty cart.
func lfPlanBase(msg string) planInput {
	return planInput{
		Lang:       salesbrain.LanguageDecision{Lang: salesbrain.LangEN},
		LangLocked: true,
		Profile:    lfOmahProfile(),
		Catalog:    lfOmahCatalog(),
		KB:         lfOmahKB(),
		State:      salesbrain.TurnState{Greeting: "siang", Now: "Sabtu 17 Okt 2026 14:20 WIB"},
		Message:    msg,
		BuyerTexts: []string{msg},
	}
}

func lfTunaLine() salesbrain.OrderLine {
	return salesbrain.OrderLine{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}
}

func TestPlanFrozenQuestionAnsweredFromDescription(t *testing.T) {
	fake := lfTexts(`{"intent":"question","reply":"It's sold frozen and vacuum-sealed, not freshly cut. Anything else you'd like to know?"}`)
	plan := planTurn(context.Background(), lfPlanBase("Is it frozen or freshly cut?"), fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): violations=%+v raw=%q", plan.FallbackWhy, plan.Violations, plan.RawOutput)
	}
	if !strings.Contains(plan.Reply, "frozen") {
		t.Fatalf("reply must answer from the description: %q", plan.Reply)
	}
	if len(fake.requests) != 1 || plan.Attempts != 1 {
		t.Fatalf("want 1 call, got %d requests / %d attempts", len(fake.requests), plan.Attempts)
	}
	req := fake.requests[0]
	if !strings.Contains(req.System, "Sold frozen and vacuum-sealed") {
		t.Fatal("system prompt must carry the product description")
	}
	if !req.JSON || req.MaxTokens != 1024 {
		t.Fatalf("want JSON=true MaxTokens=1024, got JSON=%v MaxTokens=%d", req.JSON, req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.3 {
		t.Fatalf("want temperature 0.3, got %v", req.Temperature)
	}
	if plan.Lang != salesbrain.LangEN || plan.Decision.Intent != salesbrain.IntentQuestion || plan.CartChanged {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if !strings.Contains(fake.userOf(0), "bahasa: en (terkunci)") {
		t.Fatalf("state block must carry the locked language:\n%s", fake.userOf(0))
	}
}

func TestPlanOrderAppliesItemsAndAppendsSummary(t *testing.T) {
	fake := lfTexts(`{"intent":"order","items":[{"no":5,"qty":1}],"reply":"Sure, added 1x Yellow Tail Tuna Portion 100 Gram."}`)
	plan := planTurn(context.Background(), lfPlanBase("Okay, i would like to order 1 tuna steak please"), fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): violations=%+v", plan.FallbackWhy, plan.Violations)
	}
	if !plan.CartChanged || len(plan.Cart) != 1 || plan.Cart[0].ID != "tuna-100" || plan.Cart[0].Qty != 1 {
		t.Fatalf("cart not applied: changed=%v cart=%+v", plan.CartChanged, plan.Cart)
	}
	for _, want := range []string{"Order summary", "Rp350.000", salesbrain.T(salesbrain.TplResumeRecipient, salesbrain.LangEN)} {
		if !strings.Contains(plan.Reply, want) {
			t.Errorf("reply missing %q:\n%s", want, plan.Reply)
		}
	}
	if !strings.HasPrefix(plan.Reply, "Sure, added 1x Yellow Tail Tuna Portion 100 Gram.") {
		t.Errorf("the model reply must come first:\n%s", plan.Reply)
	}
	if !strings.HasSuffix(plan.Reply, salesbrain.T(salesbrain.TplResumeRecipient, salesbrain.LangEN)) {
		t.Errorf("the resume line must come last:\n%s", plan.Reply)
	}
}

// The invented price must lie outside every qty×price (1..50) of every catalog row, otherwise
// salesbrain.Validate rightly allows it: the plan's "Rp300.000 for tuna" is 4 × Rp75.000 (salmon).
func TestPlanRetriesOnInventedPrice(t *testing.T) {
	fake := lfTexts(
		`{"intent":"question","reply":"The tuna steak is Rp325.000 per portion. Want one?"}`,
		`{"intent":"question","reply":"The tuna steak is Rp350.000 per portion. Want one?"}`,
	)
	plan := planTurn(context.Background(), lfPlanBase("How much is the tuna steak?"), fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): violations=%+v", plan.FallbackWhy, plan.Violations)
	}
	if plan.Attempts != 2 || len(fake.requests) != 2 {
		t.Fatalf("want 2 attempts, got %d attempts / %d requests", plan.Attempts, len(fake.requests))
	}
	if !strings.Contains(fake.userOf(1), "Nominal Rp325.000") {
		t.Fatalf("retry must tell the model which amount was rejected:\n%s", fake.userOf(1))
	}
	if strings.Contains(fake.userOf(0), "CATATAN SISTEM") || strings.Contains(fake.userOf(0), "Nominal Rp325.000") {
		t.Fatal("the first attempt must carry no correction note")
	}
	if !strings.Contains(plan.Reply, "Rp350.000") || strings.Contains(plan.Reply, "Rp325.000") {
		t.Fatalf("the accepted reply must be the corrected one: %q", plan.Reply)
	}
	if len(plan.Violations) != 0 {
		t.Fatalf("violations describe the last attempt, which was clean: %+v", plan.Violations)
	}
}

func TestPlanFallsBackOnBrokenJSONTwice(t *testing.T) {
	fake := lfTexts("not json", "not json")
	plan := planTurn(context.Background(), lfPlanBase("Hello"), fake.chat)

	if !plan.Fallback || plan.FallbackWhy != "invalid_output" {
		t.Fatalf("want fallback invalid_output, got fallback=%v why=%q", plan.Fallback, plan.FallbackWhy)
	}
	if plan.Attempts != 2 || len(fake.requests) != 2 {
		t.Fatalf("want exactly 2 attempts, got %d / %d requests", plan.Attempts, len(fake.requests))
	}
	if !strings.Contains(fake.userOf(1), "bukan JSON valid") {
		t.Fatalf("retry must ask for JSON only:\n%s", fake.userOf(1))
	}
	if plan.Reply != "" {
		t.Fatalf("a fallback sends nothing, got %q", plan.Reply)
	}
	if plan.RawOutput != "not json" {
		t.Fatalf("raw output of the last attempt must be kept, got %q", plan.RawOutput)
	}
}

func TestPlanRejectsWrongLanguage(t *testing.T) {
	fake := lfTexts(
		`{"intent":"question","reply":"Siap kak, mau pesan yang mana dulu ya?"}`,
		`{"intent":"question","reply":"Sure, which product would you like to order first?"}`,
	)
	plan := planTurn(context.Background(), lfPlanBase("Hello, what do you sell?"), fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): violations=%+v", plan.FallbackWhy, plan.Violations)
	}
	if plan.Attempts != 2 {
		t.Fatalf("want 2 attempts, got %d", plan.Attempts)
	}
	if !strings.Contains(fake.userOf(1), "Bahasa balasan salah") {
		t.Fatalf("retry must report the language violation:\n%s", fake.userOf(1))
	}
	if !strings.HasPrefix(plan.Reply, "Sure,") {
		t.Fatalf("the English reply must be the one sent: %q", plan.Reply)
	}
}

func TestPlanMidCheckoutQuestionKeepsCartAndResumes(t *testing.T) {
	in := lfPlanBase("delivered today or tomorrow woi")
	in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
	in.State.PendingField = "address"
	fake := lfTexts(`{"intent":"question","reply":"Orders paid between 08:00 and 12:00 WIB are shipped the same day, and later ones go out on the next working day."}`)

	plan := planTurn(context.Background(), in, fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): violations=%+v", plan.FallbackWhy, plan.Violations)
	}
	if plan.CartChanged || len(plan.Cart) != 1 || plan.Cart[0].ID != "tuna-100" {
		t.Fatalf("a question must keep the cart: changed=%v cart=%+v", plan.CartChanged, plan.Cart)
	}
	resume := salesbrain.T(salesbrain.TplResumeAddress, salesbrain.LangEN)
	if !strings.HasSuffix(plan.Reply, resume) {
		t.Fatalf("the reply must end with the address resume line:\n%s", plan.Reply)
	}
	if strings.Contains(plan.Reply, "Order summary") {
		t.Fatalf("no summary when the cart did not change:\n%s", plan.Reply)
	}
	if !strings.Contains(fake.userOf(0), "kolom_tertunda: address") || !strings.Contains(fake.userOf(0), "kalimat_lanjut") {
		t.Fatalf("state block must expose the pending field and its resume line:\n%s", fake.userOf(0))
	}
	if !strings.Contains(fake.userOf(0), "1x Yellow Tail Tuna Portion 100 Gram @Rp350.000 | subtotal Rp350.000") {
		t.Fatalf("state block must carry the cart with its subtotal:\n%s", fake.userOf(0))
	}
}

func TestPlanLLMErrorFallsBack(t *testing.T) {
	fake := &lfFakeChat{replies: []lfFakeReply{{err: errors.New("provider down")}}}
	plan := planTurn(context.Background(), lfPlanBase("Hello"), fake.chat)

	if !plan.Fallback || plan.FallbackWhy != "llm_error" {
		t.Fatalf("want fallback llm_error, got fallback=%v why=%q", plan.Fallback, plan.FallbackWhy)
	}
	if plan.Attempts != 1 || len(fake.requests) != 1 {
		t.Fatalf("an error must not be retried: %d attempts / %d requests", plan.Attempts, len(fake.requests))
	}
	if plan.Reply != "" {
		t.Fatalf("a fallback sends nothing, got %q", plan.Reply)
	}
}

func TestPlanAcceptsBankAccountsFromKBAndRejectsInvented(t *testing.T) {
	fake := lfTexts(
		`{"intent":"question","reply":"You can transfer to BCA 000111222, Mandiri 333111222 or BNI 555111222 (all under Omah Apparel). Which one works for you?"}`,
	)
	plan := planTurn(context.Background(), lfPlanBase("Which account should I transfer to?"), fake.chat)
	if plan.Fallback || plan.Attempts != 1 {
		t.Fatalf("grounded account numbers must pass first time: fallback=%v attempts=%d violations=%+v", plan.Fallback, plan.Attempts, plan.Violations)
	}

	bad := lfTexts(
		`{"intent":"question","reply":"Please transfer to BCA 1234567890. Shall I proceed?"}`,
		`{"intent":"question","reply":"Please transfer to BCA 9876543210. Shall I proceed?"}`,
	)
	plan = planTurn(context.Background(), lfPlanBase("Which account should I transfer to?"), bad.chat)
	if !plan.Fallback || plan.FallbackWhy != "invalid_output" {
		t.Fatalf("an invented account must end in fallback: fallback=%v why=%q", plan.Fallback, plan.FallbackWhy)
	}
	if !strings.Contains(bad.userOf(1), "Nomor 1234567890 tidak ada di FAQ") {
		t.Fatalf("retry must name the invented number:\n%s", bad.userOf(1))
	}
	if len(plan.Violations) == 0 || plan.Violations[0].Code != salesbrain.VNumber {
		t.Fatalf("violations must describe the last attempt: %+v", plan.Violations)
	}
	if plan.Reply != "" {
		t.Fatalf("a rejected reply is never sent: %q", plan.Reply)
	}
}

func TestPlanSumsUsageAcrossAttempts(t *testing.T) {
	fake := &lfFakeChat{replies: []lfFakeReply{
		{text: `{"intent":"question","reply":"The tuna steak is Rp325.000 per portion. Want one?"}`,
			usage: llm.Usage{Provider: "gemini", Model: "gemini-first", InputTokens: 1000, OutputTokens: 40, ThoughtsTokens: 10}},
		{text: `{"intent":"question","reply":"The tuna steak is Rp350.000 per portion. Want one?"}`,
			usage: llm.Usage{Provider: "anthropic", Model: "claude-last", InputTokens: 1100, OutputTokens: 45, ThoughtsTokens: 5}},
	}}
	plan := planTurn(context.Background(), lfPlanBase("How much is the tuna steak?"), fake.chat)

	if plan.Fallback || plan.Attempts != 2 {
		t.Fatalf("setup: want an accepted second attempt, got fallback=%v attempts=%d", plan.Fallback, plan.Attempts)
	}
	u := plan.Usage
	if u.InputTokens != 2100 || u.OutputTokens != 85 || u.ThoughtsTokens != 15 {
		t.Fatalf("usage must be summed across attempts, got %+v", u)
	}
	if u.Model != "claude-last" || u.Provider != "anthropic" {
		t.Fatalf("model and provider come from the last attempt, got %q / %q", u.Model, u.Provider)
	}

	// Tokens spent before an error still count.
	errFake := &lfFakeChat{replies: []lfFakeReply{
		{text: "not json", usage: llm.Usage{Provider: "gemini", Model: "gemini-first", InputTokens: 900, OutputTokens: 20}},
		{err: errors.New("boom")},
	}}
	plan = planTurn(context.Background(), lfPlanBase("Hello"), errFake.chat)
	if !plan.Fallback || plan.FallbackWhy != "llm_error" || plan.Attempts != 2 {
		t.Fatalf("setup: want llm_error on attempt 2, got %+v", plan)
	}
	if plan.Usage.InputTokens != 900 || plan.Usage.OutputTokens != 20 || plan.Usage.Model != "gemini-first" {
		t.Fatalf("usage of the call made before the error must be kept, got %+v", plan.Usage)
	}
}

func TestPlanSwitchedLanguageAddsConfirmationNote(t *testing.T) {
	in := lfPlanBase("please reply in english")
	in.Lang = salesbrain.LanguageDecision{Lang: salesbrain.LangEN, Persist: salesbrain.LangEN, Switched: true}
	fake := lfTexts(`{"intent":"other","reply":"Sure, I'll reply in English from now on. What would you like to order?"}`)

	plan := planTurn(context.Background(), in, fake.chat)
	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): %+v", plan.FallbackWhy, plan.Violations)
	}
	note := "CATATAN SISTEM: pembeli baru saja memilih bahasa ini; buka balasan dengan satu kalimat konfirmasi dalam bahasa ini."
	if !strings.HasPrefix(fake.userOf(0), note) {
		t.Fatalf("the confirmation note must open the user prompt:\n%s", fake.userOf(0))
	}

	// No note when the language was not just chosen.
	fake = lfTexts(`{"intent":"question","reply":"Yes, we ship across Indonesia. Anything else?"}`)
	planTurn(context.Background(), lfPlanBase("Do you ship to Bali?"), fake.chat)
	if strings.Contains(fake.userOf(0), "pembeli baru saja memilih bahasa ini") {
		t.Fatal("the note belongs to switched turns only")
	}
}

func TestPlanIgnoresModelLanguageSwitch(t *testing.T) {
	fake := lfTexts(`{"intent":"question","language_switch":"id","reply":"Sure, the salmon steak is Rp75.000 for 100 grams. Want one?"}`)
	plan := planTurn(context.Background(), lfPlanBase("How much is the salmon?"), fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): %+v", plan.FallbackWhy, plan.Violations)
	}
	if plan.Lang != salesbrain.LangEN {
		t.Fatalf("the language is decided by code, got %q", plan.Lang)
	}
	if plan.Decision.LanguageSwitch != salesbrain.LangID {
		t.Fatalf("the model's language_switch must stay visible for logging, got %q", plan.Decision.LanguageSwitch)
	}
}

func TestPlanNoResumeLineWhenReplyAlreadyAsks(t *testing.T) {
	in := lfPlanBase("delivered today or tomorrow woi")
	in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
	in.State.PendingField = "address"
	fake := lfTexts(`{"intent":"question","reply":"Orders paid before 12:00 WIB go out the same day. What is the delivery address?"}`)

	plan := planTurn(context.Background(), in, fake.chat)
	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): %+v", plan.FallbackWhy, plan.Violations)
	}
	if strings.Contains(plan.Reply, salesbrain.T(salesbrain.TplResumeAddress, salesbrain.LangEN)) {
		t.Fatalf("one call to action per message: the reply already ends with a question:\n%s", plan.Reply)
	}
}

func TestPlanNoPendingFieldForEmptyCartAndNoOrderChange(t *testing.T) {
	// A pending field without a cart is stale state: never re-ask for it.
	in := lfPlanBase("Hello")
	in.State.PendingField = "recipient"
	fake := lfTexts(`{"intent":"greeting","reply":"Hello! How can I help you today?"}`)
	plan := planTurn(context.Background(), in, fake.chat)
	if plan.Fallback || plan.Reply != "Hello! How can I help you today?" {
		t.Fatalf("an empty cart has no pending field, got fallback=%v reply=%q", plan.Fallback, plan.Reply)
	}

	// Removing the last line empties the cart: no summary, no resume line.
	in = lfPlanBase("cancel the tuna please")
	in.State.Cart = []salesbrain.OrderLine{lfTunaLine()}
	fake = lfTexts(`{"intent":"amend","items":[{"no":5,"qty":0}],"reply":"Done, I removed the tuna steak from your order."}`)
	plan = planTurn(context.Background(), in, fake.chat)
	if plan.Fallback || !plan.CartChanged || len(plan.Cart) != 0 {
		t.Fatalf("setup: the cart must be emptied, got fallback=%v changed=%v cart=%+v", plan.Fallback, plan.CartChanged, plan.Cart)
	}
	if plan.Reply != "Done, I removed the tuna steak from your order." {
		t.Fatalf("nothing is appended to an emptied cart, got %q", plan.Reply)
	}
}

func TestPlanUnknownItemsReportedNotApplied(t *testing.T) {
	fake := lfTexts(`{"intent":"order","items":[{"no":5,"qty":1},{"no":9,"qty":2}],"reply":"Added the tuna steak for you."}`)
	plan := planTurn(context.Background(), lfPlanBase("1 tuna and 2 of something else"), fake.chat)

	if plan.Fallback || !plan.CartChanged || len(plan.Cart) != 1 {
		t.Fatalf("known rows must still be applied: fallback=%v changed=%v cart=%+v", plan.Fallback, plan.CartChanged, plan.Cart)
	}
	if len(plan.UnknownItems) != 1 || plan.UnknownItems[0] != 9 {
		t.Fatalf("unknown row numbers must be reported, got %v", plan.UnknownItems)
	}
}

func TestPlanDropsSummaryRatherThanAnswerWhenTooLong(t *testing.T) {
	in := lfPlanBase("add one salmon steak 100 gram")
	for i := 0; i < 40; i++ {
		in.State.Cart = append(in.State.Cart, salesbrain.OrderLine{
			ID: fmt.Sprintf("filler-%d", i), Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000,
		})
	}
	fake := lfTexts(`{"intent":"order","items":[{"no":3,"qty":1}],"reply":"Added 1x Salmon Steak 100 Gram."}`)

	plan := planTurn(context.Background(), in, fake.chat)
	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): %+v", plan.FallbackWhy, plan.Violations)
	}
	if !plan.CartChanged || len(plan.Cart) != 41 {
		t.Fatalf("the cart still changes, got changed=%v len=%d", plan.CartChanged, len(plan.Cart))
	}
	if strings.Contains(plan.Reply, "Order summary") {
		t.Fatalf("an over-long summary must be dropped:\n%s", plan.Reply)
	}
	if n := utf8.RuneCountInString(plan.Reply); n > 1400 {
		t.Fatalf("reply must stay within 1400 runes, got %d", n)
	}
	if !strings.HasPrefix(plan.Reply, "Added 1x Salmon Steak 100 Gram.") ||
		!strings.HasSuffix(plan.Reply, salesbrain.T(salesbrain.TplResumeRecipient, salesbrain.LangEN)) {
		t.Fatalf("the answer and the resume line must survive:\n%s", plan.Reply)
	}
}

func TestPlanRawOutputIsRedactedAndCapped(t *testing.T) {
	fake := lfTexts(`{"intent":"question","reply":"Thanks Ana, we will call 0872839472828 or mail ana@example.com if needed. Anything else?"}`)
	plan := planTurn(context.Background(), lfPlanBase("Hi, I am Ana, 0872839472828"), fake.chat)

	if plan.Fallback {
		t.Fatalf("unexpected fallback (%s): %+v", plan.FallbackWhy, plan.Violations)
	}
	for _, leak := range []string{"0872839472828", "ana@example.com"} {
		if strings.Contains(plan.RawOutput, leak) {
			t.Fatalf("raw output leaks %q: %q", leak, plan.RawOutput)
		}
	}

	long := lfTexts(strings.Repeat("x", 3000), strings.Repeat("x", 3000))
	plan = planTurn(context.Background(), lfPlanBase("Hello"), long.chat)
	if n := utf8.RuneCountInString(plan.RawOutput); n > 1001 {
		t.Fatalf("raw output must be capped at 1000 runes (+ ellipsis), got %d", n)
	}
}
