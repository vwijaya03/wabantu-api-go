package validate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MCQChoice is one multiple-choice option.
type MCQChoice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// mcqChoices accepts seed format ([{id,text}]) and common AI variants (object map).
type mcqChoices []MCQChoice

func (c *mcqChoices) UnmarshalJSON(data []byte) error {
	data = bytesTrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*c = nil
		return nil
	}

	var arr []MCQChoice
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 && arr[0].ID != "" {
		*c = arr
		return nil
	}

	// Array of objects with alternate keys (label/value) or single-key maps per item.
	var flexArr []map[string]string
	if err := json.Unmarshal(data, &flexArr); err == nil && len(flexArr) > 0 {
		out := make([]MCQChoice, 0, len(flexArr))
		for i, item := range flexArr {
			if ch := choiceFromMap(item, i); ch.ID != "" && ch.Text != "" {
				out = append(out, ch)
			}
		}
		if len(out) > 0 {
			*c = out
			return nil
		}
	}

	// Object map: {"a":"text", "b":"text", ...}
	var obj map[string]string
	if err := json.Unmarshal(data, &obj); err == nil && len(obj) > 0 {
		*c = choicesFromStringMap(obj)
		return nil
	}

	// Nested object values: {"a":{"text":"..."}, ...}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(data, &nested); err == nil && len(nested) > 0 {
		out := make([]MCQChoice, 0, len(nested))
		for id, raw := range nested {
			var inner map[string]string
			if err := json.Unmarshal(raw, &inner); err == nil {
				inner["id"] = id
				if ch := choiceFromMap(inner, 0); ch.Text != "" {
					out = append(out, ch)
					continue
				}
			}
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && strings.TrimSpace(text) != "" {
				out = append(out, MCQChoice{ID: id, Text: text})
			}
		}
		if len(out) > 0 {
			sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
			*c = out
			return nil
		}
	}

	return fmt.Errorf("choices: unsupported JSON shape")
}

func choiceFromMap(m map[string]string, index int) MCQChoice {
	id := strings.TrimSpace(m["id"])
	text := strings.TrimSpace(m["text"])
	if text == "" {
		text = strings.TrimSpace(m["label"])
	}
	if text == "" {
		text = strings.TrimSpace(m["value"])
	}
	if id == "" {
		fallback := []string{"a", "b", "c", "d"}
		if index >= 0 && index < len(fallback) {
			id = fallback[index]
		}
		for k, v := range m {
			if k == "id" || k == "text" || k == "label" || k == "value" {
				continue
			}
			if strings.TrimSpace(v) != "" {
				return MCQChoice{ID: k, Text: v}
			}
		}
	}
	return MCQChoice{ID: id, Text: text}
}

func choicesFromStringMap(obj map[string]string) []MCQChoice {
	order := []string{"a", "b", "c", "d"}
	seen := map[string]bool{}
	out := make([]MCQChoice, 0, len(obj))
	for _, id := range order {
		if text, ok := obj[id]; ok && strings.TrimSpace(text) != "" {
			out = append(out, MCQChoice{ID: id, Text: text})
			seen[id] = true
		}
	}
	rest := make([]string, 0)
	for id := range obj {
		if !seen[id] && strings.TrimSpace(obj[id]) != "" {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	for _, id := range rest {
		out = append(out, MCQChoice{ID: id, Text: obj[id]})
	}
	return out
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// MCQInput is the JSON shape for validating MCQ seed files.
type MCQInput struct {
	Question          string            `json:"question"`
	CodeSnippet       string            `json:"code_snippet,omitempty"`
	Choices           mcqChoices        `json:"choices"`
	CorrectID         string            `json:"correct_id"`
	Explanation       string            `json:"explanation"`
	WrongExplanations map[string]string `json:"wrong_explanations"`
	BestPractices     []string          `json:"best_practices"`
	LearningObjective string            `json:"learning_objective"`
	Points            int               `json:"points"`
	Tags              []string          `json:"tags"`
	Difficulty        string            `json:"difficulty"`
	Topic             string            `json:"topic"`
}

// BuildInput validates build task seed JSON.
type BuildInput struct {
	Title               string          `json:"title"`
	Family              string          `json:"family"`
	SpecMarkdown        string          `json:"spec_markdown"`
	StarterCode         string          `json:"starter_code"`
	SolutionCode        string          `json:"solution_code"`
	SolutionExplanation string          `json:"solution_explanation"`
	RubricJSON          json.RawMessage `json:"rubric_json"`
	TestCases           json.RawMessage `json:"test_cases"`
	BestPractices       []string        `json:"best_practices"`
	CommonMistakes      []string        `json:"common_mistakes"`
	LearningObjective   string          `json:"learning_objective"`
	Difficulty          string          `json:"difficulty"`
	Points              int             `json:"points"`
}

// DebugInput validates debug task seed JSON.
type DebugInput struct {
	Title             string          `json:"title"`
	Family            string          `json:"family"`
	BrokenCode        string          `json:"broken_code"`
	SolutionCode      string          `json:"solution_code"`
	RootCause         string          `json:"root_cause"`
	FixExplanation    string          `json:"fix_explanation"`
	BugDescription    string          `json:"bug_description"`
	TestCases         json.RawMessage `json:"test_cases"`
	BestPractices     []string        `json:"best_practices"`
	CommonMistakes    []string        `json:"common_mistakes"`
	LearningObjective string          `json:"learning_objective"`
	Difficulty        string          `json:"difficulty"`
	Points            int             `json:"points"`
}

func ValidateMCQ(m *MCQInput) error {
	if m == nil {
		return fmt.Errorf("mcq: nil input")
	}
	if strings.TrimSpace(m.Question) == "" {
		return fmt.Errorf("mcq: question required")
	}
	if len(m.Choices) != 4 {
		return fmt.Errorf("mcq: exactly 4 choices required, got %d", len(m.Choices))
	}
	if strings.TrimSpace(m.CorrectID) == "" {
		return fmt.Errorf("mcq: correct_id required")
	}
	if strings.TrimSpace(m.Explanation) == "" {
		return fmt.Errorf("mcq: explanation required")
	}
	if len(m.BestPractices) < 2 {
		return fmt.Errorf("mcq: at least 2 best_practices required")
	}
	choiceIDs := map[string]bool{}
	for _, c := range m.Choices {
		if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("mcq: choice id and text required")
		}
		choiceIDs[c.ID] = true
	}
	if !choiceIDs[m.CorrectID] {
		return fmt.Errorf("mcq: correct_id not in choices")
	}
	for _, c := range m.Choices {
		if c.ID == m.CorrectID {
			continue
		}
		if strings.TrimSpace(m.WrongExplanations[c.ID]) == "" {
			return fmt.Errorf("mcq: wrong_explanations missing for choice %q", c.ID)
		}
	}
	if m.Points <= 0 {
		return fmt.Errorf("mcq: points must be positive")
	}
	return nil
}

func ValidateBuild(b *BuildInput) error {
	if b == nil {
		return fmt.Errorf("build: nil input")
	}
	if strings.TrimSpace(b.Title) == "" {
		return fmt.Errorf("build: title required")
	}
	if strings.TrimSpace(b.StarterCode) == "" || strings.TrimSpace(b.SolutionCode) == "" {
		return fmt.Errorf("build: starter_code and solution_code required")
	}
	if strings.TrimSpace(b.SolutionExplanation) == "" {
		return fmt.Errorf("build: solution_explanation required")
	}
	if len(b.BestPractices) < 3 {
		return fmt.Errorf("build: at least 3 best_practices required")
	}
	if len(b.CommonMistakes) < 2 {
		return fmt.Errorf("build: at least 2 common_mistakes required")
	}
	if len(b.RubricJSON) == 0 {
		return fmt.Errorf("build: rubric_json required")
	}
	var rubric struct {
		Criteria []struct {
			ID string `json:"id"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal(b.RubricJSON, &rubric); err != nil {
		return fmt.Errorf("build: rubric_json invalid: %w", err)
	}
	if len(rubric.Criteria) < 3 {
		return fmt.Errorf("build: rubric_json needs at least 3 criteria")
	}
	if b.Points <= 0 {
		return fmt.Errorf("build: points must be positive")
	}
	return nil
}

func ValidateDebug(d *DebugInput) error {
	if d == nil {
		return fmt.Errorf("debug: nil input")
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("debug: title required")
	}
	if strings.TrimSpace(d.BrokenCode) == "" || strings.TrimSpace(d.SolutionCode) == "" {
		return fmt.Errorf("debug: broken_code and solution_code required")
	}
	if strings.TrimSpace(d.RootCause) == "" {
		return fmt.Errorf("debug: root_cause required")
	}
	if strings.TrimSpace(d.FixExplanation) == "" {
		return fmt.Errorf("debug: fix_explanation required")
	}
	if len(d.BestPractices) < 2 {
		return fmt.Errorf("debug: at least 2 best_practices required")
	}
	if d.Points <= 0 {
		return fmt.Errorf("debug: points must be positive")
	}
	return nil
}
