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

const (
	databaseActionReset = "reset"
	storageActionReset  = "reset"
)



type Options struct {
	RootDirectory       string
	ConfigurationPath   string
	ActualListener      ActualListener
	ConfiguredAtStartup *bool
	Runtime             RuntimeController
	Store               ConfigurationRepository
	Databases           DatabaseFactory
	MediaStorage        MediaStorageFactory
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
	mediaStorage    MediaStorageFactory
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
	mediaStorage := options.MediaStorage
	if mediaStorage == nil {
		mediaStorage = ProductionMediaStorageFactory{}
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
		mediaStorage:    mediaStorage,
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
		return apperror.Forbidden("鍒濆鍖栧凡缁忓畬鎴愶紝涓嶈兘鍐嶆鎵ц鍒濆鍖栨搷浣溿€?")
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
				probe.label+" 鐩戝惉鍦板潃涓嶅彲鐢紝IP 鏃犳硶缁戝畾銆佺鍙ｅ凡琚崰鐢ㄦ垨褰撳墠杩涚▼娌℃湁鐩戝惉鏉冮檺銆?",
				apperror.WithCause(err),
				apperror.WithMetadata(map[string]any{
					"fieldErrors": map[string][]string{
						hostField: {probe.label + " 鐩戝惉 IP 鏃犳硶缁戝畾"},
						portField: {probe.label + " 鐩戝惉绔彛涓嶅彲鐢?"},
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
			"鏁版嵁搴撹縼绉荤洰褰曟棤鏁堬紝鏃犳硶璇诲彇杩佺Щ璁板綍銆?",
			apperror.WithCause(err),
			apperror.WithMetadata(map[string]any{
				"fieldErrors": map[string][]string{
					"migrationsDirectory": {"璇ョ洰褰曚笉鍖呭惈鏈夋晥鐨勬暟鎹簱杩佺Щ鏂囦欢"},
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
			"鏁版嵁搴撹縼绉荤洰褰曚笉鑳戒负绌轰笖涓嶈兘瓒呰繃 4000 涓瓧绗︺€?",
		)
	}
	migrationsDirectory, err := s.resolvePath(input.MigrationsDirectory, "migrationsDirectory")
	if err != nil {
		return DatabaseTestResponse{}, err
	}
	if _, err := database.ReadMigrations(migrationsDirectory); err != nil {
		return DatabaseTestResponse{}, apperror.New(
			apperror.CodeValidationError,
			"鏁版嵁搴撹縼绉荤洰褰曟棤鏁堬紝鏃犳硶璇诲彇杩佺Щ璁板綍銆?",
			apperror.WithCause(err),
			apperror.WithMetadata(map[string]any{
				"fieldErrors": map[string][]string{
					"migrationsDirectory": {"璇ョ洰褰曚笉鍖呭惈鏈夋晥鐨勬暟鎹簱杩佺Щ鏂囦欢"},
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
	storageConfig, err := s.storageConfig(input)
	if err != nil {
		return StorageTestResponse{}, err
	}
	mediaStorage, err := s.mediaStorage.Open(storageConfig)
	if err != nil {
		return StorageTestResponse{}, storageFailure("Local media storage could not be initialized", err)
	}
	defer mediaStorage.Close()
	if err := mediaStorage.EnsureDirectories(ctx); err != nil {
		return StorageTestResponse{}, storageFailure("Media storage directories could not be created", err)
	}
	if err := mediaStorage.VerifyReadWrite(ctx); err != nil {
		return StorageTestResponse{}, storageFailure("Media storage read/write check failed", err)
	}
	inspection, err := mediaStorage.Inspect(ctx)
	if err != nil {
		return StorageTestResponse{}, storageFailure("Media storage could not be inspected", err)
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
	return MediaTestResponse{
		OK:      true,
		FFmpeg:  ffmpeg,
		FFprobe: ffprobe,
		Paths:   paths,
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

func (s *Service) Complete(ctx context.Context, input SetupInput) (CompletionResponse, error) {
	s.transition.Lock()
	defer s.transition.Unlock()
	result, err := s.complete(ctx, input)
	if err != nil {
		return CompletionResponse{}, normalizeCompletionError(err)
	}
	return result, nil
}

func (s *Service) complete(ctx context.Context, input SetupInput) (CompletionResponse, error) {
	if err := s.RequireSetup(); err != nil {
		return CompletionResponse{}, err
	}
	if s.runtime.Status().Phase == RuntimePhaseReady {
		return CompletionResponse{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"鍒濆鍖栧凡缁忓畬鎴愶紝涓嶈兘鍐嶆鎻愪氦鍒濆鍖栭厤缃€?",
			nil,
		)
	}
	if input.DatabaseAction != "" && input.DatabaseAction != databaseActionReset {
		return CompletionResponse{}, apperror.Validation("数据库处理方式无效", map[string][]string{
			"databaseAction": {"仅支持清空数据库 (reset)"},
		})
	}
	if input.StorageAction != "" && input.StorageAction != storageActionReset {
		return CompletionResponse{}, apperror.Validation("存储处理方式无效", map[string][]string{
			"storageAction": {"仅支持清空目录 (reset)"},
		})
	}


	var candidate config.Config
	var source ValidatedSource
	if err := runSetupStage("configuration_validation", func() error {
		if err := validateAdministrator(input.Administrator); err != nil {
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
			"鍒濆鍖栧凡缁忓畬鎴愶紝涓嶈兘鍐嶆鎻愪氦鍒濆鍖栭厤缃€?",
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
	var mediaStorage SetupMediaStorage
	var databaseInspection InstallationInspection
	var storageInspection StorageInspection
	runtimeInitialized := false
	configurationSaved := false
	destructiveStageStarted := false
	operationErr := func() error {
		if err := runSetupStage("storage_prepare", func() error {
			var err error
			mediaStorage, err = s.mediaStorage.Open(candidate.MediaStorage)
			if err != nil {
				return storageFailure("Media storage client could not be created", err)
			}
			storageInspection, err = mediaStorage.Inspect(ctx)
			if err != nil {
				return storageFailure("Media storage contents could not be inspected", err)
			}
			hasExistingFiles := storageInspection.HasAssets || storageInspection.HasTranscode
			if hasExistingFiles && input.StorageAction != storageActionReset {
				return apperror.New(
					apperror.CodeSetupDecisionRequired,
					"检测到媒体资产或转码缓存目录中已存在文件，本项目不支持复用旧文件，必须确认清空目录后继续。",
					apperror.WithMetadata(map[string]any{
						"decisionResource": "storage",
						"hasAssets":        storageInspection.HasAssets,
						"assetCount":       storageInspection.AssetCount,
						"hasTranscode":     storageInspection.HasTranscode,
						"transcodeCount":   storageInspection.TranscodeCount,
					}),
				)
			}
			if err := mediaStorage.EnsureDirectories(ctx); err != nil {
				return storageFailure("Media storage directories could not be prepared", err)
			}
			if err := mediaStorage.VerifyReadWrite(ctx); err != nil {
				return storageFailure("Media storage read/write verification failed", err)
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
		hasExistingStorageFiles := storageInspection.HasAssets || storageInspection.HasTranscode
		if hasExistingStorageFiles && input.StorageAction == storageActionReset {
			destructiveStageStarted = true
			if err := runSetupStage("storage_clear", func() error { return mediaStorage.Clear(ctx) }); err != nil {
				return err
			}
		}

		if err := runSetupStage("installation_provision", func() error {
			if err := validateAdministrator(input.Administrator); err != nil {
				return err
			}
			passwordHash, err := s.passwords.Hash(input.Administrator.Password)
			if err != nil {
				return err
			}
			administrator := AdministratorRecord{
				Username:           strings.TrimSpace(input.Administrator.Username),
				NormalizedUsername: normalizeIdentity(input.Administrator.Username),
				DisplayName:        strings.TrimSpace(input.Administrator.DisplayName),
				PasswordHash:       passwordHash,
			}
			created, err := connection.Provision(ctx, ProvisionInput{
				Administrator: administrator,
				Source:        source,
				ReuseExisting: false,
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
		return nil
	}()
	if mediaStorage != nil {
		defer mediaStorage.Close()
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
			if err := connection.Compensate(rollbackContext, *provisioned); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("compensate installation data: %w", err))
			}
		}
		if len(rollbackErrors) > 0 {
			return CompletionResponse{}, &setupRollbackError{primary: operationErr, rollback: rollbackErrors}
		}
		if destructiveStageStarted {
			stage := setupErrorStage(operationErr)
			return CompletionResponse{}, apperror.New(
				apperror.CodeSetupFailed,
				setupStageDetail(stage, false)+" 鏁版嵁娓呴櫎闃舵宸茬粡寮€濮嬶紝鍘熸湁鏁版嵁鍙兘宸叉棤娉曟仮澶嶃€?",
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
	storageValue, err := s.storageConfig(input.Storage)
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
	ticketSecret, err := s.secretGenerator()
	if err != nil {
		return config.Config{}, fmt.Errorf("generate playback ticket secret: %w", err)
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
			MigrationsDirectory:     strings.TrimSpace(input.Paths.MigrationsDirectory),
			AdminWebDirectory:       strings.TrimSpace(input.Paths.AdminWebDirectory),
			MediaToolsDirectory:     mediaDirectory,
			LocalMusicDirectory:     strings.TrimSpace(input.Source.Directory),
			MediaAssetDirectory:     storageValue.AssetDirectory,
			MediaTranscodeDirectory: storageValue.TranscodeDirectory,
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
			PlaybackTicketSecret:        ticketSecret,
			AccessTokenTTLSeconds:       900,
			RefreshTokenTTLSeconds:      2_592_000,
		},
		MediaStorage: storageValue,
		Media: config.Media{
			Mode:        mediaMode,
			FFmpegPath:  ffmpegConfigured,
			FFprobePath: ffprobeConfigured,
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
			"鍒濆鍖栭厤缃唴瀹规棤鏁堬紝璇锋鏌ユ爣璁板瓧娈靛悗閲嶈瘯銆?",
			apperror.WithCause(err),
		)
	}
	return validated, nil
}

func (s *Service) resolvePaths(input PathsInput) (ResolvedPaths, error) {
	if strings.TrimSpace(input.MigrationsDirectory) == "" || len(input.MigrationsDirectory) > 4000 {
		return ResolvedPaths{}, databaseInputValidation(
			"migrationsDirectory",
			"鏁版嵁搴撹縼绉荤洰褰曚笉鑳戒负绌轰笖涓嶈兘瓒呰繃 4000 涓瓧绗︺€?",
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
		return "", databaseInputValidation("database", "鏁版嵁搴撳悕涓嶈兘涓虹┖涓斾笉鑳借秴杩?255 涓瓧绗︺€?")
	}
	if username == "" || len(username) > 255 {
		return "", databaseInputValidation("username", "鏁版嵁搴撶敤鎴峰悕涓嶈兘涓虹┖涓斾笉鑳借秴杩?255 涓瓧绗︺€?")
	}
	if input.Password == "" || len(input.Password) > 2000 {
		return "", databaseInputValidation("password", "鏁版嵁搴撳瘑鐮佷笉鑳戒负绌轰笖涓嶈兘瓒呰繃 2000 涓瓧绗︺€?")
	}
	if input.Port < 1 || input.Port > 65535 {
		return "", databaseInputValidation("port", "鏁版嵁搴撶鍙ｅ繀椤绘槸 1 鍒?65535 涔嬮棿鐨勬暣鏁般€?")
	}
	if input.MaxConnections < 1 || input.MaxConnections > 100 {
		return "", databaseInputValidation("maxConnections", "鏁版嵁搴撴渶澶ц繛鎺ユ暟蹇呴』鏄?1 鍒?100 涔嬮棿鐨勬暣鏁般€?")
	}
	switch input.SSLMode {
	case "disable", "prefer", "require", "verify-full":
	default:
		return "", databaseInputValidation("sslMode", "鏁版嵁搴?SSL 妯″紡鏃犳晥銆?")
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
	// Keep a stable, field-specific prefix even when the detailed localized
	// message is supplied by an older configuration layer. The admin UI uses
	// this to distinguish database input errors from connectivity failures.
	if !strings.Contains(detail, "\u6570\u636e\u5e93") {
		detail = "\u6570\u636e\u5e93\uff1a" + detail
	}
	return apperror.Validation(detail, map[string][]string{field: {detail}})
}

func (s *Service) storageConfig(input StorageInput) (config.MediaStorage, error) {
	assetDir := strings.TrimSpace(input.AssetDirectory)
	if assetDir == "" {
		assetDir = config.DefaultMediaAssetDirectory
	}
	transcodeDir := strings.TrimSpace(input.TranscodeDirectory)
	if transcodeDir == "" {
		transcodeDir = config.DefaultMediaTranscodeDirectory
	}
	resolvedAsset, err := s.resolvePath(assetDir, "storage.assetDirectory")
	if err != nil {
		return config.MediaStorage{}, err
	}
	resolvedTranscode, err := s.resolvePath(transcodeDir, "storage.transcodeDirectory")
	if err != nil {
		return config.MediaStorage{}, err
	}
	uploadTTL := 3600
	if input.UploadTTLSeconds != nil {
		uploadTTL = *input.UploadTTLSeconds
	}
	streamTTL := 1800
	if input.StreamTTLSeconds != nil {
		streamTTL = *input.StreamTTLSeconds
	}
	maxConcurrent := 4
	if input.StreamMaxConcurrent != nil {
		maxConcurrent = *input.StreamMaxConcurrent
	}
	idleTimeout := 120
	if input.StreamIdleTimeoutSeconds != nil {
		idleTimeout = *input.StreamIdleTimeoutSeconds
	}
	transcodeTimeout := 300
	if input.TranscodeTimeoutSeconds != nil {
		transcodeTimeout = *input.TranscodeTimeoutSeconds
	}
	transcodeCacheMaxBytes := config.DefaultMediaTranscodeCacheMaxBytes
	if input.TranscodeCacheMaxBytes != nil {
		transcodeCacheMaxBytes = *input.TranscodeCacheMaxBytes
	}
	maxUploadBytes := int64(config.MaxServerRequestBodyBytes)
	if input.MaxUploadBytes != nil {
		maxUploadBytes = *input.MaxUploadBytes
	}
	if transcodeCacheMaxBytes < config.MinMediaTranscodeCacheMaxBytes || transcodeCacheMaxBytes > config.MaxMediaTranscodeCacheMaxBytes {
		return config.MediaStorage{}, apperror.Validation("storage.transcodeCacheMaxBytes is invalid")
	}
	return config.MediaStorage{
		AssetDirectory:           resolvedAsset,
		TranscodeDirectory:       resolvedTranscode,
		UploadTTLSeconds:         uploadTTL,
		StreamTTLSeconds:         streamTTL,
		StreamMaxConcurrent:      maxConcurrent,
		StreamIdleTimeoutSeconds: idleTimeout,
		TranscodeTimeoutSeconds:  transcodeTimeout,
		TranscodeCacheMaxBytes:   transcodeCacheMaxBytes,
		MaxUploadBytes:           maxUploadBytes,
	}, nil
}

func validateHTTP(input HTTPInput) error {
	ipv4, ipv6 := normalizedHTTPListeners(input)
	if address := net.ParseIP(ipv4.Host); address == nil || address.To4() == nil {
		return apperror.Validation("IPv4 鐩戝惉 IP 鏃犳晥锛岃濉啓 IPv4 鍦板潃銆?", map[string][]string{
			"ipv4Host": {"璇疯緭鍏ユ湁鏁堢殑 IPv4 鍦板潃"},
		})
	}
	if address := net.ParseIP(ipv6.Host); address == nil || address.To4() != nil {
		return apperror.Validation("IPv6 鐩戝惉 IP 鏃犳晥锛岃濉啓 IPv6 鍦板潃銆?", map[string][]string{
			"ipv6Host": {"璇疯緭鍏ユ湁鏁堢殑 IPv6 鍦板潃"},
		})
	}
	if ipv4.Port < 1 || ipv4.Port > 65535 {
		return apperror.Validation("IPv4 鐩戝惉绔彛蹇呴』鍦?1 鍒?65535 涔嬮棿銆?", map[string][]string{
			"ipv4Port": {"绔彛蹇呴』鍦?1 鍒?65535 涔嬮棿"},
		})
	}
	if ipv6.Port < 1 || ipv6.Port > 65535 {
		return apperror.Validation("IPv6 鐩戝惉绔彛蹇呴』鍦?1 鍒?65535 涔嬮棿銆?", map[string][]string{
			"ipv6Port": {"绔彛蹇呴』鍦?1 鍒?65535 涔嬮棿"},
		})
	}
	if len(input.TrustedProxyAddresses) > 100 {
		return apperror.Validation("鍙嶅悜浠ｇ悊 IP 涓嶈兘瓒呰繃 100 涓€?", map[string][]string{
			"trustedProxyAddresses": {"鏈€澶氬～鍐?100 涓弽鍚戜唬鐞?IP"},
		})
	}
	for _, address := range input.TrustedProxyAddresses {
		if net.ParseIP(strings.TrimSpace(address)) == nil {
			return apperror.Validation("鍙嶅悜浠ｇ悊鍦板潃蹇呴』鏄湁鏁堢殑 IPv4 鎴?IPv6 鍦板潃銆?", map[string][]string{
				"trustedProxyAddresses": {"璇疯緭鍏ユ湁鏁堢殑 IPv4 鎴?IPv6 鍦板潃"},
			})
		}
	}
	return nil
}

func normalizedHTTPListeners(input HTTPInput) (ListenerAddress, ListenerAddress) {
	ipv4Host := strings.Trim(strings.TrimSpace(input.IPv4Host), "[]")
	if ipv4Host == "" {
		ipv4Host = "0.0.0.0"
	}
	ipv6Host := strings.Trim(strings.TrimSpace(input.IPv6Host), "[]")
	if ipv6Host == "" {
		ipv6Host = "::"
	}
	ipv4Port := input.IPv4Port
	if ipv4Port == 0 {
		ipv4Port = 3000
	}
	ipv6Port := input.IPv6Port
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

func parseDatabaseHost(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" || len(candidate) > 255 || strings.ContainsAny(candidate, " \t\r\n/@?#") {
		return "", databaseInputValidation("host", "鏁版嵁搴撳湴鍧€鏃犳晥锛岃濉啓 IP 鎴栦富鏈哄悕锛屼笉瑕佸寘鍚鍙ｆ垨鍗忚銆?")
	}
	if strings.HasPrefix(candidate, "[") || strings.HasSuffix(candidate, "]") {
		if !strings.HasPrefix(candidate, "[") || !strings.HasSuffix(candidate, "]") || len(candidate) < 3 {
			return "", databaseInputValidation("host", "鏁版嵁搴?IPv6 鍦板潃鏍煎紡鏃犳晥銆?")
		}
		candidate = candidate[1 : len(candidate)-1]
		if strings.ContainsAny(candidate, "[]") || net.ParseIP(candidate) == nil || !strings.Contains(candidate, ":") {
			return "", databaseInputValidation("host", "鏁版嵁搴?IPv6 鍦板潃鏍煎紡鏃犳晥銆?")
		}
		return strings.ToLower(candidate), nil
	}
	if address := net.ParseIP(candidate); address != nil {
		return strings.ToLower(candidate), nil
	}
	if strings.Contains(candidate, ":") {
		return "", databaseInputValidation("host", "鏁版嵁搴撳湴鍧€涓嶈兘鍖呭惈绔彛锛涜鍦ㄧ鍙ｅ瓧娈靛崟鐙～鍐欍€?")
	}
	hostname, err := idna.Lookup.ToASCII(candidate)
	if err != nil {
		return "", databaseInputValidation("host", "鏁版嵁搴撲富鏈哄悕鏍煎紡鏃犳晥銆?")
	}
	hostname = strings.ToLower(hostname)
	if hostname == "" || len(hostname) > 253 || strings.HasSuffix(hostname, ".") {
		return "", databaseInputValidation("host", "鏁版嵁搴撲富鏈哄悕鏍煎紡鏃犳晥銆?")
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", databaseInputValidation("host", "鏁版嵁搴撲富鏈哄悕鏍煎紡鏃犳晥銆?")
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '-' {
				return "", databaseInputValidation("host", "鏁版嵁搴撲富鏈哄悕鏍煎紡鏃犳晥銆?")
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
			"妫€娴嬪埌宸叉湁浣嗘棤鏁堢殑 .env 鏂囦欢锛岃淇鎴栧垹闄よ鏂囦欢鍚庨噸鏂板垵濮嬪寲銆?",
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
		"configuration_validation": "configuration validation",
		"http_probe":               "HTTP listener probe",
		"paths_probe":              "path probe",
		"database_probe":           "database probe",
		"storage_probe":            "media storage probe",
		"media_probe":              "media tools probe",
		"source_probe":             "music source probe",
		"configuration_check":      "configuration check",
		"database_open":            "database open",
		"storage_prepare":          "media storage preparation",
		"database_migration":       "database migration",
		"database_inspection":      "database inspection",
		"storage_clear":            "media storage cleanup",
		"database_clear":           "database cleanup",
		"installation_provision":   "installation provisioning",
		"runtime_initialize":       "runtime initialization",
		"configuration_save":       "configuration save",
	}
	label := labels[stage]
	if label == "" {
		label = "setup"
	}
	detail := "setup stage failed: " + label
	if rollbackIncomplete {
		detail += "; the rollback is incomplete and manual cleanup may be required"
	}
	return detail
}

func validateDatabaseDecision(inspection InstallationInspection, action string) error {
	switch inspection.State {
	case DatabaseStateEmpty:
		if action == "" || action == databaseActionReset {
			return nil
		}
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"数据库状态已经变化，当前数据库为空，请重新验证后继续。",
			nil,
		)
	case DatabaseStatePartial, DatabaseStateComplete:
		if action == databaseActionReset {
			return nil
		}
		return apperror.New(
			apperror.CodeSetupDecisionRequired,
			"检测到现有数据库不为空，本项目不支持复用旧数据库，必须确认清空数据库后继续。",
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
