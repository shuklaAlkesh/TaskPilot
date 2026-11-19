// Package store owns the PostgreSQL connection pool, transactions, and migrations.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/shuklaAlkesh/taskpilot/backend/server/migrations"
)

// Open creates a pool and verifies the database is reachable.
func Open(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("parse database url: invalid format") // the URL may hold a password
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// InTx runs fn in a READ COMMITTED transaction. It commits when fn returns nil and rolls back otherwise.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, fn)
}

// Migrate applies every pending embedded migration.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]*goose.MigrationResult, error) {
	p, db, err := provider(pool)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return p.Up(ctx)
}

// MigrationStatus reports the applied and embedded schema versions.
func MigrationStatus(ctx context.Context, pool *pgxpool.Pool) (current, target int64, err error) {
	p, db, err := provider(pool)
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()
	return p.GetVersions(ctx)
}

// CheckSchema fails when the database version differs from the embedded latest migration.
// The API refuses to start on a mismatch (docs/04-DATA-MODEL.md §8).
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	current, target, err := MigrationStatus(ctx, pool)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current != target {
		return fmt.Errorf("schema version %d, binary expects %d: run taskpilot-admin migrate up", current, target)
	}
	return nil
}

func provider(pool *pgxpool.Pool) (*goose.Provider, *sql.DB, error) {
	db := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		_ = db.Close() // provider construction failed; close error adds nothing
		return nil, nil, fmt.Errorf("create migration provider: %w", err)
	}
	return p, db, nil
}
