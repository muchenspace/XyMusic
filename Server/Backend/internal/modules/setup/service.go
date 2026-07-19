package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/shared/apperror"
)

const fpcalcDescription = "fpcalc 由 Chromaprint 提供，用于生成 AcoustID 音频指纹；它不属于 FFmpeg，未配置时只会禁用音频指纹识别。"

const (
	databaseActionReusePartial = "reuse_partial"
	databaseActionMigrate      = "migrate"
	databaseActionReset        = "reset"
	storageActionReuse         = "reuse"
	storageActionReset         = "reset"
)

type Options struct {
	RootDirectory       string
	ConfigurationPath   string
	ActualListener      ActualListener
	ConfiguredAtStartup *bool
	Runtime             RuntimeController
	Store               ConfigurationRepository
	Databases           DatabaseFactory
	ObjectStorage       ObjectStorageFactory
	MediaTool           MediaTool
	ListenerProbe       ListenerProbe
	SourceValidator     SourceValidator
	Passwords           PasswordHasher
	SecretGenerator     func() (string, error)
}

type Service struct {
	root            string
	actualListener  ActualListener
	runtime         RuntimeController
	store           ConfigurationRepository
	databases       DatabaseFactory
	objectStorage   ObjectStorageFactory
	mediaTool       MediaTool
	listenerProbe   ListenerProbe
	sourceValidator SourceValidator
	passwords       PasswordHasher
	secretGenerator func() (string, error)

	stateMu    sync.RWMutex
	configured bool
	transition sync.Mutex
}

func NewService(options Options) (*Service, error) {
	root := strings.TrimSpace(options.RootDirectory)
	if root == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate executable root: %w", err)
		}
		root = filepath.Dir(executable)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve setup root: %w", err)
	}
	if options.Runtime == nil {
		return nil, errors.New("setup runtime controller is required")
	}
	configurationPath := strings.TrimSpace(options.ConfigurationPath)
	if configurationPath == "" {
		configurationPath = filepath.Join(absoluteRoot, ".env")
	} else if !filepath.IsAbs(configurationPath) {
		configurationPath = filepath.Join(absoluteRoot, configurationPath)
	}
	store := options.Store
	if store == nil {
		store = NewFileConfigurationRepository(configurationPath)
	}
	databases := options.Databases
	if databases == nil {
		databases = ProductionDatabaseFactory{}
	}
	objects := options.ObjectStorage
	if objects == nil {
		objects = ProductionObjectStorageFactory{}
	}
	mediaTool := options.MediaTool
	if mediaTool == nil {
		mediaTool = CommandMediaTool{}
	}
	listener := options.ListenerProbe
	if listener == nil {
		listener = NetworkListenerProbe{}
	}
	sources := options.SourceValidator
	if sources == nil {
		sources = OSSourceValidator{}
	}
	passwords := options.Passwords
	if passwords == nil {
		passwords = SecurityPasswordHasher{}
	}
	secretGenerator := options.SecretGenerator
	if secretGenerator == nil {
		secretGenerator = randomSecret
	}
	actual := options.ActualListener
	if strings.TrimSpace(actual.IPv4.Host) == "" {
		actual.IPv4.Host = "0.0.0.0"
	}
	if actual.IPv4.Port == 0 {
		actual.IPv4.Port = 3000
	}
	if strings.TrimSpace(actual.IPv6.Host) == "" {
		actual.IPv6.Host = "::"
	}
	if actual.IPv6.Port == 0 {
		actual.IPv6.Port = 3000
	}
	configured := options.Runtime.Status().Source != RuntimeSourceSetup
	if options.ConfiguredAtStartup != nil {
		configured = *options.ConfiguredAtStartup
	}
	return &Service{
		root:            filepath.Clean(absoluteRoot),
		actualListener:  actual,
		runtime:         options.Runtime,
		store:           store,
		databases:       databases,
		objectStorage:   objects,
		mediaTool:       mediaTool,
		listenerProbe:   listener,
		sourceValidator: sources,
		passwords:       passwords,
		secretGenerator: secretGenerator,
		configured:      configured,
	}, nil
}

func (s *Service) Status() StatusResponse {
	runtimeStatus := s.runtime.Status()
	s.stateMu.RLock()
	configured := s.configured
	s.stateMu.RUnlock()
	return StatusResponse{
		SetupRequired:       !configured,
		Configured:          configured,
		ConfigurationSource: runtimeStatus.Source,
		Runtime: RuntimeStatusResponse{
			Phase:      runtimeStatus.Phase,
			Source:     runtimeStatus.Source,
			Generation: runtimeStatus.Generation,
			StartedAt:  runtimeStatus.StartedAt,
		},
		Platform: legacyPlatformName(),
	}
}

func (s *Service) RequireSetup() error {
	s.stateMu.RLock()
	configured := s.configured
	s.stateMu.RUnlock()
	if configured || s.runtime.Status().Phase == RuntimePhaseReady {
		return apperror.Forbidden("初始化已经完成，不能再次执行初始化操作。")
	}
	return nil
}

func (s *Service) TestHTTP(ctx context.Context, input HTTPInput) (OKResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return OKResponse{}, err
	}
	return s.testHTTP(ctx, input)
}

func (s *Service) testHTTP(ctx context.Context, input HTTPInput) (OKResponse, error) {
	if err := validateHTTP(input); err != nil {
		return OKResponse{}, err
	}
	ipv4, ipv6 := normalizedHTTPListeners(input)
	probes := []struct {
		candidate ListenerAddress
		actual    ListenerAddress
		label     string
	}{
		{candidate: ipv4, actual: s.actualListener.IPv4, label: "IPv4"},
		{candidate: ipv6, actual: s.actualListener.IPv6, label: "IPv6"},
	}
	for _, probe := range probes {
		port := probe.candidate.Port
		if probe.candidate == probe.actual {
			port = 0
		}
		if err := s.listenerProbe.Check(ctx, probe.candidate.Host, port); err != nil {
			hostField := "ipv4Host"
			portField := "ipv4Port"
			if probe.label == "IPv6" {
				hostField = "ipv6Host"
				portField = "ipv6Port"
			}
			return OKResponse{}, apperror.New(
				apperror.CodeValidationError,
				probe.label+" 监听地址不可用，IP 无法绑定、端口已被占用或当前进程没有监听权限。",
				apperror.WithCause(err),
				apperror.WithMetadata(map[string]any{
					"fieldErrors": map[string][]string{
						hostField: {probe.label + " 监听 IP 无法绑定"},
						portField: {probe.label + " 监听端口不可用"},
					},
				}),
			)
		}
	}
	return OKResponse{OK: true}, nil
}

func (s *Service) TestPaths(ctx context.Context, input PathsInput) (PathsTestResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return PathsTestResponse{}, err
	}
	return s.testPaths(ctx, input)
}

func (s *Service) testPaths(_ context.Context, input PathsInput) (PathsTestResponse, error) {
	resolved, err := s.resolvePaths(input)
	if err != nil {
		return PathsTestResponse{}, err
	}
	if _, err := database.ReadMigrations(resolved.MigrationsDirectory); err != nil {
		return PathsTestResponse{}, apperror.New(
			apperror.CodeValidationError,
			"数据库迁移目录无效，无法读取迁移记录。",
			apperror.WithCause(err),
			apperror.WithMetadata(map[string]any{
				"fieldErrors": map[string][]string{
					"migrationsDirectory": {"该目录不包含有效的数据库迁移文件"},
				},
			}),
		)
	}
	indexInfo, err := os.Stat(filepath.Join(resolved.AdminWebDirectory, "index.html"))
	if err != nil || !indexInfo.Mode().IsRegular() {
		return PathsTestResponse{}, apperror.New(
			apperror.CodeValidationError,
			"Admin web directory must contain index.html",
			apperror.WithCause(err),
		)
	}
	return PathsTestResponse{OK: true, ResolvedPaths: resolved}, nil
}

func (s *Service) TestDatabase(ctx context.Context, input DatabaseTestInput) (DatabaseTestResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return DatabaseTestResponse{}, err
	}
	return s.testDatabase(ctx, input)
}

func (s *Service) testDatabase(ctx context.Context, input DatabaseTestInput) (DatabaseTestResponse, error) {
	if strings.TrimSpace(input.MigrationsDirectory) == "" || len(input.MigrationsDirectory) > 4000 {
		return DatabaseTestResponse{}, databaseInputValidation(
			"migrationsDirectory",
			"数据库迁移目录不能为空且不能超过 4000 个字符。",
		)
	}
	migrationsDirectory, err := s.resolvePath(input.MigrationsDirectory, "migrationsDirectory")
	if err != nil {
		return DatabaseTestResponse{}, err
	}
	if _, err := database.ReadMigrations(migrationsDirectory); err != nil {
		return DatabaseTestResponse{}, apperror.New(
			apperror.CodeValidationError,
			"数据库迁移目录无效，无法读取迁移记录。",
			apperror.WithCause(err),
			apperror.WithMetadata(map[string]any{
				"fieldErrors": map[string][]string{
					"migrationsDirectory": {"该目录不包含有效的数据库迁移文件"},
				},
			}),
		)
	}
	databaseConfig, err := databaseConfig(input.Database)
	if err != nil {
		return DatabaseTestResponse{}, err
	}
	started := time.Now()
	connection, err := s.databases.Open(ctx, databaseConfig)
	if err != nil {
		return DatabaseTestResponse{}, databaseConnectionFailure(err)
	}
	defer connection.Close()
	if err := connection.Ping(ctx); err != nil {
		return DatabaseTestResponse{}, databaseConnectionFailure(err)
	}
	canCreate, err := connection.CanCreateInCurrentSchema(ctx)
	if err != nil {
		return DatabaseTestResponse{}, databasePermissionCheckFailure(err)
	}
	if !canCreate {
		return DatabaseTestResponse{}, databasePermissionDenied(nil)
	}
	if err := connection.CheckMigrationCompatibility(ctx, migrationsDirectory); err != nil {
		return DatabaseTestResponse{}, databaseMigrationCompatibilityFailure(err)
	}
	inspection, err := connection.Inspect(ctx, migrationsDirectory)
	if err != nil {
		return DatabaseTestResponse{}, databaseInspectionFailure(err)
	}
	return DatabaseTestResponse{
		OK: true, ServerTimeMS: max(0, time.Since(started).Milliseconds()), DatabaseInspection: inspection,
	}, nil
}

func (s *Service) TestStorage(ctx context.Context, input StorageInput) (StorageTestResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return StorageTestResponse{}, err
	}
	return s.testStorage(ctx, input)
}

func (s *Service) testStorage(ctx context.Context, input StorageInput) (StorageTestResponse, error) {
	storageConfig, err := storageConfig(input)
	if err != nil {
		return StorageTestResponse{}, err
	}
	objects, err := s.objectStorage.Open(storageConfig)
	if err != nil {
		return StorageTestResponse{}, storageFailure("Object storage client could not be created", err)
	}
	defer objects.Close()
	inspection, err := objects.Inspect(ctx)
	if err != nil {
		return StorageTestResponse{}, storageFailure("Object storage could not be inspected", err)
	}
	return StorageTestResponse{OK: true, StorageInspection: inspection}, nil
}

func (s *Service) TestMedia(ctx context.Context, input MediaInput) (MediaTestResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return MediaTestResponse{}, err
	}
	return s.testMedia(ctx, input)
}

func (s *Service) testMedia(ctx context.Context, input MediaInput) (MediaTestResponse, error) {
	paths, err := s.resolveMediaPaths(input)
	if err != nil {
		return MediaTestResponse{}, err
	}
	ffmpeg, err := s.mediaTool.Version(ctx, paths.FFmpegPath, "ffmpeg")
	if err != nil {
		return MediaTestResponse{}, err
	}
	ffprobe, err := s.mediaTool.Version(ctx, paths.FFprobePath, "ffprobe")
	if err != nil {
		return MediaTestResponse{}, err
	}
	var fpcalc *string
	fpcalcPath, err := s.resolveOptionalMediaPath(input.FPcalcPath, "media.fpcalcPath")
	if err != nil {
		return MediaTestResponse{}, err
	}
	if fpcalcPath != "" {
		version, err := s.mediaTool.Version(ctx, fpcalcPath, "fpcalc")
		if err != nil {
			return MediaTestResponse{}, err
		}
		fpcalc = &version
	}
	return MediaTestResponse{
		OK:                    true,
		FFmpeg:                ffmpeg,
		FFprobe:               ffprobe,
		FPcalc:                fpcalc,
		Paths:                 paths,
		FPcalcDescription:     fpcalcDescription,
		FingerprintConfigured: fpcalcPath != "" && optionalTrim(input.AcoustIDClient) != "",
	}, nil
}

func (s *Service) TestSource(ctx context.Context, input SourceInput) (SourceTestResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return SourceTestResponse{}, err
	}
	return s.testSource(ctx, input)
}

func (s *Service) testSource(ctx context.Context, input SourceInput) (SourceTestResponse, error) {
	source, err := s.sourceValidator.Validate(ctx, input, s.root)
	if err != nil {
		return SourceTestResponse{}, err
	}
	return SourceTestResponse{OK: true, Directory: source.Path}, nil
}

func (s *Service) TestAdministrator(_ context.Context, input AdministratorInput) (OKResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return OKResponse{}, err
	}
	if err := validateAdministrator(input); err != nil {
		return OKResponse{}, err
	}
	return OKResponse{OK: true}, nil
}

func (s *Service) Complete(ctx context.Context, input SetupInput, traceID string) (CompletionResponse, error) {
	s.transition.Lock()
	defer s.transition.Unlock()
	result, err := s.complete(ctx, input, traceID)
	if err != nil {
		return CompletionResponse{}, normalizeCompletionError(err)
	}
	return result, nil
}

func (s *Service) complete(ctx context.Context, input SetupInput, traceID string) (CompletionResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return CompletionResponse{}, err
	}
	if s.runtime.Status().Phase == RuntimePhaseReady {
		return CompletionResponse{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"初始化已经完成，不能再次提交初始化配置。",
			nil,
		)
	}
	if input.DatabaseAction != "" && input.DatabaseAction != databaseActionReusePartial &&
		input.DatabaseAction != databaseActionMigrate && input.DatabaseAction != databaseActionReset {
		return CompletionResponse{}, apperror.Validation("数据库处理方式无效", map[string][]string{
			"databaseAction": {"请选择复用部分数据、迁移并复用或全部清除"},
		})
	}
	if input.StorageAction != "" && input.StorageAction != storageActionReuse && input.StorageAction != storageActionReset {
		return CompletionResponse{}, apperror.Validation("对象存储处理方式无效", map[string][]string{
			"storageAction": {"请选择继续复用 Bucket 或全部清除 Bucket"},
		})
	}

	var candidate config.Config
	var source ValidatedSource
	if err := runSetupStage("configuration_validation", func() error {
		if input.DatabaseAction != databaseActionReusePartial && input.DatabaseAction != databaseActionMigrate {
			if err := validateAdministrator(input.Administrator); err != nil {
				return err
			}
		}
		if err := validateFingerprintConfiguration(input.Media); err != nil {
			return err
		}
		var err error
		source, err = s.sourceValidator.Validate(ctx, input.Source, s.root)
		if err != nil {
			return err
		}
		candidate, err = s.buildConfig(input)
		return err
	}); err != nil {
		return CompletionResponse{}, err
	}

	probes := []struct {
		stage string
		run   func() error
	}{
		{"http_probe", func() error { _, err := s.testHTTP(ctx, input.HTTP); return err }},
		{"paths_probe", func() error { _, err := s.testPaths(ctx, input.Paths); return err }},
		{"database_probe", func() error {
			_, err := s.testDatabase(ctx, DatabaseTestInput{Database: input.Database, MigrationsDirectory: input.Paths.MigrationsDirectory})
			return err
		}},
		{"storage_probe", func() error { _, err := s.testStorage(ctx, input.Storage); return err }},
		{"media_probe", func() error { _, err := s.testMedia(ctx, input.Media); return err }},
		{"source_probe", func() error { _, err := s.testSource(ctx, input.Source); return err }},
	}
	for _, probe := range probes {
		if err := runSetupStage(probe.stage, probe.run); err != nil {
			return CompletionResponse{}, err
		}
	}

	var exists bool
	if err := runSetupStage("configuration_check", func() error {
		_, configured, err := s.store.Load(ctx)
		exists = configured
		return err
	}); err != nil {
		return CompletionResponse{}, err
	}
	if exists {
		s.markConfigured()
		return CompletionResponse{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"初始化已经完成，不能再次提交初始化配置。",
			nil,
		)
	}

	var connection InstallationDatabase
	if err := runSetupStage("database_open", func() error {
		var err error
		connection, err = s.databases.Open(ctx, candidate.Database)
		if err != nil {
			return databaseConnectionFailure(err)
		}
		return nil
	}); err != nil {
		return CompletionResponse{}, err
	}
	defer connection.Close()
	migrationsDirectory, err := s.resolvePath(candidate.Paths.MigrationsDirectory, "MIGRATIONS_DIRECTORY")
	if err != nil {
		return CompletionResponse{}, err
	}

	var provisioned *ProvisionedInstallation
	var objects SetupObjectStorage
	var bucketCreated bool
	var databaseInspection InstallationInspection
	var storageInspection StorageInspection
	runtimeInitialized := false
	configurationSaved := false
	destructiveStageStarted := false
	operationErr := func() error {
		if err := runSetupStage("storage_prepare", func() error {
			var err error
			objects, err = s.objectStorage.Open(candidate.Storage)
			if err != nil {
				return storageFailure("Object storage client could not be created", err)
			}
			storageInspection, err = objects.Inspect(ctx)
			if err != nil {
				return storageFailure("Object storage contents could not be inspected", err)
			}
			if storageInspection.HasObjects && input.StorageAction == "" {
				return apperror.New(
					apperror.CodeSetupDecisionRequired,
					"目标 MinIO Bucket 已包含对象，必须先选择继续复用或全部清除。",
					apperror.WithMetadata(map[string]any{"decisionResource": "storage"}),
				)
			}
			bucketCreated, err = objects.EnsureBucket(ctx)
			if err != nil {
				return storageFailure("Object storage bucket could not be prepared", err)
			}
			if err := objects.VerifyReadWrite(ctx); err != nil {
				return storageFailure("Object storage read/write verification failed", err)
			}
			return nil
		}); err != nil {
			return err
		}
		var inspectErr error
		databaseInspection, inspectErr = connection.Inspect(ctx, migrationsDirectory)
		if inspectErr != nil {
			return runSetupStage("database_inspection", func() error {
				return databaseInspectionFailure(inspectErr)
			})
		}
		if err := validateDatabaseDecision(databaseInspection, input.DatabaseAction); err != nil {
			return err
		}
		if err := runSetupStage("database_migration", func() error {
			err := connection.RunMigrations(ctx, migrationsDirectory)
			if err != nil {
				return databaseMigrationFailure(err)
			}
			return nil
		}); err != nil {
			return err
		}
		if input.DatabaseAction == databaseActionReset {
			destructiveStageStarted = true
			if err := runSetupStage("database_clear", func() error { return connection.Reset(ctx) }); err != nil {
				return err
			}
		}
		if storageInspection.HasObjects && input.StorageAction == storageActionReset {
			destructiveStageStarted = true
			if err := runSetupStage("storage_clear", func() error { return objects.Clear(ctx) }); err != nil {
				return err
			}
		}
		if storageInspection.HasObjects && input.StorageAction != storageActionReuse && input.StorageAction != storageActionReset {
			return apperror.New(
				apperror.CodeSetupDecisionRequired,
				"目标 MinIO Bucket 已包含对象，必须先选择继续复用或全部清除。",
				apperror.WithMetadata(map[string]any{"decisionResource": "storage"}),
			)
		}
		if err := runSetupStage("installation_provision", func() error {
			administrator := AdministratorRecord{}
			reuseDatabase := input.DatabaseAction == databaseActionReusePartial || input.DatabaseAction == databaseActionMigrate
			if !reuseDatabase || !databaseInspection.HasActiveAdministrator {
				if err := validateAdministrator(input.Administrator); err != nil {
					return err
				}
				passwordHash, err := s.passwords.Hash(input.Administrator.Password)
				if err != nil {
					return err
				}
				administrator = AdministratorRecord{
					Username:           strings.TrimSpace(input.Administrator.Username),
					NormalizedUsername: normalizeIdentity(input.Administrator.Username),
					DisplayName:        strings.TrimSpace(input.Administrator.DisplayName),
					PasswordHash:       passwordHash,
				}
			}
			created, err := connection.Provision(ctx, ProvisionInput{
				Administrator: administrator,
				Source:        source,
				ReuseExisting: reuseDatabase,
			})
			if err == nil {
				provisioned = &created
			}
			return err
		}); err != nil {
			return err
		}
		if err := runSetupStage("runtime_initialize", func() error {
			before := s.runtime.Status()
			if err := s.runtime.Initialize(ctx, candidate, RuntimeSourceManaged); err != nil {
				return err
			}
			runtimeInitialized = true
			after := s.runtime.Status()
			if after.Phase != RuntimePhaseReady || after.Source != RuntimeSourceManaged || after.Generation <= before.Generation {
				return errors.New("runtime initialization returned without activating the managed configuration")
			}
			return nil
		}); err != nil {
			return err
		}
		if err := runSetupStage("configuration_save", func() error {
			return s.store.Save(ctx, candidate)
		}); err != nil {
			return err
		}
		configurationSaved = true
		if err := runSetupStage("completion_audit", func() error {
			return connection.RecordSetupSuccess(ctx, provisioned.AdministratorID, traceID, legacyPlatformName())
		}); err != nil {
			return err
		}
		return nil
	}()
	if objects != nil {
		defer objects.Close()
	}

	if operationErr != nil {
		rollbackErrors := make([]error, 0, 4)
		rollbackContext := context.WithoutCancel(ctx)
		if runtimeInitialized {
			if err := s.runtime.Close(rollbackContext); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("close initialized runtime: %w", err))
			}
		}
		if configurationSaved {
			if err := s.store.Clear(rollbackContext); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("clear saved configuration: %w", err))
			}
		}
		if provisioned != nil {
			if err := connection.Compensate(rollbackContext, *provisioned, traceID); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("compensate installation data: %w", err))
			}
		}
		if bucketCreated && objects != nil {
			if err := objects.RemoveBucket(rollbackContext); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove setup-created bucket: %w", err))
			}
		}
		if len(rollbackErrors) > 0 {
			return CompletionResponse{}, &setupRollbackError{primary: operationErr, rollback: rollbackErrors}
		}
		if destructiveStageStarted {
			stage := setupErrorStage(operationErr)
			return CompletionResponse{}, apperror.New(
				apperror.CodeSetupFailed,
				setupStageDetail(stage, false)+" 数据清除阶段已经开始，原有数据可能已无法恢复。",
				apperror.WithCause(operationErr),
				apperror.WithMetadata(map[string]any{
					"setupStage":              stage,
					"destructiveStageStarted": true,
				}),
			)
		}
		return CompletionResponse{}, operationErr
	}

	s.markConfigured()
	restartFields := make([]string, 0, 4)
	if candidate.HTTP.IPv4Host != s.actualListener.IPv4.Host {
		restartFields = append(restartFields, "http.ipv4Host")
	}
	if candidate.HTTP.IPv4Port != s.actualListener.IPv4.Port {
		restartFields = append(restartFields, "http.ipv4Port")
	}
	if candidate.HTTP.IPv6Host != s.actualListener.IPv6.Host {
		restartFields = append(restartFields, "http.ipv6Host")
	}
	if candidate.HTTP.IPv6Port != s.actualListener.IPv6.Port {
		restartFields = append(restartFields, "http.ipv6Port")
	}
	return CompletionResponse{
		Configured:            true,
		RuntimeGeneration:     s.runtime.Status().Generation,
		ActualListener:        s.actualListener,
		RestartRequiredFields: restartFields,
	}, nil
}

func (s *Service) buildConfig(input SetupInput) (config.Config, error) {
	if err := validateHTTP(input.HTTP); err != nil {
		return config.Config{}, err
	}
	if _, err := s.resolvePaths(input.Paths); err != nil {
		return config.Config{}, err
	}
	databaseValue, err := databaseConfig(input.Database)
	if err != nil {
		return config.Config{}, err
	}
	storageValue, err := storageConfig(input.Storage)
	if err != nil {
		return config.Config{}, err
	}
	mediaPaths, err := s.resolveMediaPaths(input.Media)
	if err != nil {
		return config.Config{}, err
	}
	if input.Registration.Enabled == nil {
		return config.Config{}, apperror.Validation("Registration enabled flag is required")
	}
	accessTokenSecret, err := s.secretGenerator()
	if err != nil {
		return config.Config{}, fmt.Errorf("generate access token secret: %w", err)
	}
	idempotencySecret, err := s.secretGenerator()
	if err != nil {
		return config.Config{}, fmt.Errorf("generate idempotency secret: %w", err)
	}
	cursorSecret, err := s.secretGenerator()
	if err != nil {
		return config.Config{}, fmt.Errorf("generate cursor secret: %w", err)
	}
	mediaDirectory := config.DefaultMediaToolsDirectory
	mediaMode := "ADVANCED"
	ffmpegConfigured := strings.TrimSpace(valueOrEmpty(input.Media.FFmpegPath))
	ffprobeConfigured := strings.TrimSpace(valueOrEmpty(input.Media.FFprobePath))
	if input.Media.Directory != nil {
		configuredDirectory := strings.TrimSpace(*input.Media.Directory)
		if configuredDirectory == "" {
			ffmpegConfigured = mediaPaths.FFmpegPath
			ffprobeConfigured = mediaPaths.FFprobePath
		} else {
			mediaDirectory = configuredDirectory
			mediaMode = "DIRECTORY"
			ffmpegConfigured = configuredDirectoryToolPath(mediaDirectory, mediaPaths.FFmpegPath)
			ffprobeConfigured = configuredDirectoryToolPath(mediaDirectory, mediaPaths.FFprobePath)
		}
	}
	ipv4Listener, ipv6Listener := normalizedHTTPListeners(input.HTTP)
	candidate := config.Config{
		Environment: config.Production,
		Paths: config.Paths{
			MigrationsDirectory: strings.TrimSpace(input.Paths.MigrationsDirectory),
			AdminWebDirectory:   strings.TrimSpace(input.Paths.AdminWebDirectory),
			MediaToolsDirectory: mediaDirectory,
			LocalMusicDirectory: strings.TrimSpace(input.Source.Directory),
		},
		HTTP: config.HTTP{
			IPv4Host:              ipv4Listener.Host,
			IPv4Port:              ipv4Listener.Port,
			IPv6Host:              ipv6Listener.Host,
			IPv6Port:              ipv6Listener.Port,
			Host:                  ipv4Listener.Host,
			Port:                  ipv4Listener.Port,
			TrustedProxyAddresses: trimmedStrings(input.HTTP.TrustedProxyAddresses),
		},
		Database: databaseValue,
		Security: config.Security{
			AccessTokenSecret:           accessTokenSecret,
			IdempotencyEncryptionSecret: idempotencySecret,
			CursorSigningSecret:         cursorSecret,
			AccessTokenTTLSeconds:       900,
			RefreshTokenTTLSeconds:      2_592_000,
		},
		Storage: storageValue,
		Media: config.Media{
			Mode:        mediaMode,
			FFmpegPath:  ffmpegConfigured,
			FFprobePath: ffprobeConfigured,
		},
		Scraping: config.Scraping{
			FPcalcPath:     optionalTrim(input.Media.FPcalcPath),
			AcoustIDClient: optionalTrim(input.Media.AcoustIDClient),
		},
		LocalLibrary: config.LocalLibrary{
			Name:                strings.TrimSpace(input.Source.Name),
			Directory:           strings.TrimSpace(input.Source.Directory),
			Mode:                input.Source.Mode,
			Enabled:             *input.Source.Enabled,
			SyncOnStartup:       *input.Source.SyncOnStartup,
			ScanIntervalMinutes: cloneInt(input.Source.ScanIntervalMinutes),
			IncludePatterns:     trimmedStrings(input.Source.IncludePatterns),
			ExcludePatterns:     trimmedStrings(input.Source.ExcludePatterns),
		},
		Registration: config.Registration{Enabled: *input.Registration.Enabled},
	}
	validated, err := config.Parse(config.ToEnvironment(candidate))
	if err != nil {
		return config.Config{}, apperror.New(
			apperror.CodeValidationError,
			"初始化配置内容无效，请检查标记字段后重试。",
			apperror.WithCause(err),
		)
	}
	return validated, nil
}

func (s *Service) resolvePaths(input PathsInput) (ResolvedPaths, error) {
	if strings.TrimSpace(input.MigrationsDirectory) == "" || len(input.MigrationsDirectory) > 4000 {
		return ResolvedPaths{}, databaseInputValidation(
			"migrationsDirectory",
			"数据库迁移目录不能为空且不能超过 4000 个字符。",
		)
	}
	if strings.TrimSpace(input.AdminWebDirectory) == "" || len(input.AdminWebDirectory) > 4000 {
		return ResolvedPaths{}, apperror.Validation("Admin web directory is invalid")
	}
	migrations, err := s.resolvePath(input.MigrationsDirectory, "paths.migrationsDirectory")
	if err != nil {
		return ResolvedPaths{}, err
	}
	admin, err := s.resolvePath(input.AdminWebDirectory, "paths.adminWebDirectory")
	if err != nil {
		return ResolvedPaths{}, err
	}
	return ResolvedPaths{MigrationsDirectory: migrations, AdminWebDirectory: admin}, nil
}

func (s *Service) resolveMediaPaths(input MediaInput) (ResolvedMediaPaths, error) {
	if input.Directory != nil {
		if input.FFmpegPath != nil || input.FFprobePath != nil {
			return ResolvedMediaPaths{}, apperror.Validation("Media tools directory cannot be combined with explicit executable paths")
		}
		directory := strings.TrimSpace(*input.Directory)
		if len(directory) > 2000 {
			return ResolvedMediaPaths{}, apperror.Validation("Media tools directory is invalid")
		}
		if directory == "" {
			return ResolvedMediaPaths{FFmpegPath: "ffmpeg", FFprobePath: "ffprobe"}, nil
		}
		resolved, err := s.resolvePath(directory, "media.directory")
		if err != nil {
			return ResolvedMediaPaths{}, err
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return ResolvedMediaPaths{}, apperror.New(
				apperror.CodeValidationError,
				"Media tools directory does not exist or is not a directory",
				apperror.WithCause(err),
			)
		}
		ffmpeg, err := detectExecutable(resolved, "ffmpeg")
		if err != nil {
			return ResolvedMediaPaths{}, err
		}
		ffprobe, err := detectExecutable(resolved, "ffprobe")
		if err != nil {
			return ResolvedMediaPaths{}, err
		}
		return ResolvedMediaPaths{FFmpegPath: ffmpeg, FFprobePath: ffprobe}, nil
	}
	ffmpeg, err := s.resolveRequiredMediaPath(input.FFmpegPath, "ffmpeg")
	if err != nil {
		return ResolvedMediaPaths{}, err
	}
	ffprobe, err := s.resolveRequiredMediaPath(input.FFprobePath, "ffprobe")
	if err != nil {
		return ResolvedMediaPaths{}, err
	}
	return ResolvedMediaPaths{FFmpegPath: ffmpeg, FFprobePath: ffprobe}, nil
}

func (s *Service) resolveRequiredMediaPath(input *string, label string) (string, error) {
	if input == nil || strings.TrimSpace(*input) == "" {
		return label, nil
	}
	candidate := strings.TrimSpace(*input)
	if len(candidate) > 2000 {
		return "", apperror.Validation(label + " path is too long")
	}
	if filepath.Base(candidate) == candidate && filepath.VolumeName(candidate) == "" {
		return candidate, nil
	}
	return s.resolvePath(candidate, "media."+label+"Path")
}

func (s *Service) resolveOptionalMediaPath(input *string, field string) (string, error) {
	if input == nil || strings.TrimSpace(*input) == "" {
		return "", nil
	}
	if len(*input) > 2000 {
		return "", apperror.Validation(field + " is too long")
	}
	return s.resolvePath(*input, field)
}

func (s *Service) resolvePath(candidate, field string) (string, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", apperror.Validation(field + " must not be empty")
	}
	if len(candidate) > 4000 {
		return "", apperror.Validation(field + " is too long")
	}
	if filepath.IsAbs(candidate) {
		return filepath.Clean(candidate), nil
	}
	return filepath.Join(s.root, candidate), nil
}

func (s *Service) markConfigured() {
	s.stateMu.Lock()
	s.configured = true
	s.stateMu.Unlock()
}

func BuildSetupDatabaseURL(input DatabaseInput) (string, error) {
	databaseName := strings.TrimSpace(input.Database)
	username := strings.TrimSpace(input.Username)
	if databaseName == "" || len(databaseName) > 255 {
		return "", databaseInputValidation("database", "数据库名不能为空且不能超过 255 个字符。")
	}
	if username == "" || len(username) > 255 {
		return "", databaseInputValidation("username", "数据库用户名不能为空且不能超过 255 个字符。")
	}
	if input.Password == "" || len(input.Password) > 2000 {
		return "", databaseInputValidation("password", "数据库密码不能为空且不能超过 2000 个字符。")
	}
	if input.Port < 1 || input.Port > 65535 {
		return "", databaseInputValidation("port", "数据库端口必须是 1 到 65535 之间的整数。")
	}
	if input.MaxConnections < 1 || input.MaxConnections > 100 {
		return "", databaseInputValidation("maxConnections", "数据库最大连接数必须是 1 到 100 之间的整数。")
	}
	switch input.SSLMode {
	case "disable", "prefer", "require", "verify-full":
	default:
		return "", databaseInputValidation("sslMode", "数据库 SSL 模式无效。")
	}
	host, err := parseDatabaseHost(input.Host)
	if err != nil {
		return "", err
	}
	value := &url.URL{
		Scheme:  "postgresql",
		User:    url.UserPassword(username, input.Password),
		Host:    net.JoinHostPort(host, strconv.Itoa(input.Port)),
		Path:    "/" + databaseName,
		RawPath: "/" + url.PathEscape(databaseName),
	}
	query := value.Query()
	query.Set("sslmode", input.SSLMode)
	value.RawQuery = query.Encode()
	return value.String(), nil
}

func databaseConfig(input DatabaseInput) (config.Database, error) {
	value, err := BuildSetupDatabaseURL(input)
	if err != nil {
		return config.Database{}, err
	}
	return config.Database{URL: value, MaxConnections: int32(input.MaxConnections)}, nil
}

func databaseInputValidation(field, detail string) error {
	return apperror.Validation(detail, map[string][]string{field: {detail}})
}

func storageConfig(input StorageInput) (config.Storage, error) {
	endpoint, err := requiredHTTPURL(input.Endpoint, "Storage endpoint", false)
	if err != nil {
		return config.Storage{}, err
	}
	publicBaseURL := ""
	if input.PublicBaseURL != nil && strings.TrimSpace(*input.PublicBaseURL) != "" {
		publicBaseURL, err = requiredHTTPURL(*input.PublicBaseURL, "Storage public base URL", true)
		if err != nil {
			return config.Storage{}, err
		}
	}
	if strings.TrimSpace(input.Region) == "" || len(input.Region) > 100 {
		return config.Storage{}, apperror.Validation("Storage region is invalid")
	}
	if strings.TrimSpace(input.Bucket) == "" || len(input.Bucket) > 255 {
		return config.Storage{}, apperror.Validation("Storage bucket is invalid")
	}
	if strings.TrimSpace(input.AccessKeyID) == "" || len(input.AccessKeyID) > 500 {
		return config.Storage{}, apperror.Validation("Storage access key is invalid")
	}
	if input.SecretAccessKey == "" || len(input.SecretAccessKey) > 2000 {
		return config.Storage{}, apperror.Validation("Storage secret key is invalid")
	}
	if input.ForcePathStyle == nil {
		return config.Storage{}, apperror.Validation("Storage path-style flag is required")
	}
	if input.SignedURLTTLSeconds < 30 || input.SignedURLTTLSeconds > 3600 {
		return config.Storage{}, apperror.Validation("Storage signed URL TTL is invalid")
	}
	if input.MaxUploadBytes < 1 || input.MaxUploadBytes > config.MaxServerRequestBodyBytes {
		return config.Storage{}, apperror.Validation("Storage maximum upload size is invalid")
	}
	return config.Storage{
		Endpoint:            endpoint,
		PublicBaseURL:       publicBaseURL,
		Region:              strings.TrimSpace(input.Region),
		Bucket:              strings.TrimSpace(input.Bucket),
		AccessKeyID:         input.AccessKeyID,
		SecretAccessKey:     input.SecretAccessKey,
		ForcePathStyle:      *input.ForcePathStyle,
		SignedURLTTLSeconds: input.SignedURLTTLSeconds,
		MaxUploadBytes:      input.MaxUploadBytes,
	}, nil
}

func validateHTTP(input HTTPInput) error {
	ipv4, ipv6 := normalizedHTTPListeners(input)
	if address := net.ParseIP(ipv4.Host); address == nil || address.To4() == nil {
		return apperror.Validation("IPv4 监听 IP 无效，请填写 IPv4 地址。", map[string][]string{
			"ipv4Host": {"请输入有效的 IPv4 地址"},
		})
	}
	if address := net.ParseIP(ipv6.Host); address == nil || address.To4() != nil {
		return apperror.Validation("IPv6 监听 IP 无效，请填写 IPv6 地址。", map[string][]string{
			"ipv6Host": {"请输入有效的 IPv6 地址"},
		})
	}
	if ipv4.Port < 1 || ipv4.Port > 65535 {
		return apperror.Validation("IPv4 监听端口必须在 1 到 65535 之间。", map[string][]string{
			"ipv4Port": {"端口必须在 1 到 65535 之间"},
		})
	}
	if ipv6.Port < 1 || ipv6.Port > 65535 {
		return apperror.Validation("IPv6 监听端口必须在 1 到 65535 之间。", map[string][]string{
			"ipv6Port": {"端口必须在 1 到 65535 之间"},
		})
	}
	if len(input.TrustedProxyAddresses) > 100 {
		return apperror.Validation("反向代理 IP 不能超过 100 个。", map[string][]string{
			"trustedProxyAddresses": {"最多填写 100 个反向代理 IP"},
		})
	}
	for _, address := range input.TrustedProxyAddresses {
		if net.ParseIP(strings.TrimSpace(address)) == nil {
			return apperror.Validation("反向代理地址必须是有效的 IPv4 或 IPv6 地址。", map[string][]string{
				"trustedProxyAddresses": {"请输入有效的 IPv4 或 IPv6 地址"},
			})
		}
	}
	return nil
}

func normalizedHTTPListeners(input HTTPInput) (ListenerAddress, ListenerAddress) {
	ipv4Host := strings.Trim(strings.TrimSpace(input.IPv4Host), "[]")
	if ipv4Host == "" {
		legacyHost := strings.Trim(strings.TrimSpace(input.Host), "[]")
		if address := net.ParseIP(legacyHost); address != nil && address.To4() != nil {
			ipv4Host = legacyHost
		} else {
			ipv4Host = "0.0.0.0"
		}
	}
	ipv6Host := strings.Trim(strings.TrimSpace(input.IPv6Host), "[]")
	if ipv6Host == "" {
		legacyHost := strings.Trim(strings.TrimSpace(input.Host), "[]")
		if address := net.ParseIP(legacyHost); address != nil && address.To4() == nil {
			ipv6Host = legacyHost
		} else {
			ipv6Host = "::"
		}
	}
	ipv4Port := input.IPv4Port
	if ipv4Port == 0 {
		ipv4Port = input.Port
	}
	if ipv4Port == 0 {
		ipv4Port = 3000
	}
	ipv6Port := input.IPv6Port
	if ipv6Port == 0 {
		ipv6Port = input.Port
	}
	if ipv6Port == 0 {
		ipv6Port = 3000
	}
	return ListenerAddress{Host: ipv4Host, Port: ipv4Port}, ListenerAddress{Host: ipv6Host, Port: ipv6Port}
}

func validateAdministrator(input AdministratorInput) error {
	username := strings.TrimSpace(input.Username)
	if len(username) < 3 || len(username) > 32 {
		return apperror.Validation("Administrator username must contain 3 to 32 letters, numbers or underscores")
	}
	for _, character := range username {
		if !(character >= 'A' && character <= 'Z') && !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '_' {
			return apperror.Validation("Administrator username must contain 3 to 32 letters, numbers or underscores")
		}
	}
	passwordLength := utf8.RuneCountInString(input.Password)
	if passwordLength < 6 || passwordLength > 128 {
		return apperror.Validation("Administrator password must contain 6 to 128 characters")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > 64 {
		return apperror.Validation("Administrator display name must contain 1 to 64 characters")
	}
	return nil
}

func validateFingerprintConfiguration(input MediaInput) error {
	fpcalc := optionalTrim(input.FPcalcPath)
	client := optionalTrim(input.AcoustIDClient)
	if (fpcalc != "") != (client != "") {
		return apperror.Validation("启用音频指纹时必须同时配置 fpcalc 路径和 AcoustID Client ID")
	}
	if len(client) > 500 {
		return apperror.Validation("AcoustID Client ID is too long")
	}
	return nil
}

func parseDatabaseHost(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" || len(candidate) > 255 || strings.ContainsAny(candidate, " \t\r\n/@?#") {
		return "", databaseInputValidation("host", "数据库地址无效，请填写 IP 或主机名，不要包含端口或协议。")
	}
	if strings.HasPrefix(candidate, "[") || strings.HasSuffix(candidate, "]") {
		if !strings.HasPrefix(candidate, "[") || !strings.HasSuffix(candidate, "]") || len(candidate) < 3 {
			return "", databaseInputValidation("host", "数据库 IPv6 地址格式无效。")
		}
		candidate = candidate[1 : len(candidate)-1]
		if strings.ContainsAny(candidate, "[]") || net.ParseIP(candidate) == nil || !strings.Contains(candidate, ":") {
			return "", databaseInputValidation("host", "数据库 IPv6 地址格式无效。")
		}
		return strings.ToLower(candidate), nil
	}
	if address := net.ParseIP(candidate); address != nil {
		return strings.ToLower(candidate), nil
	}
	if strings.Contains(candidate, ":") {
		return "", databaseInputValidation("host", "数据库地址不能包含端口；请在端口字段单独填写。")
	}
	hostname, err := idna.Lookup.ToASCII(candidate)
	if err != nil {
		return "", databaseInputValidation("host", "数据库主机名格式无效。")
	}
	hostname = strings.ToLower(hostname)
	if hostname == "" || len(hostname) > 253 || strings.HasSuffix(hostname, ".") {
		return "", databaseInputValidation("host", "数据库主机名格式无效。")
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", databaseInputValidation("host", "数据库主机名格式无效。")
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '-' {
				return "", databaseInputValidation("host", "数据库主机名格式无效。")
			}
		}
	}
	return hostname, nil
}

func requiredHTTPURL(raw, label string, allowPath bool) (string, error) {
	candidate := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", apperror.Validation(label + " must use http:// or https://")
	}
	if !allowPath && parsed.Path != "" {
		return "", apperror.Validation(label + " must not contain a path")
	}
	if len(candidate) < 8 || len(candidate) > 2000 {
		return "", apperror.Validation(label + " is invalid")
	}
	hostname := strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	if hostname == "localhost" {
		return "", apperror.Validation(label + " 不能使用 localhost，客户端无法通过该地址访问对象存储")
	}
	if address := net.ParseIP(hostname); address != nil && (address.IsLoopback() || address.IsUnspecified()) {
		return "", apperror.Validation(label + " 不能使用回环或未指定地址，请填写客户端可直接访问的局域网 IP 或域名")
	}
	return candidate, nil
}

func detectExecutable(directory, name string) (string, error) {
	candidates := []string{name, name + ".exe"}
	if runtime.GOOS == "windows" {
		candidates[0], candidates[1] = candidates[1], candidates[0]
	}
	for _, candidate := range candidates {
		path := filepath.Join(directory, candidate)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", apperror.Validation("Media tools directory does not contain the " + name + " executable")
}

func configuredDirectoryToolPath(directory, detected string) string {
	if filepath.IsAbs(directory) {
		return detected
	}
	return filepath.Join(directory, filepath.Base(detected))
}

type setupStageError struct {
	stage string
	cause error
}

func (e *setupStageError) Error() string { return "initial setup failed during " + e.stage }
func (e *setupStageError) Unwrap() error { return e.cause }

type setupRollbackError struct {
	primary  error
	rollback []error
}

func (e *setupRollbackError) Error() string {
	return "initial setup failed and rollback could not be completed"
}

func (e *setupRollbackError) Unwrap() []error {
	return append([]error{e.primary}, e.rollback...)
}

func runSetupStage(stage string, operation func() error) error {
	err := operation()
	if err == nil {
		return nil
	}
	if applicationError, ok := apperror.As(err); ok {
		metadata := make(map[string]any, len(applicationError.Metadata)+1)
		for key, value := range applicationError.Metadata {
			metadata[key] = value
		}
		if _, exists := metadata["setupStage"]; !exists {
			metadata["setupStage"] = stage
		}
		return apperror.New(
			applicationError.Code,
			applicationError.Detail,
			apperror.WithCause(err),
			apperror.WithMetadata(metadata),
		)
	}
	var staged *setupStageError
	if errors.As(err, &staged) {
		return err
	}
	return &setupStageError{stage: stage, cause: err}
}

func normalizeCompletionError(err error) error {
	if _, ok := err.(*apperror.Error); ok {
		return err
	}
	if errors.Is(err, ErrInvalidConfiguration) {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"检测到已有但无效的 .env 文件，请修复或删除该文件后重新初始化。",
			nil,
		)
	}
	stage := setupErrorStage(err)
	_, rollbackIncomplete := err.(*setupRollbackError)
	return apperror.New(
		apperror.CodeSetupFailed,
		setupStageDetail(stage, rollbackIncomplete),
		apperror.WithCause(err),
		apperror.WithMetadata(map[string]any{
			"setupStage":         stage,
			"rollbackIncomplete": rollbackIncomplete,
		}),
	)
}

func setupStageDetail(stage string, rollbackIncomplete bool) string {
	labels := map[string]string{
		"configuration_validation": "配置内容校验",
		"http_probe":               "HTTP 监听地址检查",
		"paths_probe":              "运行目录检查",
		"database_probe":           "数据库连接和权限检查",
		"storage_probe":            "对象存储连接检查",
		"media_probe":              "媒体工具检查",
		"source_probe":             "音乐目录检查",
		"configuration_check":      "已有配置文件检查",
		"database_open":            "数据库连接",
		"storage_prepare":          "对象存储 Bucket 准备",
		"database_migration":       "数据库迁移",
		"database_inspection":      "已有数据库配置检查",
		"storage_clear":            "对象存储清除",
		"database_clear":           "数据库数据清除",
		"installation_provision":   "管理员和音源配置",
		"runtime_initialize":       "服务运行时启动",
		"configuration_save":       "配置文件保存",
		"completion_audit":         "初始化审计记录写入",
	}
	label := labels[stage]
	if label == "" {
		label = "未知步骤"
	}
	detail := "初始化在“" + label + "”阶段失败，系统未能完成配置。"
	if rollbackIncomplete {
		detail += " 自动回滚未完整完成，请勿重复初始化，先使用追踪 ID 检查服务端日志。"
	}
	return detail
}

func validateDatabaseDecision(inspection InstallationInspection, action string) error {
	switch inspection.State {
	case DatabaseStateEmpty:
		if action == "" {
			return nil
		}
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"数据库状态已经变化，当前数据库为空，请重新验证后继续。",
			nil,
		)
	case DatabaseStatePartial:
		if action == databaseActionReusePartial || action == databaseActionReset {
			return nil
		}
		return apperror.New(
			apperror.CodeSetupDecisionRequired,
			"数据库包含部分 XyMusic 配置，请选择复用部分数据或全部清除。",
			apperror.WithMetadata(map[string]any{
				"decisionResource": "database", "databaseState": inspection.State,
				"reusable": inspection.Reusable, "missing": inspection.Missing,
			}),
		)
	case DatabaseStateComplete:
		if action == databaseActionMigrate || action == databaseActionReset {
			return nil
		}
		return apperror.New(
			apperror.CodeSetupDecisionRequired,
			"检测到完整的 XyMusic 数据库，请选择迁移并复用现有数据或全部清除。",
			apperror.WithMetadata(map[string]any{
				"decisionResource": "database", "databaseState": inspection.State,
				"migrationRequired": inspection.MigrationRequired,
				"reusable":          inspection.Reusable, "missing": inspection.Missing,
			}),
		)
	default:
		return apperror.New(apperror.CodeSetupFailed, "无法识别数据库配置状态，请检查迁移记录后重试。")
	}
}

func setupErrorStage(err error) string {
	if applicationError, ok := apperror.As(err); ok {
		if stage, valid := applicationError.Metadata["setupStage"].(string); valid && stage != "" {
			return stage
		}
	}
	var staged *setupStageError
	if errors.As(err, &staged) {
		return staged.stage
	}
	return "complete"
}

func dependencyFailure(detail string, cause error) error {
	return apperror.New(apperror.CodeDependencyUnavailable, detail, apperror.WithCause(cause))
}

func storageFailure(detail string, cause error) error {
	if _, ok := apperror.As(cause); ok {
		return cause
	}
	return dependencyFailure(detail, cause)
}

func randomSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func legacyPlatformName() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

func normalizeIdentity(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(strings.TrimSpace(value))), " "))
}

func optionalTrim(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func trimmedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return uniqueStrings(result)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
