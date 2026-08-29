package codesim

import (
	"encoding/json"
	"time"
)

const (
	SessionStatusSetup      = "setup"
	SessionStatusInProgress = "in_progress"
	SessionStatusSubmitted  = "submitted"
	SessionStatusExpired    = "expired"

	QuestionTypeMCQ        = "mcq"
	QuestionTypeReactBuild = "react_build"
	QuestionTypeReactDebug = "react_debug"
)

// BlueprintConfig is stored in codesim_blueprint.config_json.
type BlueprintConfig struct {
	Sections              []BlueprintSection `json:"sections"`
	TotalTimeLimitMinutes int                `json:"totalTimeLimitMinutes"`
	Proctoring            ProctoringConfig   `json:"proctoring"`
}

type BlueprintSection struct {
	Type               string   `json:"type"`
	Count              int      `json:"count"`
	TimeLimitMinutes   int      `json:"timeLimitMinutes"`
	Tags               []string `json:"tags,omitempty"`
	Difficulty         string   `json:"difficulty,omitempty"`
	ComponentFamily    string   `json:"componentFamily,omitempty"`
}

type ProctoringConfig struct {
	MaxBlurEvents       int  `json:"maxBlurEvents"`
	WarnOnPaste         bool `json:"warnOnPaste"`
	BlockPasteInEditor  bool `json:"blockPasteInEditor"`
}

type Blueprint struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug"`
	Title     string          `json:"title"`
	Config    BlueprintConfig `json:"config"`
	IsPublic  bool            `json:"isPublic"`
}

type MCQChoice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type MCQItem struct {
	ID                string            `json:"id"`
	Tags              []string          `json:"tags"`
	Difficulty        string            `json:"difficulty"`
	Question          string            `json:"question"`
	CodeSnippet       string            `json:"codeSnippet,omitempty"`
	Choices           []MCQChoice       `json:"choices"`
	CorrectID         string            `json:"correctId"`
	Explanation       string            `json:"explanation"`
	WrongExplanations map[string]string `json:"wrongExplanations"`
	BestPractices     []string          `json:"bestPractices"`
	LearningObjective string            `json:"learningObjective,omitempty"`
	Points            int               `json:"points"`
	Topic             string            `json:"topic,omitempty"`
}

type RubricCriterion struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Points int    `json:"points"`
	Auto   bool   `json:"auto,omitempty"`
}

type Rubric struct {
	Criteria []RubricCriterion `json:"criteria"`
}

type BuildTask struct {
	ID                  string   `json:"id"`
	Family              string   `json:"family"`
	Title               string   `json:"title"`
	SpecMarkdown        string   `json:"specMarkdown"`
	StarterCode         string   `json:"starterCode"`
	SolutionCode        string   `json:"solutionCode"`
	SolutionExplanation string   `json:"solutionExplanation"`
	Rubric              Rubric   `json:"rubric"`
	TestCases           json.RawMessage `json:"testCases"`
	BestPractices       []string `json:"bestPractices"`
	CommonMistakes      []string `json:"commonMistakes"`
	LearningObjective   string   `json:"learningObjective,omitempty"`
	Difficulty          string   `json:"difficulty"`
	Points              int      `json:"points"`
}

type DebugTask struct {
	ID                string          `json:"id"`
	Family            string          `json:"family"`
	Title             string          `json:"title"`
	BrokenCode        string          `json:"brokenCode"`
	SolutionCode      string          `json:"solutionCode"`
	BugDescription    string          `json:"bugDescription,omitempty"`
	RootCause         string          `json:"rootCause"`
	FixExplanation    string          `json:"fixExplanation"`
	TestCases         json.RawMessage `json:"testCases"`
	BestPractices     []string        `json:"bestPractices"`
	CommonMistakes    []string        `json:"commonMistakes"`
	LearningObjective string          `json:"learningObjective,omitempty"`
	Difficulty        string          `json:"difficulty"`
	Points            int             `json:"points"`
}

// ExamQuestion is a snapshot item in questions_json (no answers during exam).
type ExamQuestion struct {
	Index             int             `json:"index"`
	Type              string          `json:"type"`
	SourceID          string          `json:"sourceId"`
	Points            int             `json:"points"`
	LearningObjective string          `json:"learningObjective,omitempty"`
	MCQ               *ExamMCQ        `json:"mcq,omitempty"`
	Build             *ExamBuild      `json:"build,omitempty"`
	Debug             *ExamDebug      `json:"debug,omitempty"`
	// Private fields for grading/debrief (omitempty in exam API responses).
	GradingMeta       json.RawMessage `json:"gradingMeta,omitempty"`
}

type ExamMCQ struct {
	Question    string      `json:"question"`
	CodeSnippet string      `json:"codeSnippet,omitempty"`
	Choices     []MCQChoice `json:"choices"`
}

type ExamBuild struct {
	Title        string `json:"title"`
	SpecMarkdown string `json:"specMarkdown"`
	StarterCode  string `json:"starterCode"`
	TestCases    json.RawMessage `json:"testCases"`
}

type ExamDebug struct {
	Title          string `json:"title"`
	BrokenCode     string `json:"brokenCode"`
	BugDescription string `json:"bugDescription,omitempty"`
	TestCases      json.RawMessage `json:"testCases"`
}

type SessionAnswers struct {
	MCQ   map[string]string          `json:"mcq,omitempty"`
	Code  map[string]CodeAnswer      `json:"code,omitempty"`
}

type CodeAnswer struct {
	SourceCode  string          `json:"sourceCode"`
	TestsPassed bool            `json:"testsPassed"`
	TestResults json.RawMessage `json:"testResults,omitempty"`
}

type ExamSession struct {
	ID           string            `json:"id"`
	BlueprintID  string            `json:"blueprintId,omitempty"`
	Seed         int64             `json:"seed"`
	Status       string            `json:"status"`
	Questions    []ExamQuestion    `json:"questions"`
	Answers      SessionAnswers    `json:"answers,omitempty"`
	Selection    *SessionSelection `json:"selection,omitempty"`
	StartedAt    *time.Time        `json:"startedAt,omitempty"`
	ExpiresAt    *time.Time        `json:"expiresAt,omitempty"`
	SubmittedAt  *time.Time        `json:"submittedAt,omitempty"`
	TotalMinutes int               `json:"totalTimeLimitMinutes"`
}

type SessionSelection struct {
	Topics     []string `json:"topics,omitempty"`
	Difficulty string   `json:"difficulty,omitempty"`
	McqCount   int      `json:"mcqCount,omitempty"`
	PresetID   string   `json:"presetId,omitempty"`
}

// SessionSummary is a lightweight row for session history lists.
type SessionSummary struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	Source          string            `json:"source"`
	Label           string            `json:"label"`
	QuestionCount   int               `json:"questionCount"`
	Selection       *SessionSelection `json:"selection,omitempty"`
	Grade           string            `json:"grade,omitempty"`
	NormalizedScore int               `json:"normalizedScore,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	SubmittedAt     *time.Time        `json:"submittedAt,omitempty"`
}

type QuestionDebrief struct {
	ResultLabel       string   `json:"resultLabel"`
	Explanation       string   `json:"explanation"`
	AnswerFeedback    string   `json:"answerFeedback,omitempty"`
	BestPractices     []string `json:"bestPractices"`
	CommonMistakes    []string `json:"commonMistakes,omitempty"`
	LearningObjective string   `json:"learningObjective,omitempty"`
	SolutionCode      string   `json:"solutionCode,omitempty"`
	UserCode          string   `json:"userCode,omitempty"`
	CorrectAnswer     string   `json:"correctAnswer,omitempty"`
}

// ReportQuestionPrompt is the original question shown in post-exam review.
type ReportQuestionPrompt struct {
	Title       string      `json:"title,omitempty"`
	Body        string      `json:"body,omitempty"`
	CodeSnippet string      `json:"codeSnippet,omitempty"`
	Choices     []MCQChoice `json:"choices,omitempty"`
}

type ReportQuestion struct {
	Index        int                   `json:"index"`
	Type         string                `json:"type"`
	Correct      bool                  `json:"correct"`
	Partial      bool                  `json:"partial,omitempty"`
	EarnedPoints int                   `json:"earnedPoints"`
	MaxPoints    int                   `json:"maxPoints"`
	UserAnswer   string                `json:"userAnswer,omitempty"`
	Prompt       *ReportQuestionPrompt `json:"prompt,omitempty"`
	Debrief      QuestionDebrief       `json:"debrief"`
}

type LearningSummary struct {
	Strengths         []string `json:"strengths,omitempty"`
	Weaknesses        []string `json:"weaknesses,omitempty"`
	RecommendedTopics []string `json:"recommendedTopics,omitempty"`
}

type SessionReport struct {
	SessionID       string          `json:"sessionId"`
	EarnedPoints    int             `json:"earnedPoints"`
	TotalPoints     int             `json:"totalPoints"`
	NormalizedScore int             `json:"normalizedScore"`
	Grade           string          `json:"grade"`
	Questions       []ReportQuestion `json:"questions"`
	LearningSummary LearningSummary `json:"learningSummary"`
	ProctorSummary  ProctorSummary  `json:"proctorSummary"`
}

type ProctorSummary struct {
	BlurEvents int `json:"blurEvents"`
	PasteEvents int `json:"pasteEvents"`
}
