package factory

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/paytm-hack/seatbooking/pkg/config"
)

type Factory struct {
	db *sql.DB
}

// New opens the Postgres connection pool. sql.Open does not dial the
// database — it only validates the DSN and prepares the pool — so this
// returns immediately even if Postgres is still starting up elsewhere.
// Actual reachability is checked continuously by the readiness handler,
// not here, so a slow-to-wake dependency never blocks the server from
// binding its port.
func New(cfg config.Getter) (*Factory, error) {
	db, err := sql.Open("pgx", cfg.GetDatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("factory: failed to open db pool: %w", err)
	}

	db.SetMaxOpenConns(cfg.GetDBMaxOpenConns())
	db.SetMaxIdleConns(cfg.GetDBMaxIdleConns())

	return &Factory{db: db}, nil
}

func (f *Factory) DB() *sql.DB {
	return f.db
}

func (f *Factory) Close() error {
	return f.db.Close()
}
