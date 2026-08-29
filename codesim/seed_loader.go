package codesim

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"encore.app/wabantu/codesim/validate"
)

//go:embed seed/*.json
var seedFS embed.FS

const tendemFormBuildTarget = 45

// EnsureSeed loads embedded question bank into DB when tables are empty.
func EnsureSeed(ctx context.Context) error {
	var n int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_mcq_item`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if err := importEmbeddedMCQ(ctx, "seed/mcq.json"); err != nil {
			return err
		}
		if err := importEmbeddedBuild(ctx, "seed/build.json"); err != nil {
			return err
		}
		if err := importEmbeddedDebug(ctx, "seed/debug.json"); err != nil {
			return err
		}
	}
	if err := importTendemBank(ctx); err != nil {
		return err
	}
	if err := syncTendemBankFromEmbed(ctx); err != nil {
		return err
	}
	if err := purgeFillerMCQs(ctx); err != nil {
		return err
	}
	if err := ensureBlueprints(ctx); err != nil {
		return err
	}
	return ensureHardBlueprints(ctx)
}

func ensureHardBlueprints(ctx context.Context) error {
	raw, err := seedFS.ReadFile("seed/tendem_blueprints_hard.json")
	if err != nil {
		return nil
	}
	var seeds []blueprintSeed
	if err := json.Unmarshal(raw, &seeds); err != nil {
		return err
	}
	for _, s := range seeds {
		var n int
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_blueprint WHERE slug = $1`, s.Slug).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			_, err := db.Exec(ctx, `
				UPDATE codesim_blueprint SET title = $2, config_json = $3
				WHERE slug = $1`,
				s.Slug, s.Title, s.Config,
			)
			if err != nil {
				return err
			}
			continue
		}
		_, err := db.Exec(ctx, `
			INSERT INTO codesim_blueprint (id, slug, title, config_json, is_public)
			VALUES ($1, $2, $3, $4, true)`,
			uuid.New(), s.Slug, s.Title, s.Config,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func importTendemBank(ctx context.Context) error {
	for _, path := range []string{"seed/tendem_mcq.json", "seed/tendem_mcq_batch2.json", "seed/tendem_mcq_hard.json"} {
		if err := importEmbeddedMCQ(ctx, path); err != nil {
			return err
		}
	}
	if err := importEmbeddedBuild(ctx, "seed/tendem_build.json"); err != nil {
		return err
	}
	return importEmbeddedDebug(ctx, "seed/tendem_debug.json")
}

func importTendemIfNeeded(ctx context.Context) error {
	var formBuilds int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_build_task WHERE family = 'form'`).Scan(&formBuilds); err != nil {
		return err
	}
	if formBuilds >= tendemFormBuildTarget {
		return nil
	}
	if err := importEmbeddedMCQ(ctx, "seed/tendem_mcq.json"); err != nil {
		return err
	}
	if err := importEmbeddedMCQ(ctx, "seed/tendem_mcq_batch2.json"); err != nil {
		return err
	}
	if err := importEmbeddedBuild(ctx, "seed/tendem_build.json"); err != nil {
		return err
	}
	return importEmbeddedDebug(ctx, "seed/tendem_debug.json")
}

func purgeFillerMCQs(ctx context.Context) error {
	_, err := db.Exec(ctx, `
		DELETE FROM codesim_mcq_item
		WHERE topic LIKE 'fe-concept-%'
		   OR question LIKE 'Frontend concept check #%'`)
	return err
}

func importEmbeddedMCQ(ctx context.Context, path string) error {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []validate.MCQInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := validate.ValidateMCQ(&it); err != nil {
			return fmt.Errorf("mcq seed %s: %w", path, err)
		}
		var exists int
		_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_mcq_item WHERE question = $1`, it.Question).Scan(&exists)
		if exists > 0 {
			continue
		}
		wrong, _ := json.Marshal(it.WrongExplanations)
		bp, _ := json.Marshal(it.BestPractices)
		_, err := db.Exec(ctx, `
			INSERT INTO codesim_mcq_item (
				id, tags, difficulty, question, choices, correct_id, explanation,
				wrong_explanations, best_practices, learning_objective, points, topic
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			uuid.New(), it.Tags, it.Difficulty, it.Question, mustJSON(it.Choices),
			it.CorrectID, it.Explanation, wrong, bp, it.LearningObjective, it.Points, it.Topic,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func importEmbeddedBuild(ctx context.Context, path string) error {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return err
	}
	var items []validate.BuildInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := validate.ValidateBuild(&it); err != nil {
			return fmt.Errorf("build seed %s: %w", path, err)
		}
		var exists int
		_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_build_task WHERE title = $1`, it.Title).Scan(&exists)
		if exists > 0 {
			continue
		}
		bp, _ := json.Marshal(it.BestPractices)
		cm, _ := json.Marshal(it.CommonMistakes)
		_, err := db.Exec(ctx, `
			INSERT INTO codesim_build_task (
				id, family, title, spec_markdown, starter_code, solution_code,
				solution_explanation, rubric_json, test_cases, best_practices,
				common_mistakes, learning_objective, difficulty, points
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			uuid.New(), it.Family, it.Title, it.SpecMarkdown, it.StarterCode, it.SolutionCode,
			it.SolutionExplanation, it.RubricJSON, it.TestCases, bp, cm,
			it.LearningObjective, it.Difficulty, it.Points,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func importEmbeddedDebug(ctx context.Context, path string) error {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return err
	}
	var items []validate.DebugInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := validate.ValidateDebug(&it); err != nil {
			return fmt.Errorf("debug seed %s: %w", path, err)
		}
		var exists int
		_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_debug_task WHERE title = $1`, it.Title).Scan(&exists)
		if exists > 0 {
			continue
		}
		bp, _ := json.Marshal(it.BestPractices)
		cm, _ := json.Marshal(it.CommonMistakes)
		_, err := db.Exec(ctx, `
			INSERT INTO codesim_debug_task (
				id, family, title, broken_code, solution_code, bug_description,
				root_cause, fix_explanation, test_cases, best_practices,
				common_mistakes, learning_objective, difficulty, points
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			uuid.New(), it.Family, it.Title, it.BrokenCode, it.SolutionCode, it.BugDescription,
			it.RootCause, it.FixExplanation, it.TestCases, bp, cm,
			it.LearningObjective, it.Difficulty, it.Points,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func syncTendemBankFromEmbed(ctx context.Context) error {
	if err := syncEmbeddedMCQBank(ctx, "seed/tendem_mcq.json"); err != nil {
		return err
	}
	if err := syncEmbeddedMCQBank(ctx, "seed/tendem_mcq_batch2.json"); err != nil {
		return err
	}
	if err := syncEmbeddedMCQBank(ctx, "seed/tendem_mcq_hard.json"); err != nil {
		return err
	}
	if err := syncEmbeddedBuildBank(ctx, "seed/tendem_build.json"); err != nil {
		return err
	}
	return syncEmbeddedDebugBank(ctx, "seed/tendem_debug.json")
}

func syncEmbeddedMCQBank(ctx context.Context, path string) error {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []validate.MCQInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := validate.ValidateMCQ(&it); err != nil {
			return fmt.Errorf("sync mcq %s: %w", it.Topic, err)
		}
		if it.Topic == "" {
			continue
		}
		wrong, _ := json.Marshal(it.WrongExplanations)
		bp, _ := json.Marshal(it.BestPractices)
		_, err := db.Exec(ctx, `
			UPDATE codesim_mcq_item SET
				tags = $2,
				difficulty = $3,
				question = $4,
				choices = $5,
				correct_id = $6,
				explanation = $7,
				wrong_explanations = $8,
				best_practices = $9,
				learning_objective = $10,
				points = $11
			WHERE topic = $1`,
			it.Topic, it.Tags, it.Difficulty, it.Question, mustJSON(it.Choices),
			it.CorrectID, it.Explanation, wrong, bp, it.LearningObjective, it.Points,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func syncEmbeddedBuildBank(ctx context.Context, path string) error {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []validate.BuildInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := validate.ValidateBuild(&it); err != nil {
			return fmt.Errorf("sync build %s: %w", it.Title, err)
		}
		bp, _ := json.Marshal(it.BestPractices)
		cm, _ := json.Marshal(it.CommonMistakes)
		_, err := db.Exec(ctx, `
			UPDATE codesim_build_task SET
				spec_markdown = $2,
				starter_code = $3,
				solution_code = $4,
				solution_explanation = $5,
				rubric_json = $6,
				test_cases = $7,
				best_practices = $8,
				common_mistakes = $9,
				learning_objective = $10,
				difficulty = $11,
				points = $12
			WHERE title = $1`,
			it.Title, it.SpecMarkdown, it.StarterCode, it.SolutionCode,
			it.SolutionExplanation, it.RubricJSON, it.TestCases, bp, cm,
			it.LearningObjective, it.Difficulty, it.Points,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func syncEmbeddedDebugBank(ctx context.Context, path string) error {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []validate.DebugInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, it := range items {
		if err := validate.ValidateDebug(&it); err != nil {
			return fmt.Errorf("sync debug %s: %w", it.Title, err)
		}
		bp, _ := json.Marshal(it.BestPractices)
		cm, _ := json.Marshal(it.CommonMistakes)
		_, err := db.Exec(ctx, `
			UPDATE codesim_debug_task SET
				broken_code = $2,
				solution_code = $3,
				bug_description = $4,
				root_cause = $5,
				fix_explanation = $6,
				test_cases = $7,
				best_practices = $8,
				common_mistakes = $9,
				learning_objective = $10,
				difficulty = $11,
				points = $12
			WHERE title = $1`,
			it.Title, it.BrokenCode, it.SolutionCode, it.BugDescription,
			it.RootCause, it.FixExplanation, it.TestCases, bp, cm,
			it.LearningObjective, it.Difficulty, it.Points,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

type blueprintSeed struct {
	Slug   string          `json:"slug"`
	Title  string          `json:"title"`
	Config json.RawMessage `json:"config"`
}

func ensureBlueprints(ctx context.Context) error {
	if err := ensureDefaultBlueprint(ctx); err != nil {
		return err
	}
	raw, err := seedFS.ReadFile("seed/tendem_blueprints.json")
	if err != nil {
		return err
	}
	var seeds []blueprintSeed
	if err := json.Unmarshal(raw, &seeds); err != nil {
		return err
	}
	for _, s := range seeds {
		var n int
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_blueprint WHERE slug = $1`, s.Slug).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		_, err := db.Exec(ctx, `
			INSERT INTO codesim_blueprint (id, slug, title, config_json, is_public)
			VALUES ($1, $2, $3, $4, true)`,
			uuid.New(), s.Slug, s.Title, s.Config,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func ensureDefaultBlueprint(ctx context.Context) error {
	var n int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM codesim_blueprint WHERE slug = 'frontend-standard-v1'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		_, _ = db.Exec(ctx, `
			UPDATE codesim_blueprint
			SET config_json = $2, title = 'Tendem Frontend Developer (Standard)'
			WHERE slug = 'frontend-standard-v1'`,
			mustJSON(DefaultBlueprintConfig()),
		)
		return nil
	}
	cfg := DefaultBlueprintConfig()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO codesim_blueprint (id, slug, title, config_json, is_public)
		VALUES ($1, 'frontend-standard-v1', 'Tendem — Frontend Developer', $2, true)`,
		uuid.New(), raw,
	)
	return err
}

func DefaultBlueprintConfig() BlueprintConfig {
	return BlueprintConfig{
		Sections: []BlueprintSection{
			{Type: QuestionTypeMCQ, Count: 5, TimeLimitMinutes: 40, Tags: []string{"react", "javascript", "css", "html"}},
			{Type: QuestionTypeReactBuild, Count: 1, TimeLimitMinutes: 35, ComponentFamily: "form"},
			{Type: QuestionTypeReactDebug, Count: 1, TimeLimitMinutes: 23, ComponentFamily: "hero"},
		},
		TotalTimeLimitMinutes: 98,
		Proctoring: ProctoringConfig{
			MaxBlurEvents:      3,
			WarnOnPaste:        true,
			BlockPasteInEditor: true,
		},
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
