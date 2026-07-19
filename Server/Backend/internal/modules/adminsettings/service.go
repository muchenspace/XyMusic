package adminsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/platform/workerstatus"
	"xymusic/server/internal/shared/apperror"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
)

type ServiceDependencies struct {
	Database           *database.Pool
	Runtime            RuntimeController
	Store              ConfigurationStore
	Storage            StorageFactory
	MediaTool          MediaTool
	Worker             WorkerMonitor
	Metrics            RuntimeMetrics
	RootDirectory      string
	ConfigurationPath  string
	Listener           ListenerDTO
	ApplicationVersion string
	StartedAt          time.Time
	Now                func() time.Time
}

type Service struct {
	database           *database.Pool
	runtime            RuntimeController
	store              ConfigurationStore
	storage            StorageFactory
	mediaTool          MediaTool
	worker             WorkerMonitor
	metrics            RuntimeMetrics
	rootDirectory      string
	configurationPath  string
	listener           ListenerDTO
	applicationVersion string
	startedAt          time.Time
	now                func() time.Time
	transition         sync.Mutex
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Runtime == nil || dependencies.Store == nil ||
		dependencies.Storage == nil || dependencies.MediaTool == nil || dependencies.Worker == nil ||
		dependencies.Metrics == nil {
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
		database: dependencies.Database, runtime: dependencies.Runtime, store: dependencies.Store,
		storage: dependencies.Storage, mediaTool: dependencies.MediaTool, worker: dependencies.Worker,
		metrics:       dependencies.Metrics,
		rootDirectory: root, configurationPath: configurationPath, listener: dependencies.Listener,
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
	pool, err := database.Open(ctx, candidate.Database)
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
	objects, err := service.storage.Open(candidate.Storage)
	if err != nil {
		return StorageTestResponse{}, err
	}
	defer objects.Close()
	exists, err := objects.Probe(ctx)
	if err != nil {
		return StorageTestResponse{}, err
	}
	message := "Object storage endpoint is available; the bucket will be created when settings are applied"
	if exists {
		message = "Object storage bucket is available"
	}
	return StorageTestResponse{
		OK: true, Message: message, BucketExists: exists,
		LatencyMS: elapsedMilliseconds(service.now().Sub(started)),
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
	if err := requireDirectory(value); err != nil {
		return LocalLibraryTestResponse{}, err
	}
	return LocalLibraryTestResponse{OK: true, Message: "Music directory is readable", NormalizedPath: value}, nil
}

func (service *Service) ApplyIdempotently(
	ctx context.Context,
	actorID, traceID, key string,
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
	candidatePool, err := database.Open(ctx, candidate.Database)
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	defer candidatePool.Close()
	if err := requireAdministrator(ctx, candidatePool, actorID); err != nil {
		return IdempotentSettingsResult{}, err
	}
	if !reflect.DeepEqual(current.Database, candidate.Database) {
		resolved, resolveErr := config.ResolveRuntime(candidate, service.rootDirectory)
		if resolveErr != nil {
			return IdempotentSettingsResult{}, validation(resolveErr.Error())
		}
		if err := database.RunMigrations(ctx, candidatePool.Pool, resolved.Paths.MigrationsDirectory); err != nil {
			return IdempotentSettingsResult{}, normalizeConfigurationError(err)
		}
	}
	cipher, err := security.NewPayloadCipher(candidate.Security.IdempotencyEncryptionSecret)
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	idempotency := sharedidempotency.New(candidatePool.Pool, cipher)
	result, err := sharedidempotency.Execute(ctx, idempotency, sharedidempotency.Input{
		ActorID: actorID, Scope: "admin.system.settings.apply", Key: key, Payload: input,
	}, func() (sharedidempotency.HTTPResult[SettingsDTO], error) {
		body, applyErr := service.Apply(ctx, actorID, traceID, input)
		return sharedidempotency.HTTPResult[SettingsDTO]{Status: 200, Body: body}, applyErr
	})
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	return IdempotentSettingsResult{Status: result.Status, Body: result.Body, Replayed: result.Replayed}, nil
}

func (service *Service) Apply(ctx context.Context, actorID, traceID string, input UpdateInput) (SettingsDTO, error) {
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
	candidatePool, err := database.Open(ctx, candidate.Database)
	if err != nil {
		return SettingsDTO{}, normalizeConfigurationError(err)
	}
	defer candidatePool.Close()
	if err := requireAdministrator(ctx, candidatePool, actorID); err != nil {
		return SettingsDTO{}, err
	}
	resolvedCandidate, err := config.ResolveRuntime(candidate, service.rootDirectory)
	if err != nil {
		return SettingsDTO{}, validation(err.Error())
	}
	if hasPrefix(changed, "database.") {
		if err := database.RunMigrations(ctx, candidatePool.Pool, resolvedCandidate.Paths.MigrationsDirectory); err != nil {
			return SettingsDTO{}, normalizeConfigurationError(err)
		}
	}
	auditID, err := createApplyingAudit(ctx, candidatePool, actorID, traceID, changed)
	if err != nil {
		return SettingsDTO{}, normalizeConfigurationError(err)
	}
	failure := func(cause error) (SettingsDTO, error) {
		_ = updateSettingsAudit(context.WithoutCancel(ctx), candidatePool, auditID, "FAILURE", map[string]any{
			"state": "FAILED", "changedFields": changed, "error": safeError(cause),
		})
		active, ok := service.runtime.ActiveConfig()
		if ok && reflect.DeepEqual(active, candidate) {
			_ = service.runtime.Initialize(context.WithoutCancel(ctx), previous, status.Source)
			_ = service.store.Save(previous)
		}
		return SettingsDTO{}, normalizeConfigurationError(cause)
	}
	if err := service.validateAffectedDependencies(ctx, previous, candidate, resolvedCandidate); err != nil {
		return failure(err)
	}
	if !reflect.DeepEqual(previous.Storage, candidate.Storage) {
		objects, openErr := service.storage.Open(candidate.Storage)
		if openErr != nil {
			return failure(openErr)
		}
		ensureErr := objects.EnsureBucket(ctx)
		objects.Close()
		if ensureErr != nil {
			return failure(ensureErr)
		}
	}
	if err := service.runtime.Initialize(ctx, candidate, setup.RuntimeSourceManaged); err != nil {
		return failure(err)
	}
	if err := service.store.Save(candidate); err != nil {
		return failure(err)
	}
	newStatus := service.runtime.Status()
	if err := updateSettingsAudit(ctx, candidatePool, auditID, "SUCCESS", map[string]any{
		"state": "APPLIED", "changedFields": changed, "runtimeGeneration": newStatus.Generation,
	}); err != nil {
		return failure(err)
	}
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
	var databaseVersion string
	if err := service.database.QueryRow(ctx, "select current_setting('server_version')").Scan(&databaseVersion); err != nil {
		return SystemInformationDTO{}, err
	}
	migration := migrationInformation(ctx, service.database)
	queues, err := queueInformation(ctx, service.database)
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
	if !reflect.DeepEqual(previous.Storage, candidate.Storage) {
		objects, err := service.storage.Open(candidate.Storage)
		if err != nil {
			return err
		}
		_, probeErr := objects.Probe(ctx)
		objects.Close()
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
	if previous.Scraping.FPcalcPath != candidate.Scraping.FPcalcPath && resolved.Scraping.FPcalcPath != "" {
		if _, err := service.mediaTool.Version(ctx, resolved.Scraping.FPcalcPath, "fpcalc"); err != nil {
			return err
		}
	}
	if previous.LocalLibrary.Directory != candidate.LocalLibrary.Directory {
		if err := requireDirectory(resolved.LocalLibrary.Directory); err != nil {
			return err
		}
	}
	return nil
}

func requireAdministrator(ctx context.Context, pool *database.Pool, actorID string) error {
	var id string
	err := pool.QueryRow(ctx, `
		select id from users where id=$1 and role='ADMIN' and status='ACTIVE' limit 1
	`, actorID).Scan(&id)
	if err == nil {
		return nil
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && (databaseError.Code == "42P01" || databaseError.Code == "42703") {
		return apperror.Conflict(apperror.CodeResourceConflict, "The target database is not an initialized compatible XyMusic database", nil)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.Conflict(apperror.CodeResourceConflict, "The target database does not contain the current active administrator", nil)
	}
	return err
}

func createApplyingAudit(ctx context.Context, pool *database.Pool, actorID, traceID string, changed []string) (string, error) {
	details, err := json.Marshal(map[string]any{"state": "APPLYING", "changedFields": changed})
	if err != nil {
		return "", err
	}
	var id string
	err = pool.QueryRow(ctx, `
		insert into audit_logs(actor_id,action,target_type,target_id,result,trace_id,details)
		values($1,'admin.system.settings.apply','system',null,'FAILURE',$2,$3::jsonb)
		returning id
	`, actorID, traceID, string(details)).Scan(&id)
	return id, err
}

func updateSettingsAudit(ctx context.Context, pool *database.Pool, auditID, result string, details map[string]any) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	command, err := pool.Exec(ctx, `update audit_logs set result=$1, details=$2::jsonb where id=$3`, result, string(encoded), auditID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("configuration audit record could not be updated")
	}
	return nil
}

func migrationInformation(ctx context.Context, pool *database.Pool) string {
	var count int
	var latest *string
	err := pool.QueryRow(ctx, `
		select count(*)::int, max(created_at)::text from drizzle.__drizzle_migrations
	`).Scan(&count, &latest)
	if err != nil {
		return "not initialized"
	}
	if latest == nil {
		return fmt.Sprintf("%d applied", count)
	}
	return fmt.Sprintf("%d applied, latest %s", count, *latest)
}

func queueInformation(ctx context.Context, pool *database.Pool) (QueueDTO, error) {
	result := QueueDTO{}
	err := pool.QueryRow(ctx, `
		select
			(select count(*)::int from media_jobs where status in ('PENDING','PROCESSING')),
			(select count(*)::int from library_scan_runs where status in ('PENDING','RUNNING')),
			(select count(*)::int from object_cleanup_jobs where status in ('PENDING','PROCESSING')),
			(select count(*)::int from metadata_writeback_jobs where status in ('PENDING','PROCESSING')),
			(
				(select count(*)::int from tag_scraping_jobs where status in ('PENDING','RUNNING')) +
				(select count(*)::int from artist_artwork_scraping_jobs where status in ('PENDING','RUNNING'))
			)
	`).Scan(&result.Media, &result.Scans, &result.Cleanup, &result.Writeback, &result.Scraping)
	result.Total = result.Media + result.Scans + result.Cleanup + result.Writeback + result.Scraping
	return result, err
}

func requireDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return validation("Music directory is not readable")
	}
	directory, err := os.Open(path)
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

func safeError(err error) string {
	value := err.Error()
	if len(value) > 1000 {
		value = value[:1000]
	}
	value = credentialPattern.ReplaceAllString(value, "$1[REDACTED]@")
	value = secretPattern.ReplaceAllString(value, "$1=[REDACTED]")
	return tokenPattern.ReplaceAllString(value, "[REDACTED]")
}
