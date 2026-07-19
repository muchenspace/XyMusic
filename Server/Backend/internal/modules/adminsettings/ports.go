package adminsettings

import (
	"context"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/runtimemetrics"
	"xymusic/server/internal/platform/workerstatus"
)

type RuntimeController interface {
	Status() setup.RuntimeSnapshot
	ActiveConfig() (config.Config, bool)
	Initialize(context.Context, config.Config, string) error
}

type ConfigurationStore interface {
	Save(config.Config) error
}

type StorageFactory interface {
	Open(config.Storage) (StorageProbe, error)
}

type StorageProbe interface {
	Probe(context.Context) (bool, error)
	EnsureBucket(context.Context) error
	Close()
}

type MediaTool interface {
	Version(context.Context, string, string) (string, error)
}

type WorkerMonitor interface {
	Status(context.Context, string) workerstatus.Snapshot
}

type RuntimeMetrics interface {
	Snapshot() runtimemetrics.Snapshot
}
