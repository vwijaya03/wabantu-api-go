package codesim

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	appErrs "encore.app/wabantu/shared/errs"
)

//encore:api public method=GET path=/api/v1/codesim/blueprints
func ListBlueprints(ctx context.Context) (*ListBlueprintsResponse, error) {
	if err := EnsureSeed(ctx); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `
		SELECT id::text, slug, title, config_json, is_public
		FROM codesim_blueprint WHERE is_public = true ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Blueprint
	for rows.Next() {
		var b Blueprint
		var cfg json.RawMessage
		if err := rows.Scan(&b.ID, &b.Slug, &b.Title, &cfg, &b.IsPublic); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cfg, &b.Config)
		list = append(list, b)
	}
	return &ListBlueprintsResponse{Blueprints: list}, rows.Err()
}

type ListBlueprintsResponse struct {
	Blueprints []Blueprint `json:"blueprints"`
}

type CreateSessionParams struct {
	BlueprintID   string           `json:"blueprintId"`
	BlueprintSlug string           `json:"blueprintSlug,omitempty"`
	Seed          *int64           `json:"seed,omitempty"`
	Topics        []string         `json:"topics,omitempty"`
	Difficulty    string           `json:"difficulty,omitempty"`
	McqCount      int              `json:"mcqCount,omitempty"`
	PresetID      string           `json:"presetId,omitempty"`
	AiPlanID        string           `json:"aiPlanId,omitempty"`
	ReuseSessionID  string           `json:"reuseSessionId,omitempty"`
	CustomConfig    *BlueprintConfig `json:"customConfig,omitempty"`
	ClientToken     string           `json:"clientToken,omitempty"`
}

type CreateSessionResponse struct {
	Session ExamSession `json:"session"`
}

type ListSessionsParams struct {
	Limit        int    `query:"limit"`
	IDs          string `query:"ids"`
	ClientToken  string `query:"clientToken"`
}

type ListSessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

//encore:api public method=GET path=/api/v1/codesim/sessions
func ListSessions(ctx context.Context, p *ListSessionsParams) (*ListSessionsResponse, error) {
	accountID := optionalAccountID(ctx)
	limit := defaultSessionListLimit
	var idList []string
	clientToken := ""
	if p != nil {
		if p.Limit > 0 {
			limit = p.Limit
		}
		idList = parseSessionIDList(p.IDs)
		clientToken = parseClientToken(p.ClientToken)
	}
	if limit > maxSessionListLimit {
		limit = maxSessionListLimit
	}

	var groups [][]SessionSummary
	if accountID != "" {
		userSessions, err := listSessionsForUser(ctx, accountID, limit)
		if err != nil {
			return nil, err
		}
		groups = append(groups, userSessions)
	}
	if len(idList) > 0 {
		byIDs, err := listSessionsByIDs(ctx, idList, accountID)
		if err != nil {
			return nil, err
		}
		groups = append(groups, byIDs)
	}
	if clientToken != "" {
		byClient, err := listSessionsForClient(ctx, clientToken, limit)
		if err != nil {
			return nil, err
		}
		groups = append(groups, byClient)
	}
	return &ListSessionsResponse{Sessions: mergeSessionSummaries(limit, groups...)}, nil
}

//encore:api public method=POST path=/api/v1/codesim/sessions
func CreateSession(ctx context.Context, p *CreateSessionParams) (*CreateSessionResponse, error) {
	accountID := optionalAccountID(ctx)
	if p == nil {
		return nil, appErrs.BadRequest("body required")
	}
	if err := EnsureSeed(ctx); err != nil {
		return nil, err
	}

	var cfg BlueprintConfig
	var blueprintID string
	var questions []ExamQuestion

	switch {
	case p.ReuseSessionID != "":
		var err error
		questions, cfg, err = loadReusableAISession(ctx, p.ReuseSessionID, accountID)
		if err != nil {
			return nil, appErrs.BadRequest(err.Error())
		}
	case p.AiPlanID != "":
		planRow, err := loadAIPlan(ctx, p.AiPlanID, accountID)
		if err != nil {
			return nil, appErrs.BadRequest(err.Error())
		}
		questions, cfg, err = generateExamFromAIPlan(ctx, planRow)
		if err != nil {
			return nil, appErrs.BadRequest(err.Error())
		}
		deleteAIPlan(ctx, p.AiPlanID, accountID)
	case p.CustomConfig != nil:
		cfg = *p.CustomConfig
	default:
		var bp *Blueprint
		var err error
		if p.BlueprintID != "" {
			bp, err = loadBlueprintByID(ctx, p.BlueprintID)
		} else {
			slug := p.BlueprintSlug
			if slug == "" {
				slug = "frontend-standard-v1"
			}
			bp, err = loadBlueprintBySlug(ctx, slug)
		}
		if err != nil {
			return nil, err
		}
		cfg = bp.Config
		blueprintID = bp.ID
		if len(p.Topics) > 0 || p.Difficulty != "" || p.McqCount > 0 {
			mcqNeed := p.McqCount
			if mcqNeed <= 0 {
				mcqNeed = mcqCountFromConfig(cfg)
			}
			if err := validateLearnerSelection(ctx, p.Topics, p.Difficulty, mcqNeed); err != nil {
				return nil, appErrs.BadRequest(err.Error())
			}
			cfg = MergeLearnerMCQFilters(cfg, p.Topics, p.Difficulty, p.McqCount)
		}
	}

	seed := newRandomSeed()
	if p.Seed != nil {
		seed = *p.Seed
	}
	if questions == nil {
		exclude, err := loadExcludedSourceIDs(ctx, accountID, parseClientToken(p.ClientToken), "")
		if err != nil {
			return nil, err
		}
		questions, err = GenerateExamPaper(ctx, cfg, seed, exclude)
		if err != nil {
			return nil, err
		}
	}
	id, err := insertSession(ctx, accountID, parseClientToken(p.ClientToken), blueprintID, seed, questions, cfg)
	if err != nil {
		return nil, err
	}
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	s := rowToExamSession(row, true)
	if p.PresetID != "" && s.Selection != nil {
		s.Selection.PresetID = p.PresetID
	} else if s.Selection == nil && (len(p.Topics) > 0 || p.Difficulty != "" || p.McqCount > 0) {
		s.Selection = &SessionSelection{
			Topics:     p.Topics,
			Difficulty: p.Difficulty,
			McqCount:   p.McqCount,
			PresetID:   p.PresetID,
		}
	}
	return &CreateSessionResponse{Session: s}, nil
}

type ClaimSessionParams struct {
	ClientToken string `json:"clientToken"`
}

//encore:api public method=POST path=/api/v1/codesim/sessions/:id/claim
func ClaimSession(ctx context.Context, id string, p *ClaimSessionParams) (*OKResponse, error) {
	accountID := optionalAccountID(ctx)
	if p == nil || parseClientToken(p.ClientToken) == "" {
		return nil, appErrs.BadRequest("clientToken required")
	}
	if err := claimClientToken(ctx, id, accountID, parseClientToken(p.ClientToken)); err != nil {
		return nil, err
	}
	return &OKResponse{OK: true}, nil
}

//encore:api public method=POST path=/api/v1/codesim/sessions/:id/regenerate
func RegenerateSession(ctx context.Context, id string) (*CreateSessionResponse, error) {
	accountID := optionalAccountID(ctx)
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	if row.Status != SessionStatusSetup {
		return nil, appErrs.BadRequest("hanya bisa regenerate sebelum start")
	}
	cfg := row.ParsedConfig
	if cfg.TotalTimeLimitMinutes == 0 {
		cfg = DefaultBlueprintConfig()
	}
	seed := newRandomSeed()
	exclude, err := loadExcludedSourceIDs(ctx, accountID, row.ClientToken, id)
	if err != nil {
		return nil, err
	}
	questions, err := GenerateExamPaper(ctx, cfg, seed, exclude)
	if err != nil {
		return nil, err
	}
	if err := updateSessionQuestions(ctx, id, seed, questions); err != nil {
		return nil, err
	}
	row, err = loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	return &CreateSessionResponse{Session: rowToExamSession(row, true)}, nil
}

//encore:api public method=POST path=/api/v1/codesim/sessions/:id/start
func StartSession(ctx context.Context, id string) (*CreateSessionResponse, error) {
	accountID := optionalAccountID(ctx)
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	if row.Status != SessionStatusSetup {
		return nil, appErrs.BadRequest("session sudah dimulai")
	}
	mins := row.TotalMinutes
	if mins <= 0 {
		mins = 60
	}
	if err := startSession(ctx, id, mins); err != nil {
		return nil, err
	}
	row, err = loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	return &CreateSessionResponse{Session: rowToExamSession(row, true)}, nil
}

//encore:api public method=GET path=/api/v1/codesim/sessions/:id
func GetSession(ctx context.Context, id string) (*CreateSessionResponse, error) {
	accountID := optionalAccountID(ctx)
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	if row.Status == SessionStatusInProgress && row.ExpiresAt.Valid && time.Now().After(row.ExpiresAt.Time) {
		_, _ = db.Exec(ctx, `UPDATE codesim_exam_session SET status = $2 WHERE id = $1`, id, SessionStatusExpired)
		row.Status = SessionStatusExpired
	}
	return &CreateSessionResponse{Session: rowToExamSession(row, row.Status != SessionStatusSubmitted)}, nil
}

type SaveAnswersParams struct {
	Answers SessionAnswers `json:"answers"`
}

//encore:api public method=PUT path=/api/v1/codesim/sessions/:id/answers
func SaveAnswers(ctx context.Context, id string, p *SaveAnswersParams) (*CreateSessionResponse, error) {
	accountID := optionalAccountID(ctx)
	if p == nil {
		return nil, appErrs.BadRequest("body required")
	}
	if _, err := loadSessionForUser(ctx, id, accountID); err != nil {
		return nil, err
	}
	if err := saveAnswers(ctx, id, p.Answers); err != nil {
		return nil, err
	}
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	return &CreateSessionResponse{Session: rowToExamSession(row, true)}, nil
}

type ProctorEventsParams struct {
	Events []ProctorEventInput `json:"events"`
}

//encore:api public method=POST path=/api/v1/codesim/sessions/:id/proctor-events
func RecordProctorEvents(ctx context.Context, id string, p *ProctorEventsParams) (*OKResponse, error) {
	accountID := optionalAccountID(ctx)
	if _, err := loadSessionForUser(ctx, id, accountID); err != nil {
		return nil, err
	}
	if p != nil && len(p.Events) > 0 {
		if err := insertProctorEvents(ctx, id, p.Events); err != nil {
			return nil, err
		}
	}
	return &OKResponse{OK: true}, nil
}

type OKResponse struct {
	OK bool `json:"ok"`
}

//encore:api public method=POST path=/api/v1/codesim/sessions/:id/submit
func SubmitSession(ctx context.Context, id string) (*SessionReportResponse, error) {
	accountID := optionalAccountID(ctx)
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	if row.Status == SessionStatusSubmitted {
		report, err := gradeSession(ctx, row)
		if err != nil {
			return nil, err
		}
		return &SessionReportResponse{Report: *report}, nil
	}
	if row.Status != SessionStatusInProgress && row.Status != SessionStatusExpired {
		return nil, appErrs.BadRequest("session belum dimulai")
	}
	report, err := gradeSession(ctx, row)
	if err != nil {
		return nil, err
	}
	scoreJSON, _ := json.Marshal(report)
	if err := submitSession(ctx, id, scoreJSON); err != nil {
		return nil, err
	}
	return &SessionReportResponse{Report: *report}, nil
}

type SessionReportResponse struct {
	Report SessionReport `json:"report"`
}

//encore:api public method=GET path=/api/v1/codesim/sessions/:id/report
func GetSessionReport(ctx context.Context, id string) (*SessionReportResponse, error) {
	accountID := optionalAccountID(ctx)
	row, err := loadSessionForUser(ctx, id, accountID)
	if err != nil {
		return nil, err
	}
	if row.Status != SessionStatusSubmitted {
		return nil, appErrs.BadRequest("laporan tersedia setelah submit")
	}
	report, err := gradeSession(ctx, row)
	if err != nil {
		return nil, err
	}
	return &SessionReportResponse{Report: *report}, nil
}

type CustomBlueprintParams struct {
	Title  string          `json:"title"`
	Config BlueprintConfig `json:"config"`
}

type CustomBlueprintResponse struct {
	ID string `json:"id"`
}

//encore:api public method=POST path=/api/v1/codesim/custom-blueprint
func SaveCustomBlueprint(ctx context.Context, p *CustomBlueprintParams) (*CustomBlueprintResponse, error) {
	accountID := optionalAccountID(ctx)
	if p == nil || p.Title == "" {
		return nil, appErrs.BadRequest("title required")
	}
	cfg, err := json.Marshal(p.Config)
	if err != nil {
		return nil, err
	}
	id := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO codesim_custom_blueprint (id, account_id, title, config_json)
		VALUES ($1, $2, $3, $4)`, id, nullUUID(accountID), p.Title, cfg)
	if err != nil {
		return nil, err
	}
	return &CustomBlueprintResponse{ID: id}, nil
}
