package chatengine

import (
	"context"
	"database/sql"

	appdb "encore.app/wabantu/shared/db"
	bf "encore.app/wabantu/internal/buyerflow"
)

type kbRow struct {
	ID       string
	Question string
	Answer   string
}

func loadKB(ctx context.Context, ts appdb.TenantScope, limit int) ([]bf.KBEntry, error) {
	rows, err := ts.QueryContext(ctx, `
		SELECT id::text, question, answer
		FROM `+ts.T("knowledge_base_entry")+`
		WHERE is_active = true AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bf.KBEntry
	for rows.Next() {
		var r kbRow
		if err := rows.Scan(&r.ID, &r.Question, &r.Answer); err != nil {
			return nil, err
		}
		out = append(out, bf.KBEntry{ID: r.ID, Question: r.Question, Answer: r.Answer})
	}
	return out, rows.Err()
}

func loadBusinessName(ctx context.Context, ts appdb.TenantScope) (string, error) {
	var name sql.NullString
	err := ts.QueryRowContext(ctx, `
		SELECT business_name FROM `+ts.T("business_profile")+` LIMIT 1`).Scan(&name)
	if err == sql.ErrNoRows || !name.Valid {
		return "toko kami", nil
	}
	if err != nil {
		return "", err
	}
	return name.String, nil
}

func bestFAQReply(query string, kb []bf.KBEntry) (string, bool) {
	if len(kb) == 0 {
		return "", false
	}
	bestScore := 0.0
	var best string
	for _, e := range kb {
		score := bf.TopKBMatchScore(query, []bf.KBEntry{e})
		if score > bestScore {
			bestScore = score
			best = e.Answer
		}
	}
	if bestScore < 0.35 {
		return "", false
	}
	return best, true
}
