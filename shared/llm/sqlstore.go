package llm

import (
	"context"
	"database/sql"
	"errors"
)

// Querier is satisfied by *sql.DB (e.g. encore sqldb.Database.Stdlib()).
type Querier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// SQLStore reads ai_providers / ai_function_config from the system DB.
type SQLStore struct{ DB Querier }

// NewSQLStore returns nil (→ code defaults) when db is nil.
func NewSQLStore(db Querier) ConfigStore {
	if db == nil {
		return nil
	}
	return SQLStore{DB: db}
}

func (s SQLStore) LoadFunctionConfig(ctx context.Context, fn Function, tenantID string) (FunctionConfig, bool, error) {
	var (
		raw     []byte
		retries int
		row     *sql.Row
	)
	if tenantID == "" {
		row = s.DB.QueryRowContext(ctx, `SELECT chain, max_retries FROM ai_function_config WHERE function=$1 AND tenant_id IS NULL`, string(fn))
	} else {
		row = s.DB.QueryRowContext(ctx, `SELECT chain, max_retries FROM ai_function_config WHERE function=$1 AND tenant_id=$2`, string(fn), tenantID)
	}
	if err := row.Scan(&raw, &retries); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FunctionConfig{}, false, nil
		}
		return FunctionConfig{}, false, err
	}
	chain, err := ParseChain(raw)
	if err != nil {
		return FunctionConfig{}, false, err
	}
	return FunctionConfig{Function: fn, TenantID: tenantID, Chain: chain, MaxRetries: retries}, true, nil
}

func (s SQLStore) EnabledProviders(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, enabled FROM ai_providers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	n := 0
	for rows.Next() {
		var id string
		var en bool
		if err := rows.Scan(&id, &en); err != nil {
			return nil, err
		}
		out[id] = en
		n++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil // empty table → don't filter
	}
	return out, nil
}
