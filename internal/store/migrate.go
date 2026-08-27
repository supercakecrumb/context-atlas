package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/supercakecrumb/context-atlas/migrations"
)

// Goose stores its embedded filesystem globally, so migrations must not run in
// parallel inside one process.
var gooseMu sync.Mutex

// Migrate applies the embedded, forward-only pre-release migration.
func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("store is not initialized")
	}

	gooseMu.Lock()
	defer gooseMu.Unlock()

	db := stdlib.OpenDBFromPool(s.pool)
	defer func() { _ = db.Close() }()
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("configure Goose PostgreSQL dialect: %w", err)
	}
	goose.SetBaseFS(migrations.FS)
	defer goose.SetBaseFS(nil)
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
