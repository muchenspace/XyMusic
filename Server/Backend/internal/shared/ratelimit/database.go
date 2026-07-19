package ratelimit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

type Limiter interface {
	Consume(ctx context.Context, key string, maximum int, window time.Duration) error
}

type DatabaseLimiter struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewDatabaseLimiter(pool *pgxpool.Pool) *DatabaseLimiter {
	return &DatabaseLimiter{pool: pool, now: time.Now}
}

func (l *DatabaseLimiter) Consume(ctx context.Context, key string, maximum int, window time.Duration) error {
	if maximum < 1 {
		return errors.New("maximum must be a positive integer")
	}
	if window < time.Second || window%time.Second != 0 {
		return errors.New("window must contain a positive whole number of seconds")
	}
	observedAt := l.now().UTC()
	resetAt := observedAt.Add(window)
	keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	var count int
	var actualResetAt time.Time
	err := l.pool.QueryRow(ctx, `
		insert into rate_limit_buckets (key_hash, count, reset_at, updated_at)
		values ($1, 1, $2, $3)
		on conflict (key_hash) do update set
			count = case
				when rate_limit_buckets.reset_at <= $3 then 1
				else least(rate_limit_buckets.count + 1, $4)
			end,
			reset_at = case
				when rate_limit_buckets.reset_at <= $3 then $2
				else rate_limit_buckets.reset_at
			end,
			updated_at = $3
		returning count, reset_at`, keyHash, resetAt, observedAt, maximum+1).Scan(&count, &actualResetAt)
	if err != nil {
		return apperror.New(
			apperror.CodeDependencyUnavailable,
			"限流状态暂时不可用",
			apperror.WithCause(err),
		)
	}
	if count > maximum {
		retryAfter := int(math.Ceil(actualResetAt.Sub(observedAt).Seconds()))
		if retryAfter < 1 {
			retryAfter = 1
		}
		return apperror.RateLimited(retryAfter)
	}
	return nil
}
