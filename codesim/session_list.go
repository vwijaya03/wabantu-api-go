package codesim

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const defaultSessionListLimit = 30
const maxSessionListLimit = 50

func listSessionsForUser(ctx context.Context, accountID string, limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = defaultSessionListLimit
	}
	if limit > maxSessionListLimit {
		limit = maxSessionListLimit
	}

	rows, err := db.Query(ctx, `
		SELECT s.id::text, s.status, s.created_at, s.updated_at, s.submitted_at,
		       s.questions_json, COALESCE(s.config_json, b.config_json, '{}'), COALESCE(s.score_json, 'null')
		FROM codesim_exam_session s
		LEFT JOIN codesim_blueprint b ON b.id = s.blueprint_id
		WHERE s.account_id = $1
		ORDER BY s.updated_at DESC
		LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		sum, _, err := scanSessionSummaryRow(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

func listSessionsByIDs(ctx context.Context, ids []string, callerAccountID string) ([]SessionSummary, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := db.Query(ctx, `
		SELECT s.id::text, s.account_id::text, s.status, s.created_at, s.updated_at, s.submitted_at,
		       s.questions_json, COALESCE(s.config_json, b.config_json, '{}'), COALESCE(s.score_json, 'null')
		FROM codesim_exam_session s
		LEFT JOIN codesim_blueprint b ON b.id = s.blueprint_id
		WHERE s.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		sum, accountID, err := scanSessionSummaryRow(rows, true)
		if err != nil {
			return nil, err
		}
		if accountID != "" {
			if callerAccountID == "" || callerAccountID != accountID {
				continue
			}
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

func mergeSessionSummaries(limit int, groups ...[]SessionSummary) []SessionSummary {
	if limit <= 0 {
		limit = defaultSessionListLimit
	}
	if limit > maxSessionListLimit {
		limit = maxSessionListLimit
	}
	merged := make(map[string]SessionSummary)
	for _, group := range groups {
		for _, s := range group {
			merged[s.ID] = s
		}
	}
	out := make([]SessionSummary, 0, len(merged))
	for _, s := range merged {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func parseSessionIDList(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		if _, err := uuid.Parse(p); err != nil {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxSessionListLimit {
			break
		}
	}
	return out
}

type sessionSummaryScanner interface {
	Scan(dest ...any) error
}

func scanSessionSummaryRow(rows sessionSummaryScanner, withAccount bool) (SessionSummary, string, error) {
	var sum SessionSummary
	var qJSON, cfgJSON, scoreJSON []byte
	var submittedAt sql.NullTime
	var accountID sql.NullString

	var err error
	if withAccount {
		err = rows.Scan(&sum.ID, &accountID, &sum.Status, &sum.CreatedAt, &sum.UpdatedAt, &submittedAt,
			&qJSON, &cfgJSON, &scoreJSON)
	} else {
		err = rows.Scan(&sum.ID, &sum.Status, &sum.CreatedAt, &sum.UpdatedAt, &submittedAt,
			&qJSON, &cfgJSON, &scoreJSON)
	}
	if err != nil {
		return SessionSummary{}, "", err
	}
	if submittedAt.Valid {
		t := submittedAt.Time
		sum.SubmittedAt = &t
	}

	var questions []ExamQuestion
	_ = json.Unmarshal(qJSON, &questions)
	sum.QuestionCount = len(questions)
	sum.Source = inferSessionSource(questions)

	var cfg BlueprintConfig
	if err := json.Unmarshal(cfgJSON, &cfg); err == nil {
		sum.Selection = selectionFromConfig(cfg)
	}
	sum.Label = sessionSummaryLabel(sum.Source, sum.Selection)

	if len(scoreJSON) > 0 && string(scoreJSON) != "null" {
		var score struct {
			Grade           string `json:"grade"`
			NormalizedScore int    `json:"normalizedScore"`
		}
		if err := json.Unmarshal(scoreJSON, &score); err == nil {
			sum.Grade = score.Grade
			sum.NormalizedScore = score.NormalizedScore
		}
	}

	acct := ""
	if accountID.Valid {
		acct = accountID.String
	}
	return sum, acct, nil
}

func parseClientToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := uuid.Parse(raw); err != nil {
		return ""
	}
	return raw
}

func listSessionsForClient(ctx context.Context, clientToken string, limit int) ([]SessionSummary, error) {
	if clientToken == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultSessionListLimit
	}
	if limit > maxSessionListLimit {
		limit = maxSessionListLimit
	}
	rows, err := db.Query(ctx, `
		SELECT s.id::text, s.status, s.created_at, s.updated_at, s.submitted_at,
		       s.questions_json, COALESCE(s.config_json, b.config_json, '{}'), COALESCE(s.score_json, 'null')
		FROM codesim_exam_session s
		LEFT JOIN codesim_blueprint b ON b.id = s.blueprint_id
		WHERE s.client_token = $1
		ORDER BY s.updated_at DESC
		LIMIT $2`, clientToken, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		sum, _, err := scanSessionSummaryRow(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

func inferSessionSource(questions []ExamQuestion) string {
	for _, q := range questions {
		if strings.HasPrefix(q.SourceID, "ai-") {
			return "ai"
		}
	}
	return "bank"
}

func sessionSummaryLabel(source string, sel *SessionSelection) string {
	if source == "ai" {
		return "Ujian AI"
	}
	if sel != nil && len(sel.Topics) > 0 {
		return strings.Join(sel.Topics, ", ")
	}
	return "Bank soal"
}
