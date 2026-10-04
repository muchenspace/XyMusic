package adminsettings

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/workerstatus"
	"xymusic/server/internal/shared/apperror"
)

type ServiceDependencies struct {
	Database           Database
	Databases          DatabaseFactory
	Runtime            RuntimeController
	Store              Store
	Configuration      ConfigurationStore
	Idempotency        IdempotencyFactory
	Storage            MediaStorageFactory
	MediaTool          MediaTool
	Worker             WorkerMonitor
	Metrics            RuntimeMetrics
	DirectoryProbe     DirectoryProbe
	RootDirectory      string
	ConfigurationPath  string
	Listener           ListenerDTO
	ApplicationVersion string
	StartedAt          time.Time
	Now                func() time.Time
}

type Service struct {
	database           Database
	databases          DatabaseFactory
	runtime            RuntimeController
	store              Store
	configuration      ConfigurationStore
	idempotency        IdempotencyFactory
	storage            MediaStorageFactory
	mediaTool          MediaTool
	worker             WorkerMonitor
	metrics            RuntimeMetrics
	directoryProbe     DirectoryProbe
	rootDirectory      string
	configurationPath  string
	listener           ListenerDTO
	applicationVersion string
	startedAt          time.Time
	now                func() time.Time
	transition         sync.Mutex
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Databases == nil || dependencies.Runtime == nil ||
		dependencies.Store == nil || dependencies.Configuration == nil || dependencies.Idempotency == nil ||
		dependencies.Storage == nil || dependencies.MediaTool == nil || dependencies.Worker == nil ||
		dependencies.Metrics == nil || dependencies.DirectoryProbe == nil {
		return nil, errors.New("admin settings dependencies are required")
	}
	root, err := filepath.Abs(dependencies.RootDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve admin settings root: %w", err)
	}
	configurationPath, err := filepath.Abs(dependencies.ConfigurationPath)
	if err != nil {
		return nil, fmt.Errorf("resolve admin settings configuration path: %w", err)
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	startedAt := dependencies.StartedAt
	if startedAt.IsZero() {
		startedAt = now()
	}
	version := strings.TrimSpace(dependencies.ApplicationVersion)
	if version == "" {
		version = "development"
	}
	return &Service{
		database: dependencies.Database, databases: dependencies.Databases,
		runtime: dependencies.Runtime, store: dependencies.Store,
		configuration: dependencies.Configuration,
		idempotency:   dependencies.Idempotency,
		storage:       dependencies.Storage, mediaTool: dependencies.MediaTool, worker: dependencies.Worker,
		metrics:        dependencies.Metrics,
		directoryProbe: dependencies.DirectoryProbe,
		rootDirectory:  root, configurationPath: configurationPath, listener: dependencies.Listener,
		applicationVersion: version, startedAt: startedAt, now: now,
	}, nil
}

func (service *Service) Settings() (SettingsDTO, error) {
	active, status, err := service.activeConfig()
	if err != nil {
		return SettingsDTO{}, err
	}
	return presentSettings(active, status.Generation, status.Source, service.listener)
}

func (service *Service) TestDatabase(ctx context.Context, input DatabaseInput) (TestResponse, error) {
	current, _, err := service.activeConfig()
	if err != nil {
		return TestResponse{}, err
	}
	candidate, err := mergeDatabase(current, input)
	if err != nil {
		return TestResponse{}, err
	}
	started := service.now()
	pool, err := service.databases.Open(ctx, candidate.Database)
	if err != nil {
		return TestResponse{}, apperror.DependencyUnavailable("Database connection test failed")
	}
	pool.Close()
	latency := elapsedMilliseconds(service.now().Sub(started))
	return TestResponse{OK: true, Message: "Database connection succeeded", LatencyMS: &latency}, nil
}

func (service *Service) TestStorage(ctx context.Context, input StorageInput) (StorageTestResponse, error) {
	current, _, err := service.activeConfig()
	if err != nil {
		return StorageTestResponse{}, err
	}
	candidate, err := mergeStorage(current, input)
	if err != nil {
		return StorageTestResponse{}, err
	}
	started := service.now()
	mediaStorage, err := service.storage.Open(candidate.MediaStorage)
	if err != nil {
		return StorageTestResponse{}, err
	}
	defer mediaStorage.Close()
	if err := mediaStorage.EnsureDirectories(ctx); err != nil {
		return StorageTestResponse{}, err
	}
	if err := mediaStorage.Probe(ctx); err != nil {
		return StorageTestResponse{}, err
	}
	return StorageTestResponse{
		OK:                   true,
		Message:              "Media storage directories are accessible",
		AssetDirectoryExists: true,
		LatencyMS:            elapsedMilliseconds(service.now().Sub(started)),
	}, nil
}

func (service *Service) TestMediaTools(ctx context.Context, input MediaToolsInput) (TestResponse, error) {
	current, _, err := service.activeConfig()
	if err != nil {
		return TestResponse{}, err
	}
	candidate, err := mergeMediaTools(current, input)
	if err != nil {
		return TestResponse{}, err
	}
	resolved, err := config.ResolveRuntime(candidate, service.rootDirectory)
	if err != nil {
		return TestResponse{}, validation(err.Error())
	}
	ffmpeg, err := service.mediaTool.Version(ctx, resolved.Media.FFmpegPath, "ffmpeg")
	if err != nil {
		return TestResponse{}, err
	}
	ffprobe, err := service.mediaTool.Version(ctx, resolved.Media.FFprobePath, "ffprobe")
	if err != nil {
		return TestResponse{}, err
	}
	return TestResponse{
		OK: true, Message: "FFmpeg tools are available", Details: []string{ffmpeg, ffprobe},
		Paths: map[string]string{"ffmpegPath": resolved.Media.FFmpegPath, "ffprobePath": resolved.Media.FFprobePath},
	}, nil
}

func (service *Service) TestLocalLibrary(_ context.Context, directory *string) (LocalLibraryTestResponse, error) {
	current, _, err := service.activeConfig()
	if err != nil {
		return LocalLibraryTestResponse{}, err
	}
	value := current.LocalLibrary.Directory
	if directory != nil {
		value = *directory
	}
	value, err = requiredText(value, 4000, "directory")
	if err != nil {
		return LocalLibraryTestResponse{}, err
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(service.rootDirectory, value)
	}
	value = filepath.Clean(value)
	if err := service.requireDirectory(value); err != nil {
		return LocalLibraryTestResponse{}, err
	}
	return LocalLibraryTestResponse{OK: true, Message: "Music directory is readable", NormalizedPath: value}, nil
}

func (service *Service) ApplyIdempotently(
	ctx context.Context,
	actorID, key string,
	input UpdateInput,
) (IdempotentSettingsResult, error) {
	current, _, err := service.activeConfig()
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	candidate, err := mergeSettings(current, input)
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	candidatePool, err := service.databases.Open(ctx, candidate.Database)
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	defer candidatePool.Close()
	if err := service.store.RequireAdministrator(ctx, candidatePool, actorID); err != nil {
		return IdempotentSettingsResult{}, err
	}
	if !reflect.DeepEqual(current.Database, candidate.Database) {
		resolved, resolveErr := config.ResolveRuntime(candidate, service.rootDirectory)
		if resolveErr != nil {
			return IdempotentSettingsResult{}, validation(resolveErr.Error())
		}
		if err := service.databases.Migrate(ctx, candidatePool, resolved.Paths.MigrationsDirectory); err != nil {
			return IdempotentSettingsResult{}, normalizeConfigurationError(err)
		}
	}
	idempotency, err := service.idempotency.New(candidatePool, candidate.Security.IdempotencyEncryptionSecret)
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	result, err := idempotency.Execute(ctx, actorID, "admin.system.settings.apply", key, input,
		func() (int, SettingsDTO, error) {
			body, applyErr := service.Apply(ctx, actorID, input)
			return 200, body, applyErr
		})
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	return IdempotentSettingsResult{Status: result.Status, Body: result.Body, Replayed: result.Replayed}, nil
}

func (service *Service) Apply(ctx context.Context, actorID string, input UpdateInput) (SettingsDTO, error) {
	service.transition.Lock()
	defer service.transition.Unlock()
	previous, status, err := service.activeConfig()
	if err != nil {
		return SettingsDTO{}, err
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion != status.Generation {
		return SettingsDTO{}, apperror.Conflict(apperror.CodeVersionConflict, "System settings version is stale", map[string]any{
			"expectedVersion": input.ExpectedVersion, "currentVersion": status.Generation,
		})
	}
	candidate, err := mergeSettings(previous, input)
	if err != nil {
		return SettingsDTO{}, err
	}
	changed := changedFields(previous, candidate)
	if len(changed) == 0 {
		return SettingsDTO{}, validation("At least one system setting must change")
	}
	candidatePool, err := service.databases.Open(ctx, candidate.Database)
	if err != nil {
		return SettingsDTO{}, normalizeConfigurationError(err)
	}
	defer candidatePool.Close()
	if err := service.store.RequireAdministrator(ctx, candidatePool, actorID); err != nil {
		return SettingsDTO{}, err
	}
	resolvedCandidate, err := config.ResolveRuntime(candidate, service.rootDirectory)
	if err != nil {
		return SettingsDTO{}, validation(err.Error())
	}
	if hasPrefix(changed, "database.") {
		if err := service.databases.Migrate(ctx, candidatePool, resolvedCandidate.Paths.MigrationsDirectory); err != nil {
			return SettingsDTO{}, normalizeConfigurationError(err)
		}
	}
	failure := func(cause error) (SettingsDTO, error) {
		active, ok := service.runtime.ActiveConfig()
		if ok && reflect.DeepEqual(active, candidate) {
			_ = service.runtime.Initialize(context.WithoutCancel(ctx), previous, status.Source)
			_ = service.configuration.Save(previous)
		}
		return SettingsDTO{}, normalizeConfigurationError(cause)
	}
	if err := service.validateAffectedDependencies(ctx, previous, candidate, resolvedCandidate); err != nil {
		return failure(err)
	}
	if !reflect.DeepEqual(previous.MediaStorage, candidate.MediaStorage) {
		mediaStorage, openErr := service.storage.Open(candidate.MediaStorage)
		if openErr != nil {
			return failure(openErr)
		}
		ensureErr := mediaStorage.EnsureDirectories(ctx)
		mediaStorage.Close()
		if ensureErr != nil {
			return failure(ensureErr)
		}
	}
	if err := service.runtime.Initialize(ctx, candidate, setup.RuntimeSourceManaged); err != nil {
		return failure(err)
	}
	if err := service.configuration.Save(candidate); err != nil {
		return failure(err)
	}
	newStatus := service.runtime.Status()
	result, err := presentSettings(candidate, newStatus.Generation, setup.RuntimeSourceManaged, service.listener)
	if err != nil {
		return failure(err)
	}
	result.AppliedFields = changed
	return result, nil
}

func (service *Service) SystemInformation(ctx context.Context) (SystemInformationDTO, error) {
	cfg, status, err := service.activeConfig()
	if err != nil {
		return SystemInformationDTO{}, err
	}
	resolved, err := config.ResolveRuntime(cfg, service.rootDirectory)
	if err != nil {
		return SystemInformationDTO{}, err
	}
	databaseVersion, err := service.store.ServerVersion(ctx)
	if err != nil {
		return SystemInformationDTO{}, err
	}
	migration := service.store.MigrationInformation(ctx)
	queues, err := service.store.QueueInformation(ctx)
	if err != nil {
		return SystemInformationDTO{}, err
	}
	var ffmpegVersion *string
	if value, versionErr := service.mediaTool.Version(ctx, resolved.Media.FFmpegPath, "ffmpeg"); versionErr == nil {
		ffmpegVersion = &value
	}
	worker := service.worker.Status(ctx, workerstatus.ConfigurationFingerprint(cfg))
	return service.systemInformationDTO(status, databaseVersion, migration, ffmpegVersion, worker, queues), nil
}

func (service *Service) systemInformationDTO(
	status setup.RuntimeSnapshot,
	databaseVersion string,
	migrationVersion string,
	ffmpegVersion *string,
	worker workerstatus.Snapshot,
	queues QueueDTO,
) SystemInformationDTO {
	uptime := service.now().Sub(service.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	return SystemInformationDTO{
		ApplicationVersion: service.applicationVersion, RuntimeVersion: runtime.Version(),
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, UptimeSeconds: int64(uptime / time.Second),
		DatabaseVersion: databaseVersion, MigrationVersion: migrationVersion, FFmpegVersion: ffmpegVersion,
		DataDirectory: dataDirectory(service.configurationPath), ConfigurationFile: service.configurationPath,
		ConfigurationSource: status.Source, Worker: worker, Metrics: service.metrics.Snapshot(), Queues: queues,
	}
}

func (service *Service) activeConfig() (config.Config, setup.RuntimeSnapshot, error) {
	active, ok := service.runtime.ActiveConfig()
	if !ok {
		return config.Config{}, setup.RuntimeSnapshot{}, apperror.DependencyUnavailable("The managed runtime is not available")
	}
	return active, service.runtime.Status(), nil
}

func (service *Service) validateAffectedDependencies(
	ctx context.Context,
	previous, candidate, resolved config.Config,
) error {
	if !reflect.DeepEqual(previous.MediaStorage, candidate.MediaStorage) {
		mediaStorage, err := service.storage.Open(candidate.MediaStorage)
		if err != nil {
			return err
		}
		probeErr := mediaStorage.Probe(ctx)
		mediaStorage.Close()
		if probeErr != nil {
			return probeErr
		}
	}
	if !reflect.DeepEqual(previous.Media, candidate.Media) || previous.Paths.MediaToolsDirectory != candidate.Paths.MediaToolsDirectory {
		if _, err := service.mediaTool.Version(ctx, resolved.Media.FFmpegPath, "ffmpeg"); err != nil {
			return err
		}
		if _, err := service.mediaTool.Version(ctx, resolved.Media.FFprobePath, "ffprobe"); err != nil {
			return err
		}
	}
	if previous.LocalLibrary.Directory != candidate.LocalLibrary.Directory {
		if err := service.requireDirectory(resolved.LocalLibrary.Directory); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) requireDirectory(path string) error {
	info, err := service.directoryProbe.Stat(path)
	if err != nil || !info.IsDir() {
		return validation("Music directory is not readable")
	}
	directory, err := service.directoryProbe.Open(path)
	if err != nil {
		return validation("Music directory is not readable by the service process")
	}
	return directory.Close()
}

func hasPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func normalizeConfigurationError(err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}
	return apperror.New(
		apperror.CodeDependencyUnavailable,
		"The candidate configuration could not be applied; the previous runtime remains active",
		apperror.WithCause(err),
	)
}

func elapsedMilliseconds(value time.Duration) int64 {
	if value < 0 {
		return 0
	}
	return value.Milliseconds()
}

var (
	credentialPattern = regexp.MustCompile(`(?i)((?:postgres|postgresql)://[^:\s/]+:)[^@\s/]+@`)
	secretPattern     = regexp.MustCompile(`(?i)\b(password|secret(?:Access)?Key|authorization)\s*[=:]\s*[^,;\s]+`)
	tokenPattern      = regexp.MustCompile(`\b[A-Za-z0-9_-]{64,}\b`)
)
