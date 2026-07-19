package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryUnlockTimeout = 5 * time.Second

type PostgresDatabase struct {
	pool PostgresPool
}

type PostgresPool interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

func NewPostgresDatabase(pool PostgresPool) (*PostgresDatabase, error) {
	if pool == nil {
		return nil, errors.New("retention PostgreSQL pool is required")
	}
	return &PostgresDatabase{pool: pool}, nil
}

func (database *PostgresDatabase) WithAdvisoryLock(
	ctx context.Context,
	name string,
	operation func(Executor) error,
) error {
	connection, err := database.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire retention advisory lock connection: %w", err)
	}
	locked := false
	defer func() {
		if !locked {
			connection.Release()
			return
		}
		unlockContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), advisoryUnlockTimeout)
		_, unlockErr := connection.Exec(
			unlockContext,
			"SELECT pg_advisory_unlock(hashtextextended($1, 0))",
			name,
		)
		cancel()
		if unlockErr == nil {
			connection.Release()
			return
		}

		// Never return a pooled session that may still hold the advisory lock.
		rawConnection := connection.Hijack()
		closeContext, closeCancel := context.WithTimeout(context.Background(), advisoryUnlockTimeout)
		_ = rawConnection.Close(closeContext)
		closeCancel()
	}()

	if _, err := connection.Exec(
		ctx,
		"SELECT pg_advisory_lock(hashtextextended($1, 0))",
		name,
	); err != nil {
		return fmt.Errorf("acquire retention advisory lock: %w", err)
	}
	locked = true
	if err := operation(postgresExecutor{connection: connection}); err != nil {
		return err
	}
	return nil
}

type postgresExecutor struct {
	connection *pgxpool.Conn
}

func (executor postgresExecutor) Execute(
	ctx context.Context,
	statement string,
	arguments ...any,
) (int64, error) {
	tag, err := executor.connection.Exec(ctx, statement, arguments...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
