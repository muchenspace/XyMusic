package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type backgroundGroup struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type backgroundTask struct {
	name string
	run  func(context.Context) (bool, error)
}

func startBackgroundGroup(logger *slog.Logger, tasks ...backgroundTask) *backgroundGroup {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	group := &backgroundGroup{cancel: cancel, done: make(chan struct{})}
	var workers sync.WaitGroup
	for _, task := range tasks {
		if task.run == nil {
			continue
		}
		workers.Add(1)
		go func(task backgroundTask) {
			defer workers.Done()
			runBackgroundPoller(ctx, logger, task)
		}(task)
	}
	go func() {
		workers.Wait()
		close(group.done)
	}()
	return group
}

func runBackgroundPoller(ctx context.Context, logger *slog.Logger, task backgroundTask) {
	idleDelay := time.Second
	for ctx.Err() == nil {
		worked, err := task.run(ctx)
		if err == nil && worked {
			idleDelay = time.Second
			continue
		}
		if err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "background.poller.failed",
				"task", task.name,
				"error", err,
				"retryIn", idleDelay,
			)
		}
		if !waitBackground(ctx, idleDelay) {
			return
		}
		if idleDelay < 15*time.Second {
			idleDelay *= 2
			if idleDelay > 15*time.Second {
				idleDelay = 15 * time.Second
			}
		}
	}
}

func waitBackground(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (group *backgroundGroup) Close(ctx context.Context) error {
	if group == nil {
		return nil
	}
	group.cancel()
	select {
	case <-group.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
