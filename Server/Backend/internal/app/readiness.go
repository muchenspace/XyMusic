package app

import (
	"context"
	"errors"
	"time"

	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/localmedia"
)

// dependencyReadiness checks the runtime dependencies needed to serve traffic.
type dependencyReadiness struct {
	database   *database.Pool
	localMedia *localmedia.Store
}

func (check *dependencyReadiness) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- check.database.Ping(ctx) }()
	go func() { errorsChannel <- check.localMedia.Ping(ctx) }()
	for range 2 {
		if err := <-errorsChannel; err != nil {
			return errors.Join(errors.New("runtime dependency is unavailable"), err)
		}
	}
	return nil
}
