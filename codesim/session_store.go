package codesim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"

	appErrs "encore.app/wabantu/shared/errs"
)

func loadBlueprintByID(ctx context.Context, id string) (*Blueprint, error) {
	if err := EnsureSeed(ctx); err != nil {
		return nil, err
	}
	var b Blueprint
	var cfg json.RawMessage
	err := db.QueryRow(ctx, `
		SELECT id::text, slug, title, config_json, is_public
		FROM codesim_blueprint WHERE id = $1`, id).Scan(&b.ID, &b.Slug, &b.Title, &cfg, &b.IsPublic)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("blueprint tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(cfg, &b.Config); err != nil {
		return nil, err
	}
	return &b, nil
}

func loadBlueprintBySlug(ctx context.Context, slug string) (*Blueprint, error) {
	if err := EnsureSeed(ctx); err != nil {
		return nil, err
	}
	var b Blueprint
	var cfg json.RawMessage
	err := db.QueryRow(ctx, `
		SELECT id::text, slug, title, config_json, is_public
		FROM codesim_blueprint WHERE slug = $1`, slug).Scan(&b.ID, &b.Slug, &b.Title, &cfg, &b.IsPublic)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("blueprint tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(cfg, &b.Config); err != nil {
		return nil, err
	}
	return &b, nil
}

func newRandomSeed() int64 {
	return rand.New(rand.NewSource(time.Now().UnixNano())).Int63()
}

func insertSession(ctx context.Context, accountID, clientToken, blueprintID string, seed int64, questions []ExamQuestion, cfg BlueprintConfig) (string, error) {
	id := uuid.New()
	qJSON, err := json.Marshal(questions)
	if err != nil {
		return "", err
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	ans, _ := json.Marshal(SessionAnswers{MCQ: map[string]string{}, Code: map[string]CodeAnswer{}})
	_, err = db.Exec(ctx, `
		INSERT INTO codesim_exam_session (
			id, account_id, client_token, blueprint_id, seed, status, questions_json, answers_json, config_json, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())`,
		id, nullUUID(accountID), nullUUID(clientToken), nullUUID(blueprintID), seed, SessionStatusSetup, qJSON, ans, cfgJSON,
	)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func nullUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func claimClientToken(ctx context.Context, sessionID, accountID, clientToken string) error {
	if clientToken == "" {
		return nil
	}
	if _, err := loadSession(ctx, sessionID, accountID); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE codesim_exam_session
		SET client_token = $2, updated_at = now()
		WHERE id = $1
		  AND client_token IS NULL
		  AND (account_id IS NULL OR account_id::text = $3)`,
		sessionID, clientToken, nullUUID(accountID),
	)
	return err
}

func loadSessionForUser(ctx context.Context, sessionID, accountID string) (*examSessionRow, error) {
	return loadSession(ctx, sessionID, accountID)
}

func loadSession(ctx context.Context, sessionID, callerAccountID string) (*examSessionRow, error) {
	var row examSessionRow
	var qJSON, aJSON, scoreJSON []byte
	var accountID, blueprintID, clientToken sql.NullString
	err := db.QueryRow(ctx, `
		SELECT s.id::text, s.account_id::text, s.client_token::text, s.blueprint_id::text, s.seed, s.status,
		       s.questions_json, s.answers_json, s.started_at, s.expires_at, s.submitted_at,
		       s.score_json, COALESCE(s.config_json, b.config_json, '{}')
		FROM codesim_exam_session s
		LEFT JOIN codesim_blueprint b ON b.id = s.blueprint_id
		WHERE s.id = $1`,
		sessionID,
	).Scan(&row.ID, &accountID, &clientToken, &blueprintID, &row.Seed, &row.Status,
		&qJSON, &aJSON, &row.StartedAt, &row.ExpiresAt, &row.SubmittedAt, &scoreJSON, &row.BlueprintConfig)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appErrs.NotFound("session tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if accountID.Valid {
		row.AccountID = accountID.String
	}
	if clientToken.Valid {
		row.ClientToken = clientToken.String
	}
	if blueprintID.Valid {
		row.BlueprintID = blueprintID.String
	}
	if row.AccountID != "" {
		if callerAccountID == "" || callerAccountID != row.AccountID {
			return nil, appErrs.NotFound("session tidak ditemukan")
		}
	}
	_ = json.Unmarshal(qJSON, &row.Questions)
	_ = json.Unmarshal(aJSON, &row.Answers)
	if len(scoreJSON) > 0 {
		_ = json.Unmarshal(scoreJSON, &row.ScoreJSON)
	}
	var cfg BlueprintConfig
	if err := json.Unmarshal(row.BlueprintConfig, &cfg); err == nil && cfg.TotalTimeLimitMinutes > 0 {
		row.ParsedConfig = cfg
		row.TotalMinutes = cfg.TotalTimeLimitMinutes
	} else {
		row.TotalMinutes = 60
	}
	return &row, nil
}

type examSessionRow struct {
	ID              string
	AccountID       string
	ClientToken     string
	BlueprintID     string
	Seed            int64
	Status          string
	Questions       []ExamQuestion
	Answers         SessionAnswers
	StartedAt       sql.NullTime
	ExpiresAt       sql.NullTime
	SubmittedAt     sql.NullTime
	ScoreJSON       json.RawMessage
	BlueprintConfig json.RawMessage
	ParsedConfig    BlueprintConfig
	TotalMinutes    int
}

func rowToExamSession(row *examSessionRow, stripGrading bool) ExamSession {
	qs := row.Questions
	if stripGrading {
		for i := range qs {
			qs[i].GradingMeta = nil
		}
	}
	s := ExamSession{
		ID:           row.ID,
		BlueprintID:  row.BlueprintID,
		Seed:         row.Seed,
		Status:       row.Status,
		Questions:    qs,
		TotalMinutes: row.TotalMinutes,
		Selection:    selectionFromConfig(row.ParsedConfig),
	}
	if !stripGrading || row.Status == SessionStatusSubmitted {
		s.Answers = row.Answers
	}
	if row.StartedAt.Valid {
		t := row.StartedAt.Time
		s.StartedAt = &t
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		s.ExpiresAt = &t
	}
	if row.SubmittedAt.Valid {
		t := row.SubmittedAt.Time
		s.SubmittedAt = &t
	}
	return s
}

func updateSessionQuestions(ctx context.Context, sessionID string, seed int64, questions []ExamQuestion) error {
	qJSON, err := json.Marshal(questions)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		UPDATE codesim_exam_session SET seed = $2, questions_json = $3, updated_at = now()
		WHERE id = $1 AND status = $4`, sessionID, seed, qJSON, SessionStatusSetup)
	return err
}

func startSession(ctx context.Context, sessionID string, minutes int) error {
	exp := time.Now().Add(time.Duration(minutes) * time.Minute)
	_, err := db.Exec(ctx, `
		UPDATE codesim_exam_session
		SET status = $2, started_at = now(), expires_at = $3, updated_at = now()
		WHERE id = $1 AND status = $4`,
		sessionID, SessionStatusInProgress, exp, SessionStatusSetup)
	return err
}

func saveAnswers(ctx context.Context, sessionID string, answers SessionAnswers) error {
	aJSON, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	res, err := db.Exec(ctx, `
		UPDATE codesim_exam_session SET answers_json = $2, updated_at = now()
		WHERE id = $1 AND status = $3`, sessionID, aJSON, SessionStatusInProgress)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return appErrs.BadRequest("session tidak aktif")
	}
	return nil
}

func submitSession(ctx context.Context, sessionID string, score json.RawMessage) error {
	_, err := db.Exec(ctx, `
		UPDATE codesim_exam_session
		SET status = $2, submitted_at = now(), score_json = $3, updated_at = now()
		WHERE id = $1 AND status IN ($4, $5)`,
		sessionID, SessionStatusSubmitted, score, SessionStatusInProgress, SessionStatusExpired)
	return err
}

func insertProctorEvents(ctx context.Context, sessionID string, events []ProctorEventInput) error {
	for _, e := range events {
		meta := e.Metadata
		if meta == nil {
			meta = json.RawMessage(`{}`)
		}
		_, err := db.Exec(ctx, `
			INSERT INTO codesim_proctor_event (id, session_id, event_type, metadata)
			VALUES ($1, $2, $3, $4)`,
			uuid.New(), sessionID, e.EventType, meta)
		if err != nil {
			return err
		}
	}
	return nil
}

func countProctorEvents(ctx context.Context, sessionID string) (ProctorSummary, error) {
	var blur, paste int
	err := db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE event_type IN ('blur','visibility_hidden','tab_switch')),
			COUNT(*) FILTER (WHERE event_type = 'paste')
		FROM codesim_proctor_event WHERE session_id = $1`, sessionID).Scan(&blur, &paste)
	return ProctorSummary{BlurEvents: blur, PasteEvents: paste}, err
}

type ProctorEventInput struct {
	EventType string          `json:"eventType"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

func mcqKey(q ExamQuestion) string {
	return fmt.Sprintf("%d", q.Index)
}

func codeKey(q ExamQuestion) string {
	return fmt.Sprintf("%d", q.Index)
}
