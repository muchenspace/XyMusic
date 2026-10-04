package adminsettings

import (
	"context"
	"io"
	"os"

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

// Row mirrors the subset of the pgx row contract used by the settings store.
type Row interface {
	Scan(dest ...any) error
}

// Database is the narrow contract for a candidate database connection opened
// while validating or applying settings.
type Database interface {
	Ping(context.Context) error
	Close()
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// DatabaseFactory opens candidate database connections and runs migrations on
// them. The production implementation adapts internal/platform/database.
type DatabaseFactory interface {
	Open(context.Context, config.Database) (Database, error)
	Migrate(context.Context, Database, string) error
}

// Store owns every SQL statement used by the administrator settings module.
// A Store is bound to the active runtime database; operations against a
// candidate database receive that database explicitly.
type Store interface {
	ServerVersion(context.Context) (string, error)
	MigrationInformation(context.Context) string
	QueueInformation(context.Context) (QueueDTO, error)
	RequireAdministrator(context.Context, Database, string) error
}

// Idempotency executes an operation behind the persistent idempotency
// contract of the administrator settings module.
type Idempotency interface {
	Execute(
		ctx context.Context,
		actorID, scope, key string,
		payload any,
		operation func() (int, SettingsDTO, error),
	) (IdempotentSettingsResult, error)
}

// IdempotencyFactory builds the idempotency executor for a candidate database.
type IdempotencyFactory interface {
	New(Database, string) (Idempotency, error)
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

// DirectoryProbe performs the read-only filesystem checks used to validate
// the local library directory.
type DirectoryProbe interface {
	Stat(string) (os.FileInfo, error)
	Open(string) (io.Closer, error)
}

type WorkerMonitor interface {
	Status(context.Context, string) workerstatus.Snapshot
}

type RuntimeMetrics interface {
	Snapshot() runtimemetrics.Snapshot
}
