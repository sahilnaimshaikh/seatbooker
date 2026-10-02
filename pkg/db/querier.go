package db

import (
	"context"
	"database/sql"
)

// Querier is satisfied by both *sql.DB and *sql.Tx. Each table type (e.g.
// ShowTable, SeatTable) is constructed with a Querier, so the same table
// methods can run standalone (handed a *sql.DB) or as one statement inside
// a caller's transaction (handed a *sql.Tx) — the db layer never opens or
// commits a transaction itself; that's the service layer's responsibility.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var (
	_ Querier = (*sql.DB)(nil)
	_ Querier = (*sql.Tx)(nil)
)
