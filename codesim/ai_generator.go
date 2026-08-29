package codesim

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"encore.dev"
	"encore.dev/rlog"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/codesim/validate"
)

const codesimAIHTTPTimeout = 3 * time.Minute
const codesimAIGenTimeout = 5 * time.Minute
const codesimAIJSONRetries = 3
const codesimAIMaxOutputTokens = 8192

func codesimAIModel() string {
	return ai.DefaultHaikuAPIID()
}

// aiGenContext detaches from the HTTP request deadline (e.g. Next.js 30s proxy) but keeps a hard cap.
func aiGenContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), codesimAIGenTimeout)
}

var codesimSecrets struct {
	AnthropicApiKey string
}

// AIExamPlan is a human-readable outline shown before generating questions.
type AIExamPlan struct {
	Summary             string   `json:"summary"`
	McqCount            int      `json:"mcqCount"`
	McqFocus            string   `json:"mcqFocus"`
	BuildFocus          string   `json:"buildFocus"`
	DebugFocus          string   `json:"debugFocus"`
	SuggestedDifficulty string   `json:"suggestedDifficulty"`
	Tags                []string `json:"tags"`
	Warnings            []string `json:"warnings,omitempty"`
}

type aiPlanRow struct {
	ID        string
	Brief     string
	Plan      AIExamPlan
	McqCount  int
	ExpiresAt time.Time
}

func LiveAIGenEnabled() bool {
	if strings.TrimSpace(os.Getenv("CODESIM_LIVE_AI_GEN")) != "1" {
		return false
	}
	return encore.Meta().Environment.Cloud == encore.CloudLocal
}

func anthropicClient() (*ai.AnthropicClient, error) {
	key := anthropicAPIKey()
	if key == "" {
		return nil, fmt.Errorf(
			"kunci Anthropic belum diset — jalankan: cd api-go && ./scripts/setup-secrets-from-env.sh lalu restart encore run (atau set ANTHROPIC_API_KEY di .env.local)",
		)
	}
	return ai.NewAnthropicClient(key, ai.AnthropicConfig{
		Model:       codesimAIModel(),
		MaxTokens:   8192,
		HTTPTimeout: codesimAIHTTPTimeout,
	}), nil
}

func anthropicAPIKey() string {
	if k := strings.TrimSpace(codesimSecrets.AnthropicApiKey); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
}

func createAIPlan(ctx context.Context, accountID, brief string, mcqCount int) (*aiPlanRow, error) {
	if !LiveAIGenEnabled() {
		return nil, fmt.Errorf("AI generate hanya tersedia di local dev (CODESIM_LIVE_AI_GEN=1)")
	}
	brief = strings.TrimSpace(brief)
	if len(brief) < 10 {
		return nil, fmt.Errorf("deskripsi topik minimal 10 karakter")
	}
	if mcqCount <= 0 {
		mcqCount = defaultMcqCount()
	}
	if mcqCount < 3 || mcqCount > 7 {
		return nil, fmt.Errorf("jumlah MCQ harus antara 3 dan 7")
	}

	plan, err := requestAIPlan(ctx, brief, mcqCount)
	if err != nil {
		return nil, err
	}
	plan.McqCount = mcqCount

	planJSON, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(30 * time.Minute)
	var planID string
	err = db.QueryRow(ctx, `
		INSERT INTO codesim_ai_plan (id, account_id, brief, plan_json, expires_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		RETURNING id::text`,
		nullUUID(accountID), brief, planJSON, expires,
	).Scan(&planID)
	if err != nil {
		return nil, err
	}
	return &aiPlanRow{
		ID:        planID,
		Brief:     brief,
		Plan:      plan,
		McqCount:  mcqCount,
		ExpiresAt: expires,
	}, nil
}

func loadAIPlan(ctx context.Context, planID, accountID string) (*aiPlanRow, error) {
	var brief string
	var planJSON []byte
	var expires time.Time
	var err error
	if accountID != "" {
		err = db.QueryRow(ctx, `
			SELECT brief, plan_json, expires_at
			FROM codesim_ai_plan
			WHERE id = $1 AND account_id = $2`,
			planID, accountID,
		).Scan(&brief, &planJSON, &expires)
	} else {
		err = db.QueryRow(ctx, `
			SELECT brief, plan_json, expires_at
			FROM codesim_ai_plan
			WHERE id = $1 AND account_id IS NULL`,
			planID,
		).Scan(&brief, &planJSON, &expires)
	}
	if err != nil {
		return nil, fmt.Errorf("rencana AI tidak ditemukan atau kedaluwarsa")
	}
	if time.Now().After(expires) {
		return nil, fmt.Errorf("rencana AI sudah kedaluwarsa — buat rencana baru")
	}
	var plan AIExamPlan
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return nil, err
	}
	return &aiPlanRow{
		ID:        planID,
		Brief:     brief,
		Plan:      plan,
		McqCount:  plan.McqCount,
		ExpiresAt: expires,
	}, nil
}

func generateExamFromAIPlan(ctx context.Context, row *aiPlanRow) ([]ExamQuestion, BlueprintConfig, error) {
	genCtx, cancel := aiGenContext(ctx)
	defer cancel()

	payload, err := requestAIExamPayload(genCtx, row.Brief, row.Plan)
	if err != nil {
		return nil, BlueprintConfig{}, err
	}
	if len(payload.MCQs) != row.McqCount {
		return nil, BlueprintConfig{}, fmt.Errorf("AI mengembalikan %d MCQ, butuh %d", len(payload.MCQs), row.McqCount)
	}
	for i := range payload.MCQs {
		if err := validate.NormalizeAndValidateMCQ(&payload.MCQs[i]); err != nil {
			return nil, BlueprintConfig{}, fmt.Errorf("MCQ %d invalid: %w", i+1, err)
		}
	}
	if err := validate.ValidateBuild(&payload.Build); err != nil {
		return nil, BlueprintConfig{}, fmt.Errorf("build task invalid: %w", err)
	}
	if err := validate.ValidateDebug(&payload.Debug); err != nil {
		return nil, BlueprintConfig{}, fmt.Errorf("debug task invalid: %w", err)
	}

	cfg := BuildLearnerConfig(row.Plan.Tags, row.Plan.SuggestedDifficulty, row.McqCount)
	questions := aiPayloadToExamQuestions(payload)
	return questions, cfg, nil
}

func requestAIPlan(ctx context.Context, brief string, mcqCount int) (AIExamPlan, error) {
	system := `Kamu merencanakan simulasi tes coding frontend. Output HANYA JSON object (tanpa markdown fence) dengan field:
summary (string, 2-3 kalimat Bahasa Indonesia),
mcqFocus, buildFocus, debugFocus (string),
suggestedDifficulty ("easy"|"medium"|"hard"),
tags (array string, subset: react, hooks, javascript, css, html),
warnings (array string opsional jika brief terlalu luang/sempit).`
	user := fmt.Sprintf("Brief peserta: %q\nJumlah MCQ: %d\nSusun rencana ujian 7 soal (%d MCQ + 1 build + 1 debug).", brief, mcqCount, mcqCount)

	client, err := anthropicClient()
	if err != nil {
		return AIExamPlan{}, err
	}
	genCtx, cancel := aiGenContext(ctx)
	defer cancel()
	var plan AIExamPlan
	if err := completeJSONObject(genCtx, client, system, user, 1024, &plan); err != nil {
		return AIExamPlan{}, fmt.Errorf("parse rencana AI: %w", err)
	}
	if strings.TrimSpace(plan.Summary) == "" {
		return AIExamPlan{}, fmt.Errorf("rencana AI tidak valid: summary kosong")
	}
	return plan, nil
}

type aiExamPayload struct {
	MCQs  []validate.MCQInput `json:"mcqs"`
	Build validate.BuildInput `json:"build"`
	Debug validate.DebugInput `json:"debug"`
}

type mcqGenBatch struct {
	From  int
	Count int
}

func splitMCQGen(total int) []mcqGenBatch {
	if total <= 3 {
		return []mcqGenBatch{{From: 1, Count: total}}
	}
	first := (total + 1) / 2
	return []mcqGenBatch{
		{From: 1, Count: first},
		{From: first + 1, Count: total - first},
	}
}

func completeJSONObject(ctx context.Context, client *ai.AnthropicClient, system, user string, maxTokens int64, dest any) error {
	if maxTokens <= 0 {
		maxTokens = codesimAIMaxOutputTokens
	}
	compactUser := user
	var lastErr error
	for attempt := 1; attempt <= codesimAIJSONRetries; attempt++ {
		raw, usage, err := client.CompleteText(ctx, codesimAIModel(), system, compactUser, maxTokens)
		if err != nil {
			lastErr = err
			continue
		}
		jsonStr := extractJSONObject(raw)
		hitTokenLimit := int64(usage.OutputTokens) >= maxTokens-48
		if !json.Valid([]byte(jsonStr)) || hitTokenLimit {
			if hitTokenLimit {
				rlog.Warn("codesim ai hit token limit", "attempt", attempt, "maxTok", maxTokens, "outTok", usage.OutputTokens, "len", len(jsonStr))
			} else {
				rlog.Warn("codesim ai invalid json", "attempt", attempt, "len", len(jsonStr), "outTok", usage.OutputTokens)
			}
			lastErr = fmt.Errorf("JSON tidak lengkap dari AI")
			if attempt < codesimAIJSONRetries {
				if maxTokens < codesimAIMaxOutputTokens {
					maxTokens = codesimAIMaxOutputTokens
				}
				compactUser = user + "\n\nPENTING: JSON harus lengkap dan valid. Kode starter maks 12 baris, solution maks 20 baris. Semua string singkat."
			}
			continue
		}
		if err := json.Unmarshal([]byte(jsonStr), dest); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("gagal parse respons AI")
	}
	return lastErr
}

func requestAIExamPayload(ctx context.Context, brief string, plan AIExamPlan) (aiExamPayload, error) {
	client, err := anthropicClient()
	if err != nil {
		return aiExamPayload{}, err
	}

	batches := splitMCQGen(plan.McqCount)
	mcqSlots := make([][]validate.MCQInput, len(batches))
	var build validate.BuildInput
	var debug validate.DebugInput

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	wg.Add(len(batches) + 2)
	for i, batch := range batches {
		i, batch := i, batch
		go func() {
			defer wg.Done()
			mcqs, err := requestAIMCQBatch(ctx, client, brief, plan, batch)
			if err != nil {
				recordErr(err)
				return
			}
			mu.Lock()
			mcqSlots[i] = mcqs
			mu.Unlock()
		}()
	}
	go func() {
		defer wg.Done()
		b, err := requestAIBuild(ctx, client, brief, plan)
		if err != nil {
			recordErr(err)
			return
		}
		mu.Lock()
		build = b
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		d, err := requestAIDebug(ctx, client, brief, plan)
		if err != nil {
			recordErr(err)
			return
		}
		mu.Lock()
		debug = d
		mu.Unlock()
	}()
	wg.Wait()
	if firstErr != nil {
		return aiExamPayload{}, firstErr
	}

	var mcqs []validate.MCQInput
	for _, slot := range mcqSlots {
		mcqs = append(mcqs, slot...)
	}

	payload := aiExamPayload{MCQs: mcqs, Build: build, Debug: debug}
	rlog.Info("codesim ai exam generated", "mcqs", len(payload.MCQs), "model", codesimAIModel())
	return payload, nil
}

const mcqAISystemPrompt = `Kamu membuat soal MCQ simulasi tes coding frontend.
Output HANYA JSON object: { "mcqs": [...] } tanpa markdown fence.
Setiap MCQ WAJIB:
- question: kalimat intro singkat (Bahasa Indonesia)
- code_snippet: WAJIB jika soal meminta analisis kode React/JS (isi kode mentah TANPA markdown fence, 8-25 baris)
- choices: ARRAY [{"id":"a","text":"..."},{"id":"b","text":"..."},{"id":"c","text":"..."},{"id":"d","text":"..."}]
- correct_id, explanation (1 kalimat), wrong_explanations (key a/b/c/d untuk pilihan salah)
- best_practices (tepat 2), learning_objective, points: 10, tags, difficulty, topic
Jangan taruh kode hanya di question tanpa code_snippet.`

func requestAIMCQBatch(ctx context.Context, client *ai.AnthropicClient, brief string, plan AIExamPlan, batch mcqGenBatch) ([]validate.MCQInput, error) {
	user := fmt.Sprintf(`Brief: %q
Fokus: %s | Difficulty: %s | Tags: %v
Buat TEPAT %d soal MCQ (nomor %d-%d).`,
		brief, plan.McqFocus, plan.SuggestedDifficulty, plan.Tags,
		batch.Count, batch.From, batch.From+batch.Count-1)

	maxTok := int64(1200 + batch.Count*800)
	if maxTok > 4096 {
		maxTok = 4096
	}
	var out struct {
		MCQs []validate.MCQInput `json:"mcqs"`
	}
	if err := completeJSONObject(ctx, client, mcqAISystemPrompt, user, maxTok, &out); err != nil {
		return nil, fmt.Errorf("parse MCQ AI: %w", err)
	}
	if len(out.MCQs) != batch.Count {
		return nil, fmt.Errorf("AI mengembalikan %d MCQ, butuh %d", len(out.MCQs), batch.Count)
	}
	for i := range out.MCQs {
		if err := validate.NormalizeAndValidateMCQ(&out.MCQs[i]); err != nil {
			return nil, fmt.Errorf("MCQ batch invalid: %w", err)
		}
	}
	return out.MCQs, nil
}

const buildAISystemPrompt = `Kamu membuat soal build React. Output HANYA JSON: {"build":{...}} tanpa markdown.
Wajib field: title, family, spec_markdown (bullet singkat), starter_code (maks 12 baris),
solution_code (maks 20 baris), solution_explanation (1-2 kalimat), rubric_json (3 criteria),
test_cases (1 item), best_practices (3 singkat), common_mistakes (2), learning_objective,
difficulty, points: 40. Kode minimal tanpa komentar panjang. Bahasa Indonesia.

Contoh ukuran (jangan copy isi, hanya struktur):
{"build":{"title":"Counter","family":"component","spec_markdown":"- Tampil count\\n- Tombol +1","starter_code":"export function Counter(){return <p>0</p>;}","solution_code":"import {useState} from \\"react\\";\\nexport function Counter(){const [n,setN]=useState(0);return <button onClick={()=>setN(n+1)}>{n}</button>;}","solution_explanation":"useState + handler klik","rubric_json":{"criteria":[{"id":"tests_pass","label":"Test lulus","points":25,"auto":true},{"id":"state","label":"State benar","points":10,"auto":false},{"id":"handler","label":"Handler klik","points":5,"auto":false}]},"test_cases":[{"name":"renders","assert":"ok"}],"best_practices":["Controlled state","Handler di event"],"common_mistakes":["Mutasi langsung","Lupa preventDefault"],"learning_objective":"State dasar","difficulty":"medium","points":40}}`

const debugAISystemPrompt = `Kamu membuat soal debug React. Output HANYA JSON: {"debug":{...}} tanpa markdown.
Wajib: title, family, broken_code (maks 18 baris), solution_code (maks 20 baris), bug_description,
root_cause, fix_explanation (singkat), test_cases (1), best_practices (2), common_mistakes (2),
learning_objective, difficulty, points: 40. Bug realistis (hook/state/key). Kode ringkas. Bahasa Indonesia.`

func requestAIBuild(ctx context.Context, client *ai.AnthropicClient, brief string, plan AIExamPlan) (validate.BuildInput, error) {
	user := fmt.Sprintf(`Brief: %q | Build: %s | %s | %v`, brief, plan.BuildFocus, plan.SuggestedDifficulty, plan.Tags)

	var out struct {
		Build validate.BuildInput `json:"build"`
	}
	if err := completeJSONObject(ctx, client, buildAISystemPrompt, user, codesimAIMaxOutputTokens, &out); err != nil {
		return validate.BuildInput{}, fmt.Errorf("parse build AI: %w", err)
	}
	return out.Build, nil
}

func requestAIDebug(ctx context.Context, client *ai.AnthropicClient, brief string, plan AIExamPlan) (validate.DebugInput, error) {
	user := fmt.Sprintf(`Brief: %q | Debug: %s | %s | %v`, brief, plan.DebugFocus, plan.SuggestedDifficulty, plan.Tags)

	var out struct {
		Debug validate.DebugInput `json:"debug"`
	}
	if err := completeJSONObject(ctx, client, debugAISystemPrompt, user, codesimAIMaxOutputTokens, &out); err != nil {
		return validate.DebugInput{}, fmt.Errorf("parse debug AI: %w", err)
	}
	return out.Debug, nil
}

func aiPayloadToExamQuestions(p aiExamPayload) []ExamQuestion {
	var out []ExamQuestion
	idx := 1
	for _, m := range p.MCQs {
		item := mcqInputToItem(m)
		meta, _ := json.Marshal(item)
		out = append(out, ExamQuestion{
			Index:             idx,
			Type:              QuestionTypeMCQ,
			SourceID:          fmt.Sprintf("ai-mcq-%d", idx),
			Points:            item.Points,
			LearningObjective: item.LearningObjective,
			MCQ: &ExamMCQ{
				Question:    item.Question,
				CodeSnippet: m.CodeSnippet,
				Choices:     item.Choices,
			},
			GradingMeta: meta,
		})
		idx++
	}
	build := buildInputToTask(p.Build)
	bmeta, _ := json.Marshal(build)
	out = append(out, ExamQuestion{
		Index:             idx,
		Type:              QuestionTypeReactBuild,
		SourceID:          "ai-build-1",
		Points:            build.Points,
		LearningObjective: build.LearningObjective,
		Build: &ExamBuild{
			Title:        build.Title,
			SpecMarkdown: build.SpecMarkdown,
			StarterCode:  build.StarterCode,
			TestCases:    build.TestCases,
		},
		GradingMeta: bmeta,
	})
	idx++
	dbg := debugInputToTask(p.Debug)
	dmeta, _ := json.Marshal(dbg)
	out = append(out, ExamQuestion{
		Index:             idx,
		Type:              QuestionTypeReactDebug,
		SourceID:          "ai-debug-1",
		Points:            dbg.Points,
		LearningObjective: dbg.LearningObjective,
		Debug: &ExamDebug{
			Title:          dbg.Title,
			BrokenCode:     dbg.BrokenCode,
			BugDescription: dbg.BugDescription,
		},
		GradingMeta: dmeta,
	})
	return out
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) >= 2 {
			lines = lines[1:]
			if strings.HasPrefix(lines[len(lines)-1], "```") {
				lines = lines[:len(lines)-1]
			}
			s = strings.Join(lines, "\n")
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	if strings.HasPrefix(s, "[") {
		start = strings.Index(s, "[")
		end = strings.LastIndex(s, "]")
		if start >= 0 && end > start {
			return s[start : end+1]
		}
	}
	return s
}

func deleteAIPlan(ctx context.Context, planID, accountID string) {
	if accountID != "" {
		_, _ = db.Exec(ctx, `DELETE FROM codesim_ai_plan WHERE id = $1 AND account_id = $2`, planID, accountID)
		return
	}
	_, _ = db.Exec(ctx, `DELETE FROM codesim_ai_plan WHERE id = $1 AND account_id IS NULL`, planID)
}

func mcqInputToItem(m validate.MCQInput) MCQItem {
	choices := make([]MCQChoice, len(m.Choices))
	for i, c := range m.Choices {
		choices[i] = MCQChoice{ID: c.ID, Text: c.Text}
	}
	return MCQItem{
		Tags:              m.Tags,
		Difficulty:        m.Difficulty,
		Question:          m.Question,
		CodeSnippet:       m.CodeSnippet,
		Choices:           choices,
		CorrectID:         m.CorrectID,
		Explanation:       m.Explanation,
		WrongExplanations: m.WrongExplanations,
		BestPractices:     m.BestPractices,
		LearningObjective: m.LearningObjective,
		Points:            m.Points,
		Topic:             m.Topic,
	}
}

func buildInputToTask(b validate.BuildInput) BuildTask {
	var rubric Rubric
	_ = json.Unmarshal(b.RubricJSON, &rubric)
	return BuildTask{
		Family:              b.Family,
		Title:               b.Title,
		SpecMarkdown:        b.SpecMarkdown,
		StarterCode:         b.StarterCode,
		SolutionCode:        b.SolutionCode,
		SolutionExplanation: b.SolutionExplanation,
		Rubric:              rubric,
		TestCases:           b.TestCases,
		BestPractices:       b.BestPractices,
		CommonMistakes:      b.CommonMistakes,
		LearningObjective:   b.LearningObjective,
		Difficulty:          b.Difficulty,
		Points:              b.Points,
	}
}

func debugInputToTask(d validate.DebugInput) DebugTask {
	return DebugTask{
		Family:            d.Family,
		Title:             d.Title,
		BrokenCode:        d.BrokenCode,
		SolutionCode:      d.SolutionCode,
		BugDescription:    d.BugDescription,
		RootCause:         d.RootCause,
		FixExplanation:    d.FixExplanation,
		TestCases:         d.TestCases,
		BestPractices:     d.BestPractices,
		CommonMistakes:    d.CommonMistakes,
		LearningObjective: d.LearningObjective,
		Difficulty:        d.Difficulty,
		Points:            d.Points,
	}
}
