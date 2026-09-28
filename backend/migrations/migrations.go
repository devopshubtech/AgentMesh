// Package migrations embeds the SQL schema migrations and applies them with goose.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql
var files embed.FS

// Up applies all pending migrations. A Postgres advisory lock serializes
// concurrent callers, so it is safe to run from several replicas at once.
func Up(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := provider(db)
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		log.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	return nil
}

// Status reports the current and latest available schema versions.
func Status(ctx context.Context, pool *pgxpool.Pool) (current, latest int64, err error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := provider(db)
	if err != nil {
		return 0, 0, err
	}
	current, err = p.GetDBVersion(ctx)
	if err != nil {
		return 0, 0, err
	}
	srcs := p.ListSources()
	if len(srcs) > 0 {
		latest = srcs[len(srcs)-1].Version
	}
	return current, latest, nil
}

func provider(db *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
}
