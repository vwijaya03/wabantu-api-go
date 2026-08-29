package codesim

import (
	"context"
	"encoding/json"
)

const maxHistorySessionsForExclude = 200

// loadExcludedSourceIDs returns bank source IDs already used in prior exam sessions
// for the same learner (account and/or client token).
func loadExcludedSourceIDs(ctx context.Context, accountID, clientToken, skipSessionID string) (map[string]bool, error) {
	if accountID == "" && clientToken == "" {
		return nil, nil
	}
	rows, err := db.Query(ctx, `
		SELECT questions_json
		FROM codesim_exam_session
		WHERE ($4 = '' OR id::text <> $4)
		  AND (
		    ($1::uuid IS NOT NULL AND account_id = $1)
		    OR ($2 <> '' AND client_token = $2::uuid)
		  )
		ORDER BY updated_at DESC
		LIMIT $3`,
		nullUUID(accountID), clientToken, maxHistorySessionsForExclude, skipSessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var questions []ExamQuestion
		if err := json.Unmarshal(raw, &questions); err != nil {
			continue
		}
		for _, q := range questions {
			if q.SourceID != "" {
				out[q.SourceID] = true
			}
		}
	}
	return out, rows.Err()
}

func filterExcludedMCQs(pool []MCQItem, exclude map[string]bool) []MCQItem {
	if len(exclude) == 0 {
		return pool
	}
	filtered := make([]MCQItem, 0, len(pool))
	for _, m := range pool {
		if !exclude[m.ID] {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return pool
	}
	return filtered
}

func filterExcludedBuilds(pool []BuildTask, exclude map[string]bool) []BuildTask {
	if len(exclude) == 0 {
		return pool
	}
	filtered := make([]BuildTask, 0, len(pool))
	for _, t := range pool {
		if !exclude[t.ID] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return pool
	}
	return filtered
}

func filterExcludedDebugs(pool []DebugTask, exclude map[string]bool) []DebugTask {
	if len(exclude) == 0 {
		return pool
	}
	filtered := make([]DebugTask, 0, len(pool))
	for _, t := range pool {
		if !exclude[t.ID] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return pool
	}
	return filtered
}
