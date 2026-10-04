package app

import (
	"context"
	"errors"
	"time"
)

// Close releases the runtime and its background work.
func (runtime *Runtime) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = runtime.CloseContext(ctx)
}

func (runtime *Runtime) CloseContext(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	var closeErr error
	if runtime.background != nil {
		closeErr = runtime.background.Close(ctx)
	}
	if runtime.AdminTagBatches != nil {
		closeErr = errors.Join(closeErr, runtime.AdminTagBatches.Close(ctx))
	}
	if runtime.AdminArtistArtworkBatches != nil {
		closeErr = errors.Join(closeErr, runtime.AdminArtistArtworkBatches.Close(ctx))
	}
	if runtime.events != nil {
		runtime.events.Close()
	}
	if runtime.Metrics != nil {
		runtime.Metrics.Close()
	}
	if runtime.DB != nil {
		runtime.DB.Close()
	}
	return closeErr
}
