package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/config"
)

type Pool struct {
	*pgxpool.Pool
}

func Open(ctx context.Context, cfg config.Database) (*Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	poolConfig.MaxConns = cfg.MaxConnections
	poolConfig.MaxConnIdleTime = 20 * time.Second
	poolConfig.ConnConfig.ConnectTimeout = 10 * time.Second

	connection, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	pool := &Pool{Pool: connection}
	if err := pool.Ping(ctx); err != nil {
		connection.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}
