package events

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"encore.dev/storage/sqldb"

	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/tenant"
)

var eventsDB = sqldb.Named("tenant")

// tenantEvtTables longest-first so shorter names are not partially matched.
var tenantEvtTables = []string{
	"evt_event_therapy_slot_template",
	"evt_staff_roster_volunteer",
	"evt_staff_roster_therapy",
	"evt_event_assignment",
	"evt_event_volunteer",
	"evt_event_therapy",
	"evt_event_person",
	"evt_person_therapy",
	"evt_volunteer_role",
	"evt_staff_roster",
	"evt_export_job",
	"evt_audit_log",
	"evt_time_slot",
	"evt_patient",
	"evt_therapy",
	"evt_event",
	"evt_task",
	"contact",
}

var evtTableREs = make([]*regexp.Regexp, len(tenantEvtTables))

func init() {
	for i, table := range tenantEvtTables {
		evtTableREs[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(table) + `\b`)
	}
}

func qualEvtSQL(sch appdb.SchemaSQL, sql string) string {
	if strings.Contains(sql, sch.Schema) {
		return sql
	}
	out := sql
	for i, table := range tenantEvtTables {
		out = evtTableREs[i].ReplaceAllString(out, sch.T(table))
	}
	return out
}

// evtQuerier is satisfied by *sql.DB and *sql.Tx.
type evtQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type tenantScope struct {
	q   evtQuerier
	sch appdb.SchemaSQL
}

func (ts tenantScope) T(table string) string {
	return ts.sch.T(table)
}

func (ts tenantScope) WithQ(q evtQuerier) tenantScope {
	return tenantScope{q: q, sch: ts.sch}
}

func (ts tenantScope) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return ts.q.ExecContext(ctx, qualEvtSQL(ts.sch, query), args...)
}

func (ts tenantScope) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return ts.q.QueryContext(ctx, qualEvtSQL(ts.sch, query), args...)
}

func (ts tenantScope) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return ts.q.QueryRowContext(ctx, qualEvtSQL(ts.sch, query), args...)
}

func (ts tenantScope) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	beginner, ok := ts.q.(interface {
		BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return nil, fmt.Errorf("events: cannot begin transaction on this querier")
	}
	return beginner.BeginTx(ctx, opts)
}

func openTenant(ctx context.Context, schema string) (tenantScope, error) {
	if err := tenant.PrepareTenantAccess(ctx, schema); err != nil {
		return tenantScope{}, err
	}
	if err := ensureEventsSchema(ctx, schema); err != nil {
		return tenantScope{}, err
	}
	return tenantScope{
		q:   eventsDB.Stdlib(),
		sch: appdb.SchemaSQL{Schema: schema},
	}, nil
}
