package setup

import (
	"context"

	"xymusic/server/internal/config"
)

const (
	RuntimeSourceSetup   = "setup"
	RuntimeSourceManaged = "managed"
	RuntimePhaseReady    = "READY"
)

type RuntimeSnapshot struct {
	Phase      string
	Source     string
	Generation int
	StartedAt  *string
	LastError  *string
}

// RuntimeController activates the newly persisted configuration. The setup
// module deliberately owns only this port; the application composition layer
// supplies the concrete hot-reload/runtime implementation.
type RuntimeController interface {
	Status() RuntimeSnapshot
	Initialize(context.Context, config.Config, string) error
	Close(context.Context) error
}

type ConfigurationRepository interface {
	Load(context.Context) (config.Config, bool, error)
	Save(context.Context, config.Config) error
	Clear(context.Context) error
}

type DatabaseFactory interface {
	Open(context.Context, config.Database) (InstallationDatabase, error)
}

type InstallationDatabase interface {
	Ping(context.Context) error
	CanCreateInCurrentSchema(context.Context) (bool, error)
	CheckMigrationCompatibility(context.Context, string) error
	RunMigrations(context.Context, string) error
	Inspect(context.Context, string) (InstallationInspection, error)
	Reset(context.Context) error
	Provision(context.Context, ProvisionInput) (ProvisionedInstallation, error)
	Compensate(context.Context, ProvisionedInstallation, string) error
	RecordSetupSuccess(context.Context, string, string, string) error
	Close()
}

type ProvisionInput struct {
	Administrator AdministratorRecord
	Source        ValidatedSource
	ReuseExisting bool
}

type AdministratorRecord struct {
	Username           string
	NormalizedUsername string
	DisplayName        string
	PasswordHash       string
}

type ProvisionedInstallation struct {
	AdministratorID      string
	LibraryRootID        string
	CreatedAdministrator bool
	CreatedLibraryRoot   bool
}

type ObjectStorageFactory interface {
	Open(config.Storage) (SetupObjectStorage, error)
}

type SetupObjectStorage interface {
	Probe(context.Context) error
	Inspect(context.Context) (StorageInspection, error)
	EnsureBucket(context.Context) (bool, error)
	VerifyReadWrite(context.Context) error
	Clear(context.Context) error
	RemoveBucket(context.Context) error
	Close()
}

type MediaTool interface {
	Version(context.Context, string, string) (string, error)
}

type ListenerProbe interface {
	Check(context.Context, string, int) error
}

type SourceValidator interface {
	Validate(context.Context, SourceInput, string) (ValidatedSource, error)
}

type PasswordHasher interface {
	Hash(string) (string, error)
}

type ValidatedSource struct {
	Name                string
	Path                string
	NormalizedPath      string
	Mode                string
	Enabled             bool
	ScanOnStartup       bool
	ScanIntervalMinutes *int
	IncludePatterns     []string
	ExcludePatterns     []string
	Status              string
}
