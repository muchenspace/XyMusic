package ratelimit

import (
	"context"
	"time"
)

type Limiter interface {
	Consume(ctx context.Context, key string, maximum int, window time.Duration) error
}
