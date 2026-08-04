package resourcebudget

import (
	"context"
	"errors"
	"fmt"
)

var ErrRequestExceedsCapacity = errors.New("resource budget request exceeds capacity")

// Budget is a weighted, context-aware semaphore. A single process can share
// one budget between independent workers so their configured concurrency does
// not accidentally add up to an unsafe machine-wide total.
type Budget struct {
	tokens chan struct{}
	limit  int
}

func New(limit int) (*Budget, error) {
	if limit <= 0 {
		return nil, errors.New("resource budget capacity must be positive")
	}
	tokens := make(chan struct{}, limit)
	for index := 0; index < limit; index++ {
		tokens <- struct{}{}
	}
	return &Budget{tokens: tokens, limit: limit}, nil
}

func (budget *Budget) Capacity() int {
	if budget == nil {
		return 0
	}
	return budget.limit
}

func (budget *Budget) Acquire(ctx context.Context, units int) error {
	if budget == nil || units <= 0 {
		return nil
	}
	if units > budget.limit {
		return fmt.Errorf("%w: requested %d, capacity %d", ErrRequestExceedsCapacity, units, budget.limit)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	acquired := 0
	for acquired < units {
		select {
		case <-budget.tokens:
			acquired++
		case <-ctx.Done():
			budget.Release(acquired)
			return ctx.Err()
		}
	}
	return nil
}

func (budget *Budget) Release(units int) {
	if budget == nil || units <= 0 {
		return
	}
	if units > budget.limit {
		units = budget.limit
	}
	for index := 0; index < units; index++ {
		budget.tokens <- struct{}{}
	}
}
