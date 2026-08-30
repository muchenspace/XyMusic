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

type MediaStorageFactory interface {
	Open(config.MediaStorage) (MediaStorageProbe, error)
}

type MediaStorageProbe interface {
	Probe(context.Context) error
	EnsureDirectories(context.Context) error
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
