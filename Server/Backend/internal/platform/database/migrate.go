package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	migrationLockName       = "xymusic.schema-migrations"
	migrationCleanupTimeout = 5 * time.Second
)

type Migration struct {
	Tag       string
	CreatedAt int64
	Hash      string
	SQL       []string
}

type AppliedMigration struct {
	Hash      string
	CreatedAt int64
}

type CompatibilityErrorKind string

const (
	CompatibilityNewerSchema    CompatibilityErrorKind = "NEWER_SCHEMA"
	CompatibilityHistoryForked  CompatibilityErrorKind = "HISTORY_FORKED"
	CompatibilityHashMismatch   CompatibilityErrorKind = "HASH_MISMATCH"
	CompatibilityHistoryInvalid CompatibilityErrorKind = "HISTORY_INVALID"
)

type CompatibilityError struct {
	Kind      CompatibilityErrorKind
	Message   string
	Migration int64
}

func (e *CompatibilityError) Error() string { return e.Message }

func IsPermanentMigrationError(err error) bool {
	var compatibility *CompatibilityError
	return errors.As(err, &compatibility)
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

	applied, err := readAppliedMigrations(ctx, connection)
	if err != nil {
		return err
	}
	if err := AssertCompatible(available, applied); err != nil {
		return err
	}
	if _, err := connection.Exec(ctx, "create schema if not exists drizzle"); err != nil {
		return fmt.Errorf("create migration schema: %w", err)
	}
	if _, err := connection.Exec(ctx, `
		create table if not exists drizzle.__drizzle_migrations (
			id serial primary key,
			hash text not null,
			created_at bigint
		)`); err != nil {
		return fmt.Errorf("create migration journal: %w", err)
	}

	for _, migration := range available[len(applied):] {
		if err := applyMigration(ctx, connection, migration); err != nil {
			return err
		}
	}
	return nil
}

// CheckMigrationCompatibility is read-only and is used before activating a
// candidate runtime or running production parity tests.
func CheckMigrationCompatibility(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	available, err := ReadMigrations(directory)
	if err != nil {
		return err
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration compatibility connection: %w", err)
	}
	defer connection.Release()
	applied, err := readAppliedMigrations(ctx, connection)
	if err != nil {
		return err
	}
	return AssertCompatible(available, applied)
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
		digest := sha256.Sum256(contents)
		migrations = append(migrations, Migration{
			Tag:       entry.Tag,
			CreatedAt: entry.When,
			Hash:      hex.EncodeToString(digest[:]),
			SQL:       strings.Split(string(contents), "--> statement-breakpoint"),
		})
	}
	return migrations, nil
}

func AssertCompatible(available []Migration, applied []AppliedMigration) error {
	if len(applied) > len(available) {
		return &CompatibilityError{
			Kind:    CompatibilityNewerSchema,
			Message: "The database schema was migrated by a newer XyMusic version",
		}
	}
	for index, actual := range applied {
		expected := available[index]
		if expected.CreatedAt != actual.CreatedAt {
			return &CompatibilityError{
				Kind:      CompatibilityHistoryForked,
				Message:   "The database migration history is not a prefix of this XyMusic release",
				Migration: actual.CreatedAt,
			}
		}
		if expected.Hash != actual.Hash {
			return &CompatibilityError{
				Kind:      CompatibilityHashMismatch,
				Message:   fmt.Sprintf("Database migration %d does not match this XyMusic release", actual.CreatedAt),
				Migration: actual.CreatedAt,
			}
		}
	}
	return nil
}

func readAppliedMigrations(ctx context.Context, connection *pgxpool.Conn) ([]AppliedMigration, error) {
	var relation *string
	if err := connection.QueryRow(ctx, "select to_regclass('drizzle.__drizzle_migrations')::text").Scan(&relation); err != nil {
		return nil, fmt.Errorf("inspect migration journal: %w", err)
	}
	if relation == nil {
		return []AppliedMigration{}, nil
	}
	rows, err := connection.Query(ctx, `
		select hash, created_at
		from drizzle.__drizzle_migrations
		order by created_at asc, id asc`)
	if err != nil {
		if postgresErrorCode(err) == "42703" {
			return nil, &CompatibilityError{Kind: CompatibilityHistoryInvalid, Message: "The database migration history is invalid"}
		}
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	defer rows.Close()
	result := make([]AppliedMigration, 0)
	for rows.Next() {
		var migration AppliedMigration
		if err := rows.Scan(&migration.Hash, &migration.CreatedAt); err != nil {
			return nil, &CompatibilityError{Kind: CompatibilityHistoryInvalid, Message: "The database migration history is invalid"}
		}
		if migration.Hash == "" || migration.CreatedAt < 1 {
			return nil, &CompatibilityError{Kind: CompatibilityHistoryInvalid, Message: "The database migration history is invalid"}
		}
		result = append(result, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
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
	for _, statement := range migration.SQL {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Tag, err)
		}
	}
	if _, err := tx.Exec(ctx,
		"insert into drizzle.__drizzle_migrations (hash, created_at) values ($1, $2)",
		migration.Hash, migration.CreatedAt,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Tag, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.Tag, err)
	}
	return nil
}

func postgresErrorCode(err error) string {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return pgError.Code
	}
	return ""
}
