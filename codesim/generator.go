package codesim

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"

	"encore.app/wabantu/codesim/validate"
)

type bankData struct {
	MCQs   []MCQItem
	Builds []BuildTask
	Debugs []DebugTask
}

func loadBank(ctx context.Context) (*bankData, error) {
	if err := EnsureSeed(ctx); err != nil {
		return nil, err
	}
	b := &bankData{}
	rows, err := db.Query(ctx, `
		SELECT id::text, tags, difficulty, question, COALESCE(code_snippet, ''), choices, correct_id, explanation,
		       wrong_explanations, best_practices, learning_objective, points, topic
		FROM codesim_mcq_item`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m MCQItem
		var choices, wrong, bp json.RawMessage
		if err := rows.Scan(&m.ID, &m.Tags, &m.Difficulty, &m.Question, &m.CodeSnippet, &choices, &m.CorrectID,
			&m.Explanation, &wrong, &bp, &m.LearningObjective, &m.Points, &m.Topic); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(choices, &m.Choices)
		_ = json.Unmarshal(wrong, &m.WrongExplanations)
		_ = json.Unmarshal(bp, &m.BestPractices)
		b.MCQs = append(b.MCQs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	brows, err := db.Query(ctx, `
		SELECT id::text, family, title, spec_markdown, starter_code, solution_code,
		       solution_explanation, rubric_json, test_cases, best_practices, common_mistakes,
		       learning_objective, difficulty, points
		FROM codesim_build_task`)
	if err != nil {
		return nil, err
	}
	defer brows.Close()
	for brows.Next() {
		var t BuildTask
		var rubric, tc, bp, cm json.RawMessage
		if err := brows.Scan(&t.ID, &t.Family, &t.Title, &t.SpecMarkdown, &t.StarterCode, &t.SolutionCode,
			&t.SolutionExplanation, &rubric, &tc, &bp, &cm, &t.LearningObjective, &t.Difficulty, &t.Points); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rubric, &t.Rubric)
		t.TestCases = tc
		_ = json.Unmarshal(bp, &t.BestPractices)
		_ = json.Unmarshal(cm, &t.CommonMistakes)
		b.Builds = append(b.Builds, t)
	}
	if err := brows.Err(); err != nil {
		return nil, err
	}

	drows, err := db.Query(ctx, `
		SELECT id::text, family, title, broken_code, solution_code, bug_description,
		       root_cause, fix_explanation, test_cases, best_practices, common_mistakes,
		       learning_objective, difficulty, points
		FROM codesim_debug_task`)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var t DebugTask
		var tc, bp, cm json.RawMessage
		if err := drows.Scan(&t.ID, &t.Family, &t.Title, &t.BrokenCode, &t.SolutionCode, &t.BugDescription,
			&t.RootCause, &t.FixExplanation, &tc, &bp, &cm, &t.LearningObjective, &t.Difficulty, &t.Points); err != nil {
			return nil, err
		}
		t.TestCases = tc
		_ = json.Unmarshal(bp, &t.BestPractices)
		_ = json.Unmarshal(cm, &t.CommonMistakes)
		b.Debugs = append(b.Debugs, t)
	}
	return b, drows.Err()
}

// GenerateExamPaper builds exam questions from blueprint and seed.
func GenerateExamPaper(ctx context.Context, cfg BlueprintConfig, seed int64, exclude map[string]bool) ([]ExamQuestion, error) {
	bank, err := loadBank(ctx)
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewSource(seed))
	var out []ExamQuestion
	idx := 1

	for _, sec := range cfg.Sections {
		switch sec.Type {
		case QuestionTypeMCQ:
			pool := filterExcludedMCQs(bank.MCQs, exclude)
			qs, err := pickMCQs(pool, sec.Tags, sec.Difficulty, sec.Count, rng)
			if err != nil {
				return nil, err
			}
			for _, m := range qs {
				meta, _ := json.Marshal(m)
				choices := make([]validate.MCQChoice, len(m.Choices))
				for i, c := range m.Choices {
					choices[i] = validate.MCQChoice{ID: c.ID, Text: c.Text}
				}
				qi := validate.MCQInputFromParts(m.Question, choices)
				if strings.TrimSpace(m.CodeSnippet) != "" {
					qi.CodeSnippet = strings.TrimSpace(m.CodeSnippet)
				}
				validate.NormalizeMCQInput(&qi)
				out = append(out, ExamQuestion{
					Index:             idx,
					Type:              QuestionTypeMCQ,
					SourceID:          m.ID,
					Points:            m.Points,
					LearningObjective: m.LearningObjective,
					MCQ: &ExamMCQ{
						Question:    qi.Question,
						CodeSnippet: qi.CodeSnippet,
						Choices:     m.Choices,
					},
					GradingMeta: meta,
				})
				idx++
			}
		case QuestionTypeReactBuild:
			pool := filterExcludedBuilds(bank.Builds, exclude)
			t, err := pickBuild(pool, sec.ComponentFamily, sec.Difficulty, rng)
			if err != nil {
				return nil, err
			}
			meta, _ := json.Marshal(t)
			out = append(out, ExamQuestion{
				Index:             idx,
				Type:              QuestionTypeReactBuild,
				SourceID:          t.ID,
				Points:            t.Points,
				LearningObjective: t.LearningObjective,
				Build: &ExamBuild{
					Title:        t.Title,
					SpecMarkdown: t.SpecMarkdown,
					StarterCode:  t.StarterCode,
					TestCases:    t.TestCases,
				},
				GradingMeta: meta,
			})
			idx++
		case QuestionTypeReactDebug:
			pool := filterExcludedDebugs(bank.Debugs, exclude)
			t, err := pickDebug(pool, sec.ComponentFamily, sec.Difficulty, rng)
			if err != nil {
				return nil, err
			}
			meta, _ := json.Marshal(t)
			out = append(out, ExamQuestion{
				Index:             idx,
				Type:              QuestionTypeReactDebug,
				SourceID:          t.ID,
				Points:            t.Points,
				LearningObjective: t.LearningObjective,
				Debug: &ExamDebug{
					Title:          t.Title,
					BrokenCode:     t.BrokenCode,
					BugDescription: t.BugDescription,
					TestCases:      t.TestCases,
				},
				GradingMeta: meta,
			})
			idx++
		}
	}
	return out, nil
}

func pickMCQs(all []MCQItem, tags []string, difficulty string, count int, rng *rand.Rand) ([]MCQItem, error) {
	pool := filterMCQs(all, tags, difficulty)
	if len(pool) < count {
		pool = filterMCQs(all, tags, "")
	}
	if len(pool) < count {
		pool = all
	}
	if len(pool) < count {
		return nil, fmt.Errorf("not enough mcq items: need %d have %d", count, len(pool))
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:count], nil
}

func pickBuild(all []BuildTask, family, difficulty string, rng *rand.Rand) (BuildTask, error) {
	pool := filterBuilds(all, family, difficulty)
	if len(pool) == 0 {
		pool = filterBuilds(all, family, "")
	}
	if len(pool) == 0 {
		var fallback []BuildTask
		for _, t := range all {
			if family == "" || t.Family == family {
				fallback = append(fallback, t)
			}
		}
		pool = fallback
	}
	if len(pool) == 0 {
		pool = all
	}
	if len(pool) == 0 {
		return BuildTask{}, fmt.Errorf("no build tasks in bank")
	}
	return pool[rng.Intn(len(pool))], nil
}

func pickDebug(all []DebugTask, family, difficulty string, rng *rand.Rand) (DebugTask, error) {
	pool := filterDebugs(all, family, difficulty)
	if len(pool) == 0 {
		pool = filterDebugs(all, family, "")
	}
	if len(pool) == 0 {
		var fallback []DebugTask
		for _, t := range all {
			if family == "" || t.Family == family {
				fallback = append(fallback, t)
			}
		}
		pool = fallback
	}
	if len(pool) == 0 {
		pool = all
	}
	if len(pool) == 0 {
		return DebugTask{}, fmt.Errorf("no debug tasks in bank")
	}
	return pool[rng.Intn(len(pool))], nil
}

func filterBuilds(all []BuildTask, family, difficulty string) []BuildTask {
	var out []BuildTask
	for _, t := range all {
		if difficulty != "" && t.Difficulty != difficulty {
			continue
		}
		if family != "" && t.Family != family {
			continue
		}
		out = append(out, t)
	}
	return out
}

func filterDebugs(all []DebugTask, family, difficulty string) []DebugTask {
	var out []DebugTask
	for _, t := range all {
		if difficulty != "" && t.Difficulty != difficulty {
			continue
		}
		if family != "" && t.Family != family {
			continue
		}
		out = append(out, t)
	}
	return out
}

func tagsOverlap(itemTags, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, t := range itemTags {
		if slices.Contains(filter, t) {
			return true
		}
	}
	return false
}
