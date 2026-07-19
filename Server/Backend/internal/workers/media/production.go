package media

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/config"
)

type ProductionOptions struct {
	Database         *pgxpool.Pool
	Storage          config.Storage
	Media            config.Media
	WorkerID         string
	Logger           Logger
	Clock            Clock
	Runner           ProcessRunner
	TemporaryRoot    string
	Lease            time.Duration
	Heartbeat        time.Duration
	CancellationPoll time.Duration
}

func NewProduction(options ProductionOptions) (*Worker, error) {
	if options.Database == nil {
		return nil, errors.New("media worker production database is required")
	}
	store, err := NewPostgresStore(options.Database)
	if err != nil {
		return nil, err
	}
	storage, err := NewMinIOObjectStorage(options.Storage)
	if err != nil {
		return nil, err
	}
	return New(Options{
		Store: store, Storage: storage,
		FFmpegPath: options.Media.FFmpegPath, FFprobePath: options.Media.FFprobePath,
		WorkerID: options.WorkerID, Logger: options.Logger, Clock: options.Clock,
		Runner: options.Runner, TemporaryRoot: options.TemporaryRoot,
		Lease: options.Lease, Heartbeat: options.Heartbeat,
		CancellationPoll: options.CancellationPoll,
	})
}
