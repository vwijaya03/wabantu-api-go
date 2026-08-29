package codesim

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// TopicTag is one selectable MCQ tag from the question bank.
type TopicTag struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	McqCount int    `json:"mcqCount"`
}

// TopicPreset is a suggested topic bundle for learners.
type TopicPreset struct {
	ID          string   `json:"id"`
	Label       string `json:"label"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type ExamFormatSection struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	Notes string `json:"notes,omitempty"`
}

// ExamFormat describes a fixed exam structure (e.g. Tendem Frontend Developer).
type ExamFormat struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	DurationMinutes int                 `json:"durationMinutes"`
	TotalQuestions  int                 `json:"totalQuestions"`
	Sections        []ExamFormatSection `json:"sections"`
	MixNotes        string              `json:"mixNotes,omitempty"`
}

type ListTopicsResponse struct {
	Tags            []TopicTag    `json:"tags"`
	Difficulties    []TopicTag    `json:"difficulties"`
	Presets         []TopicPreset `json:"presets"`
	Suggested       []string      `json:"suggested"`
	McqCountOptions []int         `json:"mcqCountOptions"`
	DefaultMcqCount int           `json:"defaultMcqCount"`
	AiGenEnabled    bool          `json:"aiGenEnabled"`
	ExamFormat      ExamFormat    `json:"examFormat"`
}

var topicLabels = map[string]string{
	"react":      "React",
	"hooks":      "React Hooks",
	"javascript": "JavaScript",
	"css":        "CSS",
	"html":       "HTML",
}

//encore:api public method=GET path=/api/v1/codesim/topics
func ListTopics(ctx context.Context) (*ListTopicsResponse, error) {
	if err := EnsureSeed(ctx); err != nil {
		return nil, err
	}

	tagCounts, err := loadTagCounts(ctx)
	if err != nil {
		return nil, err
	}
	diffCounts, err := loadDifficultyCounts(ctx)
	if err != nil {
		return nil, err
	}

	tags := make([]TopicTag, 0, len(tagCounts))
	for id, n := range tagCounts {
		tags = append(tags, TopicTag{
			ID:       id,
			Label:    topicLabel(id),
			McqCount: n,
		})
	}
	slices.SortFunc(tags, func(a, b TopicTag) int {
		return strings.Compare(a.Label, b.Label)
	})

	diffs := make([]TopicTag, 0, len(diffCounts))
	for id, n := range diffCounts {
		diffs = append(diffs, TopicTag{
			ID:       id,
			Label:    difficultyLabel(id),
			McqCount: n,
		})
	}
	slices.SortFunc(diffs, func(a, b TopicTag) int {
		return strings.Compare(a.ID, b.ID)
	})

	presets := defaultTopicPresets(tagCounts)
	suggested := []string{"react", "javascript"}
	if len(tags) > 0 {
		suggested = presets[0].Tags
	}

	return &ListTopicsResponse{
		Tags:            tags,
		Difficulties:    diffs,
		Presets:         presets,
		Suggested:       suggested,
		McqCountOptions: []int{3, 4, 5, 6, 7},
		DefaultMcqCount: defaultMcqCount(),
		AiGenEnabled:    LiveAIGenEnabled(),
		ExamFormat:      TendemExamFormat(),
	}, nil
}

// TendemExamFormat is the standard Tendem Frontend Developer test layout.
func TendemExamFormat() ExamFormat {
	return ExamFormat{
		ID:              "tendem-frontend-developer",
		Title:           "Tendem — Frontend Developer",
		DurationMinutes: 98,
		TotalQuestions:  7,
		Sections: []ExamFormatSection{
			{Label: "Multiple Choice Questions", Count: 5, Notes: "Q1–Q5"},
			{Label: "React Component (form)", Count: 1, Notes: "WaitlistForm or similar variant"},
			{Label: "React Debug", Count: 1, Notes: "Hero or similar variant"},
		},
		MixNotes: "Hard Mindrift/Tendem samples are mixed automatically (30 topic rotations). All sections use the hard question bank.",
	}
}

func defaultMcqCount() int {
	for _, sec := range DefaultBlueprintConfig().Sections {
		if sec.Type == QuestionTypeMCQ {
			return sec.Count
		}
	}
	return 5
}

func mcqCountFromConfig(cfg BlueprintConfig) int {
	for _, sec := range cfg.Sections {
		if sec.Type == QuestionTypeMCQ && sec.Count > 0 {
			return sec.Count
		}
	}
	return defaultMcqCount()
}

func loadTagCounts(ctx context.Context) (map[string]int, error) {
	rows, err := db.Query(ctx, `
		SELECT unnest(tags) AS tag, COUNT(*)::int
		FROM codesim_mcq_item
		GROUP BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var tag string
		var n int
		if err := rows.Scan(&tag, &n); err != nil {
			return nil, err
		}
		out[tag] = n
	}
	return out, rows.Err()
}

func loadDifficultyCounts(ctx context.Context) (map[string]int, error) {
	rows, err := db.Query(ctx, `
		SELECT difficulty, COUNT(*)::int
		FROM codesim_mcq_item
		GROUP BY difficulty`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

func topicLabel(id string) string {
	if l, ok := topicLabels[id]; ok {
		return l
	}
	if id == "" {
		return id
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

func difficultyLabel(id string) string {
	switch id {
	case "easy":
		return "Mudah"
	case "medium":
		return "Sedang"
	case "hard":
		return "Sulit"
	default:
		return id
	}
}

func defaultTopicPresets(tagCounts map[string]int) []TopicPreset {
	candidates := []TopicPreset{
		{
			ID:          "react-focus",
			Label:       "Fokus React",
			Description: "Hooks, state, forms, dan performa React",
			Tags:        []string{"react", "hooks"},
		},
		{
			ID:          "javascript-core",
			Label:       "JavaScript",
			Description: "Tipe, closure, dan pola JS dasar",
			Tags:        []string{"javascript"},
		},
		{
			ID:          "css-layout",
			Label:       "CSS & Layout",
			Description: "Flexbox, spacing, dan styling",
			Tags:        []string{"css"},
		},
		{
			ID:          "html-a11y",
			Label:       "HTML & A11y",
			Description: "Semantic HTML, landmark, dan aksesibilitas dasar",
			Tags:        []string{"html"},
		},
		{
			ID:          "full-frontend",
			Label:       "Campuran lengkap",
			Description: "Simulasi interview frontend umum",
			Tags:        []string{"react", "javascript", "css", "html"},
		},
	}
	var out []TopicPreset
	for _, p := range candidates {
		if presetHasQuestions(p.Tags, tagCounts) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []TopicPreset{{
			ID:          "all",
			Label:       "All topics",
			Description: "Soal dari seluruh bank",
			Tags:        nil,
		}}
	}
	return out
}

func presetHasQuestions(tags []string, counts map[string]int) bool {
	if len(tags) == 0 {
		return len(counts) > 0
	}
	for _, t := range tags {
		if counts[t] > 0 {
			return true
		}
	}
	return false
}

// BuildLearnerConfig creates an exam blueprint from learner topic/difficulty picks.
func BuildLearnerConfig(topics []string, difficulty string, mcqCount int) BlueprintConfig {
	return MergeLearnerMCQFilters(DefaultBlueprintConfig(), topics, difficulty, mcqCount)
}

// MergeLearnerMCQFilters applies optional MCQ filters onto a blueprint (build/debug sections unchanged).
func MergeLearnerMCQFilters(cfg BlueprintConfig, topics []string, difficulty string, mcqCount int) BlueprintConfig {
	for i := range cfg.Sections {
		if cfg.Sections[i].Type != QuestionTypeMCQ {
			continue
		}
		if len(topics) > 0 {
			cfg.Sections[i].Tags = topics
		}
		if d := strings.TrimSpace(difficulty); d != "" {
			cfg.Sections[i].Difficulty = d
		}
		if mcqCount > 0 {
			cfg.Sections[i].Count = mcqCount
		}
		break
	}
	return cfg
}

func selectionFromConfig(cfg BlueprintConfig) *SessionSelection {
	var topics []string
	var difficulty string
	var mcqCount int
	for _, sec := range cfg.Sections {
		if sec.Type == QuestionTypeMCQ {
			topics = append(topics, sec.Tags...)
			difficulty = sec.Difficulty
			mcqCount = sec.Count
			break
		}
	}
	if len(topics) == 0 && difficulty == "" && mcqCount == defaultMcqCount() {
		return nil
	}
	return &SessionSelection{Topics: topics, Difficulty: difficulty, McqCount: mcqCount}
}

func validateLearnerSelection(ctx context.Context, topics []string, difficulty string, mcqCount int) error {
	if len(topics) == 0 && difficulty == "" && mcqCount <= 0 {
		return nil
	}
	if mcqCount > 0 && (mcqCount < 3 || mcqCount > 7) {
		return fmt.Errorf("jumlah MCQ harus antara 3 dan 7")
	}
	need := mcqCount
	if need <= 0 {
		need = defaultMcqCount()
	}
	bank, err := loadBank(ctx)
	if err != nil {
		return err
	}
	count := len(filterMCQs(bank.MCQs, topics, difficulty))
	if count < need {
		return fmt.Errorf("not enough questions in the bank for this selection (available %d, need %d MCQs). Try other topics or fewer filters", count, need)
	}
	return nil
}

func filterMCQs(all []MCQItem, tags []string, difficulty string) []MCQItem {
	var out []MCQItem
	for _, m := range all {
		if difficulty != "" && m.Difficulty != difficulty {
			continue
		}
		if len(tags) > 0 && !tagsOverlap(m.Tags, tags) {
			continue
		}
		out = append(out, m)
	}
	return out
}
