package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	migrationLockName       = "xymusic.schema-migrations"
	migrationCleanupTimeout = 5 * time.Second
)

type Migration struct {
	Tag       string
	CreatedAt int64
	SQL       []string
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	available, err := ReadMigrations(directory)
	if err != nil {
		return err
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, "select pg_advisory_lock(hashtextextended($1, 0))", migrationLockName); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), migrationCleanupTimeout)
		defer cancel()
		_, _ = connection.Exec(cleanupContext, "select pg_advisory_unlock(hashtextextended($1, 0))", migrationLockName)
	}()

	applied, err := readAppliedMigrationTimes(ctx, connection)
	if err != nil {
		return err
	}
	if _, err := connection.Exec(ctx, "create schema if not exists drizzle"); err != nil {
		return fmt.Errorf("create migration schema: %w", err)
	}
	if _, err := connection.Exec(ctx, `
		create table if not exists drizzle.__drizzle_migrations (
			id serial primary key,
			created_at bigint
		)`); err != nil {
		return fmt.Errorf("create migration journal: %w", err)
	}
	// Drop the legacy journal column left by older releases.
	if _, err := connection.Exec(ctx, "alter table drizzle.__drizzle_migrations drop column if exists hash"); err != nil {
		return fmt.Errorf("remove legacy migration metadata: %w", err)
	}

	for _, migration := range available {
		if _, exists := applied[migration.CreatedAt]; exists {
			continue
		}
		if err := applyMigration(ctx, connection, migration); err != nil {
			// A failed first migration leaves only the journal relation because
			// the migration itself is transactional. Remove that empty marker so
			// a fresh installation remains retryable without manual journal edits.
			if len(applied) == 0 {
				cleanupEmptyMigrationJournal(ctx, connection)
			}
			return err
		}
		applied[migration.CreatedAt] = struct{}{}
	}
	return nil
}

func cleanupEmptyMigrationJournal(ctx context.Context, connection *pgxpool.Conn) {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), migrationCleanupTimeout)
	defer cancel()
	var hasRows bool
	if err := connection.QueryRow(cleanupContext,
		"SELECT EXISTS (SELECT 1 FROM drizzle.__drizzle_migrations)",
	).Scan(&hasRows); err != nil || hasRows {
		return
	}
	// Do not use CASCADE: if an operator has placed another object in the
	// application-owned schema, leaving that schema is safer than dropping it.
	_, _ = connection.Exec(cleanupContext, "DROP TABLE IF EXISTS drizzle.__drizzle_migrations")
	_, _ = connection.Exec(cleanupContext, "DROP SCHEMA IF EXISTS drizzle")
}

func ReadMigrations(directory string) ([]Migration, error) {
	journalBytes, err := os.ReadFile(filepath.Join(directory, "meta", "_journal.json"))
	if err != nil {
		return nil, fmt.Errorf("read migration journal: %w", err)
	}
	var journal struct {
		Entries []struct {
			Tag  string `json:"tag"`
			When int64  `json:"when"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(journalBytes, &journal); err != nil {
		return nil, fmt.Errorf("parse migration journal: %w", err)
	}
	if len(journal.Entries) == 0 {
		return nil, errors.New("database migrations directory does not contain any migrations")
	}

	migrations := make([]Migration, 0, len(journal.Entries))
	for _, entry := range journal.Entries {
		if entry.Tag == "" || entry.When < 1 {
			return nil, errors.New("migration journal contains an invalid entry")
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Tag+".sql"))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Tag, err)
		}
		migrations = append(migrations, Migration{
			Tag:       entry.Tag,
			CreatedAt: entry.When,
			SQL:       strings.Split(string(contents), "--> statement-breakpoint"),
		})
	}
	return migrations, nil
}

func readAppliedMigrationTimes(ctx context.Context, connection *pgxpool.Conn) (map[int64]struct{}, error) {
	var relation *string
	if err := connection.QueryRow(ctx, "select to_regclass('drizzle.__drizzle_migrations')::text").Scan(&relation); err != nil {
		return nil, fmt.Errorf("inspect migration journal: %w", err)
	}
	if relation == nil {
		return map[int64]struct{}{}, nil
	}
	rows, err := connection.Query(ctx, "select created_at from drizzle.__drizzle_migrations where created_at is not null")
	if err != nil {
		return nil, fmt.Errorf("read migration journal: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]struct{})
	for rows.Next() {
		var createdAt int64
		if err := rows.Scan(&createdAt); err != nil {
			return nil, fmt.Errorf("read migration journal entry: %w", err)
		}
		result[createdAt] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration journal: %w", err)
	}
	return result, nil
}

func applyMigration(ctx context.Context, connection *pgxpool.Conn, migration Migration) error {
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.Tag, err)
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), migrationCleanupTimeout)
		defer cancel()
		_ = tx.Rollback(cleanupContext)
	}()
	for index, statement := range migration.SQL {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply migration %s statement %d: %w", migration.Tag, index, err)
		}
	}
	if _, err := tx.Exec(ctx,
		"insert into drizzle.__drizzle_migrations (created_at) values ($1)",
		migration.CreatedAt,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Tag, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.Tag, err)
	}
	return nil
}
