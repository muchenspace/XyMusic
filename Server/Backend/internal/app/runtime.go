package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/modules/admincatalog"
	"xymusic/server/internal/modules/adminjobs"
	"xymusic/server/internal/modules/adminmanagement"
	"xymusic/server/internal/modules/adminmedia"
	"xymusic/server/internal/modules/adminmetadata"
	"xymusic/server/internal/modules/adminmutation"
	"xymusic/server/internal/modules/adminsettings"
	"xymusic/server/internal/modules/adminsources"
	"xymusic/server/internal/modules/admintagscraping"
	"xymusic/server/internal/modules/adminweb"
	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/modules/library"
	"xymusic/server/internal/modules/localassets"
	"xymusic/server/internal/modules/playback"
	"xymusic/server/internal/modules/playlist"
	"xymusic/server/internal/modules/profile"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/httpserver"
	platformidempotency "xymusic/server/internal/platform/idempotency"
	"xymusic/server/internal/platform/localmedia"
	platformratelimit "xymusic/server/internal/platform/ratelimit"
	"xymusic/server/internal/platform/runtimemetrics"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/platform/workerstatus"
	"xymusic/server/internal/shared/pagination"
	"xymusic/server/internal/shared/resourcebudget"
	"xymusic/server/internal/shared/sse"
	"xymusic/server/internal/workers/retention"
)

type Runtime struct {
	Config                    config.Config
	DB                        *database.Pool
	LocalMedia                *localmedia.Store
	Identity                  *identity.Service
	AdminCatalog              *admincatalog.Service
	AdminJobs                 *adminjobs.Service
	AdminManagement           *adminmanagement.Service
	AdminMedia                *adminmedia.Service
	AdminMetadata             *adminmetadata.Service
	AdminMutation             *adminmutation.Service
	AdminSettings             *adminsettings.Service
	AdminSources              *adminsources.Service
	AdminTagScraping          *admintagscraping.Service
	AdminTagBatches           *admintagscraping.BatchService
	AdminArtistArtworkBatches *admintagscraping.ArtistArtworkBatchService
	Catalog                   *catalog.Service
	Library                   *library.Service
	Playback                  *playback.Service
	Playlist                  *playlist.Service
	Profile                   *profile.Service
	Metrics                   *runtimemetrics.Collector
	Handler                   *gin.Engine
	ready                     *dependencyReadiness
	events                    *sse.Broadcaster
	background                *backgroundGroup
}

type Options struct {
	RootDirectory   string
	RegisterRoutes  httpserver.RouteRegistrar
	Administration  *AdministrationOptions
	StartBackground bool
	Logger          *slog.Logger
}

type AdministrationOptions struct {
	Runtime            adminsettings.RuntimeController
	Store              adminsettings.ConfigurationStore
	Worker             adminsettings.WorkerMonitor
	Storage            adminsettings.MediaStorageFactory
	MediaTool          adminsettings.MediaTool
	ConfigurationPath  string
	IPv4ListenerHost   string
	IPv4ListenerPort   int
	IPv6ListenerHost   string
	IPv6ListenerPort   int
	ApplicationVersion string
	StartedAt          time.Time
}

// assembly carries the shared dependencies and constructed services while the
// composition root wires the modules in a fixed order.
type assembly struct {
	ctx          context.Context
	raw          config.Config
	options      Options
	resolved     config.Config
	logger       *slog.Logger
	workerLogger slogWorkerLogger

	db                   *database.Pool
	metrics              *runtimemetrics.Collector
	events               *sse.Broadcaster
	localMedia           *localmedia.Store
	idempotencyService   *platformidempotency.Service
	localAssetsPresenter *localassets.Presenter

	identityService *identity.Service
	identityRoutes  *identity.Routes

	adminMediaService *adminmedia.Service
	adminMediaRoutes  *adminmedia.Routes

	metadataRepository   *adminmetadata.Repository
	adminMetadataService *adminmetadata.Service
	adminMetadataRoutes  *adminmetadata.Routes
	metadataWorker       *adminmetadata.WritebackWorker
	adminJobsService     *adminjobs.Service
	adminJobsRoutes      *adminjobs.Routes

	sourceRepository    *adminsources.Repository
	sourceWorker        *adminsources.Worker
	adminSourcesService *adminsources.Service
	adminSourcesRoutes  *adminsources.Routes

	tagRepository             *admintagscraping.Repository
	tagScrapingService        *admintagscraping.Service
	tagBatchService           *admintagscraping.BatchService
	artistArtworkBatchService *admintagscraping.ArtistArtworkBatchService
	tagRoutes                 *admintagscraping.Routes

	catalogService *catalog.Service
	catalogRoutes  *catalog.Routes

	playbackService *playback.Service
	playbackRoutes  *playback.Routes

	localAssetsRepository *localassets.Repository
	localAssetsRoutes     *localassets.Routes
	localAssetsCleaner    *localassets.Cleaner

	profileService *profile.Service
	profileRoutes  *profile.Routes

	libraryService *library.Service
	libraryRoutes  *library.Routes

	playlistUsers   *playlist.ProductionUserPresenter
	playlistService *playlist.Service
	playlistRoutes  *playlist.Routes

	adminManagementService     *adminmanagement.Service
	adminManagementRoutes      *adminmanagement.Routes
	adminManagementIdempotency *adminmanagement.PersistentIdempotency

	adminCatalogService *admincatalog.Service
	adminCatalogRoutes  *admincatalog.Routes

	adminMutationService  *adminmutation.Service
	adminMutationRoutes   *adminmutation.Routes
	permanentDeleteWorker *adminmutation.PermanentDeleteBatchWorker

	adminSettingsService *adminsettings.Service
	adminSettingsRoutes  *adminsettings.Routes

	adminAuthRoutes *adminauth.Routes

	engine     *gin.Engine
	readiness  *dependencyReadiness
	background *backgroundGroup
}

func Bootstrap(ctx context.Context, raw config.Config, options Options) (*Runtime, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	workerLogger := newSlogWorkerLogger(logger)
	resolved, err := config.ResolveRuntime(raw, options.RootDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime configuration: %w", err)
	}

	probeBudget, err := resourcebudget.New(max(1, resolved.LocalLibrary.ScanProbeWorkers))
	if err != nil {
		return nil, fmt.Errorf("create ffprobe resource budget: %w", err)
	}

	db, err := database.Open(ctx, resolved.Database)
	if err != nil {
		return nil, err
	}
	failed := true
	var state *assembly
	defer func() {
		if failed {
			if state != nil {
				if state.background != nil {
					closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_ = state.background.Close(closeContext)
					cancel()
				}
				if state.events != nil {
					state.events.Close()
				}
				if state.metrics != nil {
					state.metrics.Close()
				}
			}
			db.Close()
		}
	}()

	state = &assembly{
		ctx: ctx, raw: raw, options: options, resolved: resolved,
		logger: logger, workerLogger: workerLogger, db: db,
	}
	if err := state.wireCore(); err != nil {
		return nil, err
	}
	if err := state.wireIdentity(); err != nil {
		return nil, err
	}
	if err := state.wireAdminMedia(); err != nil {
		return nil, err
	}
	if err := state.wireAdminMetadata(); err != nil {
		return nil, err
	}
	if err := state.wireAdminSources(probeBudget); err != nil {
		return nil, err
	}
	if err := state.wireTagScraping(); err != nil {
		return nil, err
	}
	if err := state.wireCatalog(); err != nil {
		return nil, err
	}
	if err := state.wirePlayback(); err != nil {
		return nil, err
	}
	if err := state.wireLocalAssets(); err != nil {
		return nil, err
	}
	if err := state.wireProfile(); err != nil {
		return nil, err
	}
	if err := state.wireLibrary(); err != nil {
		return nil, err
	}
	if err := state.wirePlaylist(); err != nil {
		return nil, err
	}
	if err := state.wireAdminManagement(); err != nil {
		return nil, err
	}
	if err := state.wireAdminCatalog(); err != nil {
		return nil, err
	}
	if err := state.wireAdminMutation(); err != nil {
		return nil, err
	}
	if err := state.wireAdminSettings(); err != nil {
		return nil, err
	}
	if err := state.wireAdminAuth(); err != nil {
		return nil, err
	}
	if err := state.wireHTTP(); err != nil {
		return nil, err
	}
	background, err := state.startBackground()
	if err != nil {
		return nil, err
	}

	failed = false
	return &Runtime{
		Config: resolved, DB: db, LocalMedia: state.localMedia, Identity: state.identityService,
		AdminCatalog: state.adminCatalogService, AdminJobs: state.adminJobsService,
		AdminManagement: state.adminManagementService, AdminMedia: state.adminMediaService,
		AdminMetadata: state.adminMetadataService, AdminMutation: state.adminMutationService,
		AdminSettings: state.adminSettingsService, AdminSources: state.adminSourcesService,
		AdminTagScraping: state.tagScrapingService, AdminTagBatches: state.tagBatchService,
		AdminArtistArtworkBatches: state.artistArtworkBatchService,
		Catalog:                   state.catalogService, Library: state.libraryService, Playback: state.playbackService,
		Playlist: state.playlistService, Profile: state.profileService, Metrics: state.metrics, Handler: state.engine, ready: state.readiness,
		events: state.events, background: background,
	}, nil
}

func (state *assembly) wireCore() error {
	metrics, err := runtimemetrics.New(runtimemetrics.Options{})
	if err != nil {
		return fmt.Errorf("create runtime metrics: %w", err)
	}
	state.metrics = metrics
	if err := database.RunMigrations(state.ctx, state.db.Pool, state.resolved.Paths.MigrationsDirectory); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	if err := database.EnsureLargeLibraryIndexes(state.ctx, state.db.Pool); err != nil {
		return fmt.Errorf("ensure large-library indexes: %w", err)
	}

	localMedia, err := localmedia.NewStore(state.resolved.MediaStorage.AssetDirectory, state.resolved.MediaStorage.MaxUploadBytes)
	if err != nil {
		return fmt.Errorf("create local media store: %w", err)
	}
	if err := localMedia.Ping(state.ctx); err != nil {
		return fmt.Errorf("ping local media store: %w", err)
	}
	state.localMedia = localMedia

	payloadCipher, err := platformsecurity.NewPayloadCipher(state.resolved.Security.IdempotencyEncryptionSecret)
	if err != nil {
		return err
	}
	state.idempotencyService = platformidempotency.New(state.db.Pool, payloadCipher)
	events, err := sse.New(sse.Options{})
	if err != nil {
		return fmt.Errorf("create SSE broadcaster: %w", err)
	}
	state.events = events
	state.localAssetsPresenter = localassets.NewPresenter()
	return nil
}

func (state *assembly) wireIdentity() error {
	identityService, err := identity.NewService(
		state.resolved.Security.RefreshTokenTTLSeconds,
		state.resolved.Registration.Enabled,
		identity.ServiceDependencies{
			Repository: identity.NewRepository(state.db.Pool),
			AccessTokens: identity.NewAccessTokenService(
				state.resolved.Security.AccessTokenSecret,
				time.Duration(state.resolved.Security.AccessTokenTTLSeconds)*time.Second,
			),
			Idempotency: identity.NewPersistentRefreshIdempotency(state.idempotencyService),
			ArtworkURLs: state.localAssetsPresenter,
			Passwords:   identity.SecurityPasswordManager{},
			Secrets:     identity.SecuritySecretHasher{},
			OpaqueToken: identity.CreateOpaqueToken,
		})
	if err != nil {
		return fmt.Errorf("create identity service: %w", err)
	}
	state.identityService = identityService
	state.identityRoutes = identity.NewRoutes(identityService, platformratelimit.NewDatabaseLimiter(state.db.Pool))
	return nil
}

func (state *assembly) wireAdminMedia() error {
	adminMediaInspector, err := adminmedia.NewFFmpegMediaInspector(
		state.localMedia,
		state.resolved.Media.FFprobePath,
		state.resolved.Media.FFmpegPath,
	)
	if err != nil {
		return fmt.Errorf("create admin media inspector: %w", err)
	}
	adminMediaService, err := adminmedia.NewService(
		state.resolved.MediaStorage.UploadTTLSeconds,
		state.resolved.MediaStorage.MaxUploadBytes,
		adminmedia.ServiceDependencies{
			Repository:  adminmedia.NewRepository(state.db.Pool),
			Idempotency: adminmedia.NewPersistentIdempotency(state.idempotencyService),
			LocalMedia:  state.localMedia,
			Inspector:   adminMediaInspector,
		})
	if err != nil {
		return fmt.Errorf("create admin media service: %w", err)
	}
	state.adminMediaService = adminMediaService
	state.adminMediaRoutes = adminmedia.NewRoutes(
		state.identityService,
		adminMediaService,
	)
	if err != nil {
		return fmt.Errorf("create admin media routes: %w", err)
	}
	return nil
}

func (state *assembly) wireAdminMetadata() error {
	metadataRepository := adminmetadata.NewRepository(state.db.Pool)
	state.metadataRepository = metadataRepository
	adminMetadataService, err := adminmetadata.NewServiceWithOptions(
		metadataRepository, pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret),
	)
	if err != nil {
		return fmt.Errorf("create admin metadata service: %w", err)
	}
	state.adminMetadataService = adminMetadataService
	adminMetadataRoutes, err := adminmetadata.NewRoutes(
		adminMetadataService,
		state.identityService,
		adminmetadata.NewPersistentIdempotency(state.idempotencyService),
	)
	if err != nil {
		return fmt.Errorf("create admin metadata routes: %w", err)
	}
	state.adminMetadataRoutes = adminMetadataRoutes
	metadataJobs, err := adminmetadata.NewAdminJobsAdapter(adminMetadataService)
	if err != nil {
		return fmt.Errorf("create admin metadata job adapter: %w", err)
	}
	adminJobsService, err := adminjobs.NewServiceWithOptions(
		adminjobs.NewRepository(state.db.Pool), metadataJobs,
		pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret),
	)
	if err != nil {
		return fmt.Errorf("create admin jobs service: %w", err)
	}
	state.adminJobsService = adminJobsService
	adminJobsRoutes, err := adminjobs.NewRoutes(
		adminJobsService,
		state.identityService,
		adminjobs.NewPersistentIdempotency(state.idempotencyService),
		state.events,
	)
	if err != nil {
		return fmt.Errorf("create admin jobs routes: %w", err)
	}
	state.adminJobsRoutes = adminJobsRoutes
	metadataWorker, err := adminmetadata.NewWritebackWorker(adminmetadata.WorkerDependencies{
		Store: metadataRepository, FFmpegPath: state.resolved.Media.FFmpegPath, FFprobePath: state.resolved.Media.FFprobePath,
		Artwork: state.localMedia, Logger: state.workerLogger,
	})
	if err != nil {
		return fmt.Errorf("create metadata writeback worker: %w", err)
	}
	state.metadataWorker = metadataWorker
	return nil
}

func (state *assembly) wireAdminSources(probeBudget *resourcebudget.Budget) error {
	sourceRepository := adminsources.NewRepository(state.db.Pool)
	state.sourceRepository = sourceRepository
	sourceProbe, err := adminsources.NewFFprobeMetadataProbe(state.resolved.Media.FFprobePath, nil)
	if err != nil {
		return fmt.Errorf("create local library metadata probe: %w", err)
	}
	sourceSynchronizer, err := adminsources.NewProductionSynchronizer(adminsources.ProductionSynchronizerOptions{
		Database:       state.db.Pool,
		Probe:          sourceProbe,
		ProbeWorkers:   state.resolved.LocalLibrary.ScanProbeWorkers,
		ProbeBudget:    probeBudget,
		LocalMedia:     state.localMedia,
		FFmpegPath:     state.resolved.Media.FFmpegPath,
		ArtworkWorkers: max(2, min(16, runtime.GOMAXPROCS(0)*2)),
	})
	if err != nil {
		return fmt.Errorf("create local library synchronizer: %w", err)
	}
	sourceScanner, err := adminsources.NewFilesystemScannerWithOptions(adminsources.FilesystemScannerOptions{
		Synchronizer: sourceSynchronizer, Workers: state.resolved.LocalLibrary.ScanWorkers,
		CommitWorkers:   state.resolved.LocalLibrary.ScanCommitWorkers,
		CommitBatchSize: state.resolved.LocalLibrary.ScanCommitBatchSize,
	})
	if err != nil {
		return fmt.Errorf("create local library scanner: %w", err)
	}
	sourceWorker, err := adminsources.NewWorker(adminsources.WorkerOptions{
		Store: sourceRepository, Scanner: sourceScanner, RootDirectory: state.options.RootDirectory,
		DefaultRoot: state.resolved.LocalLibrary, RootProbe: adminsources.OSRootProbe{},
	})
	if err != nil {
		return fmt.Errorf("create local library scan worker: %w", err)
	}
	state.sourceWorker = sourceWorker
	workerAvailable := func(ctx context.Context) (bool, error) {
		if state.options.StartBackground {
			return true, nil
		}
		if administration := state.options.Administration; administration != nil && administration.Worker != nil {
			status := administration.Worker.Status(ctx, workerstatus.ConfigurationFingerprint(state.raw))
			return status.Available, nil
		}
		return false, nil
	}
	adminSourcesService, err := adminsources.NewServiceWithOptions(adminsources.ServiceDependencies{
		Store: sourceRepository, RootDirectory: state.options.RootDirectory, WorkerAvailability: workerAvailable,
		DirectoryBrowser: adminsources.OSDirectoryBrowser{}, RootProbe: adminsources.OSRootProbe{},
	}, pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret))
	if err != nil {
		return fmt.Errorf("create administrator sources service: %w", err)
	}
	state.adminSourcesService = adminSourcesService
	adminSourcesRoutes, err := adminsources.NewRoutes(
		adminSourcesService,
		state.identityService,
		adminsources.NewPersistentIdempotency(state.idempotencyService),
		state.events,
	)
	if err != nil {
		return fmt.Errorf("create administrator sources routes: %w", err)
	}
	state.adminSourcesRoutes = adminSourcesRoutes
	return nil
}

func (state *assembly) wireTagScraping() error {
	tagRepository := admintagscraping.NewRepository(state.db.Pool)
	state.tagRepository = tagRepository
	tagArtwork, err := admintagscraping.NewAdminMediaArtworkApplier(
		adminTagScrapingMediaAdapter{media: state.adminMediaService},
	)
	if err != nil {
		return fmt.Errorf("create tag scraping artwork adapter: %w", err)
	}
	tagScrapingService, err := admintagscraping.NewService(admintagscraping.ServiceDependencies{
		Store: tagRepository,
		Music: admintagscraping.NewMusicPlatformClientWithOptions(admintagscraping.MusicPlatformOptions{
			RequestWorkers: state.resolved.Scraping.RequestWorkers,
			ArtworkWorkers: state.resolved.Scraping.ArtworkWorkers,
		}),
		Artwork: tagArtwork, DefaultLibraryDirectory: state.resolved.LocalLibrary.Directory,
	})
	if err != nil {
		return fmt.Errorf("create tag scraping service: %w", err)
	}
	state.tagScrapingService = tagScrapingService
	tagBatchService, err := admintagscraping.NewBatchService(admintagscraping.BatchServiceDependencies{
		Store: tagRepository, Processor: tagScrapingService, Logger: state.workerLogger,
		Workers: state.resolved.Scraping.BatchWorkers, ClaimWindow: state.resolved.Scraping.BatchClaimWindow,
	})
	if err != nil {
		return fmt.Errorf("create tag scraping batch service: %w", err)
	}
	state.tagBatchService = tagBatchService
	artistArtworkBatchService, err := admintagscraping.NewArtistArtworkBatchService(
		admintagscraping.ArtistArtworkBatchServiceDependencies{
			Store: tagRepository, Processor: tagScrapingService, Logger: state.workerLogger,
			Workers: state.resolved.Scraping.ArtworkWorkers, ClaimWindow: state.resolved.Scraping.ArtworkClaimWindow,
		},
	)
	if err != nil {
		return fmt.Errorf("create artist artwork scraping batch service: %w", err)
	}
	state.artistArtworkBatchService = artistArtworkBatchService
	tagRoutes, err := admintagscraping.NewRoutes(
		tagScrapingService,
		tagBatchService,
		artistArtworkBatchService,
		state.identityService,
		admintagscraping.NewPersistentIdempotency(state.idempotencyService),
	)
	if err != nil {
		return fmt.Errorf("create tag scraping routes: %w", err)
	}
	state.tagRoutes = tagRoutes
	return nil
}

func (state *assembly) wireCatalog() error {
	catalogService, err := catalog.NewService(catalog.ServiceDependencies{
		Repository:  catalog.NewRepository(state.db.Pool),
		Cursors:     pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret),
		ArtworkURLs: state.localAssetsPresenter,
	})
	if err != nil {
		return fmt.Errorf("create catalog service: %w", err)
	}
	state.catalogService = catalogService
	catalogRoutes, err := catalog.NewRoutes(catalogService, catalog.AuthenticateFunc(
		func(ctx context.Context, authorization string) (string, error) {
			actor, err := state.identityService.Authenticate(ctx, authorization)
			if err != nil {
				return "", err
			}
			return actor.UserID, nil
		},
	))
	if err != nil {
		return fmt.Errorf("create catalog routes: %w", err)
	}
	state.catalogRoutes = catalogRoutes
	return nil
}

func (state *assembly) wirePlayback() error {
	playbackResolver := playback.NewPlaybackSourceResolver(state.db.Pool, state.localMedia)
	playbackSigner, err := playback.NewTicketSigner(state.resolved.Security.PlaybackTicketSecret)
	if err != nil {
		return fmt.Errorf("create playback ticket signer: %w", err)
	}
	playbackService, err := playback.NewService(
		playbackResolver,
		playbackSigner,
		time.Duration(state.resolved.MediaStorage.StreamTTLSeconds)*time.Second,
	)
	if err != nil {
		return fmt.Errorf("create playback service: %w", err)
	}
	state.playbackService = playbackService
	playbackRoutes, err := playback.NewRoutes(
		playbackService,
		playbackSigner,
		&playbackIdentityAdapter{identity: state.identityService},
		playback.NewOSAssetOpener(),
	)
	if err != nil {
		return fmt.Errorf("create playback routes: %w", err)
	}
	state.playbackRoutes = playbackRoutes
	return nil
}

func (state *assembly) wireLocalAssets() error {
	localAssetsRepository := localassets.NewRepository(state.db.Pool)
	state.localAssetsRepository = localAssetsRepository
	localAssetsRoutes, err := localassets.NewRoutes(localAssetsRepository, state.localMedia)
	if err != nil {
		return fmt.Errorf("create local assets routes: %w", err)
	}
	state.localAssetsRoutes = localAssetsRoutes
	localAssetsCleaner, err := localassets.NewCleaner(state.db.Pool, state.localMedia)
	if err != nil {
		return fmt.Errorf("create local asset cleaner: %w", err)
	}
	state.localAssetsCleaner = localAssetsCleaner
	return nil
}

func (state *assembly) wireProfile() error {
	profileInspector, err := profile.NewFFmpegAvatarInspector(
		state.localMedia,
		state.resolved.Media.FFprobePath,
		state.resolved.Media.FFmpegPath,
	)
	if err != nil {
		return fmt.Errorf("create profile avatar inspector: %w", err)
	}
	profileService, err := profile.NewService(
		state.resolved.MediaStorage.UploadTTLSeconds,
		state.resolved.MediaStorage.MaxUploadBytes,
		profile.ServiceDependencies{
			Repository:   profile.NewRepository(state.db.Pool),
			CurrentUsers: state.identityService,
			Idempotency:  profile.NewPersistentIdempotency(state.idempotencyService),
			LocalMedia:   state.localMedia,
			Inspector:    profileInspector,
		})
	if err != nil {
		return fmt.Errorf("create profile service: %w", err)
	}
	state.profileService = profileService
	state.profileRoutes = profile.NewRoutes(state.identityService, profileService)
	return nil
}

func (state *assembly) wireLibrary() error {
	authenticateUserID := func(ctx context.Context, authorization string) (string, error) {
		actor, err := state.identityService.Authenticate(ctx, authorization)
		if err != nil {
			return "", err
		}
		return actor.UserID, nil
	}
	libraryService, err := library.NewService(library.ServiceDependencies{
		Repository:  library.NewRepository(state.db.Pool),
		Cursors:     pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret),
		Tracks:      state.catalogService,
		Idempotency: library.NewPersistentIdempotency(state.idempotencyService),
	})
	if err != nil {
		return fmt.Errorf("create library service: %w", err)
	}
	state.libraryService = libraryService
	libraryRoutes, err := library.NewRoutes(libraryService, library.AuthenticateFunc(authenticateUserID))
	if err != nil {
		return fmt.Errorf("create library routes: %w", err)
	}
	state.libraryRoutes = libraryRoutes
	return nil
}

func (state *assembly) wirePlaylist() error {
	playlistUsers, err := playlist.NewProductionUserPresenter(
		state.db.Pool,
		state.localAssetsPresenter,
	)
	if err != nil {
		return fmt.Errorf("create playlist user presenter: %w", err)
	}
	state.playlistUsers = playlistUsers
	playlistService, err := playlist.NewService(playlist.ServiceDependencies{
		Repository: playlist.NewRepository(state.db.Pool),
		Cursors:    pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret),
		Catalog:    state.catalogService,
		Users:      playlistUsers,
	})
	if err != nil {
		return fmt.Errorf("create playlist service: %w", err)
	}
	state.playlistService = playlistService
	playlistRoutes, err := playlist.NewRoutes(
		playlistService,
		playlist.AuthenticateFunc(func(ctx context.Context, authorization string) (string, error) {
			actor, err := state.identityService.Authenticate(ctx, authorization)
			if err != nil {
				return "", err
			}
			return actor.UserID, nil
		}),
		playlist.NewPersistentIdempotency(state.idempotencyService),
	)
	if err != nil {
		return fmt.Errorf("create playlist routes: %w", err)
	}
	state.playlistRoutes = playlistRoutes
	return nil
}

func (state *assembly) wireAdminManagement() error {
	adminManagementIdempotency := adminmanagement.NewPersistentIdempotency(state.idempotencyService)
	state.adminManagementIdempotency = adminManagementIdempotency
	adminManagementService, err := adminmanagement.NewServiceWithOptions(adminmanagement.ServiceDependencies{
		Store:     adminmanagement.NewRepository(state.db.Pool),
		Artworks:  adminArtworkPresenter{delegate: state.playlistUsers},
		Passwords: identity.SecurityPasswordManager{},
	}, pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret))
	if err != nil {
		return fmt.Errorf("create admin management service: %w", err)
	}
	state.adminManagementService = adminManagementService
	adminManagementRoutes, err := adminmanagement.NewRoutes(
		adminManagementService, state.identityService, adminManagementIdempotency,
	)
	if err != nil {
		return fmt.Errorf("create admin management routes: %w", err)
	}
	state.adminManagementRoutes = adminManagementRoutes
	return nil
}

func (state *assembly) wireAdminCatalog() error {
	adminCatalogService, err := admincatalog.NewServiceWithOptions(
		admincatalog.NewRepository(state.db.Pool), adminArtworkPresenter{delegate: state.playlistUsers},
		pagination.NewCursorCodec(state.resolved.Security.CursorSigningSecret),
	)
	if err != nil {
		return fmt.Errorf("create admin catalog service: %w", err)
	}
	state.adminCatalogService = adminCatalogService
	adminCatalogRoutes, err := admincatalog.NewRoutes(adminCatalogService, state.identityService)
	if err != nil {
		return fmt.Errorf("create admin catalog routes: %w", err)
	}
	state.adminCatalogRoutes = adminCatalogRoutes
	return nil
}

func (state *assembly) wireAdminMutation() error {
	adminMutationIdempotency := adminmutation.NewPersistentIdempotency(state.idempotencyService)
	adminMutationRepository := adminmutation.NewRepository(state.db.Pool)
	adminMutationService, err := adminmutation.NewService(
		adminMutationRepository, adminArtworkPresenter{delegate: state.playlistUsers},
		state.resolved.LocalLibrary.Directory,
	)
	if err != nil {
		return fmt.Errorf("create admin mutation service: %w", err)
	}
	state.adminMutationService = adminMutationService
	permanentDeleteWorker, err := adminmutation.NewPermanentDeleteBatchWorker(
		adminmutation.PermanentDeleteBatchWorkerDependencies{
			Store: adminMutationRepository, Deleter: adminMutationRepository,
			LibraryDirectory: state.resolved.LocalLibrary.Directory, Logger: state.workerLogger,
		},
	)
	if err != nil {
		return fmt.Errorf("create permanent track deletion worker: %w", err)
	}
	state.permanentDeleteWorker = permanentDeleteWorker
	adminMutationRoutes, err := adminmutation.NewRoutes(
		adminMutationService, state.identityService, adminMutationIdempotency,
	)
	if err != nil {
		return fmt.Errorf("create admin mutation routes: %w", err)
	}
	state.adminMutationRoutes = adminMutationRoutes
	return nil
}

func (state *assembly) wireAdminSettings() error {
	if administration := state.options.Administration; administration != nil {
		storageFactory := administration.Storage
		if storageFactory == nil {
			storageFactory = adminsettings.ProductionMediaStorageFactory{}
		}
		mediaTool := administration.MediaTool
		if mediaTool == nil {
			mediaTool = setup.CommandMediaTool{}
		}
		adminSettingsDatabase := adminsettings.NewProductionDatabase(state.db)
		adminSettingsService, err := adminsettings.NewService(adminsettings.ServiceDependencies{
			Database: adminSettingsDatabase, Databases: adminsettings.ProductionDatabaseFactory{},
			Runtime: administration.Runtime, Store: adminsettings.NewRepository(adminSettingsDatabase),
			Configuration: administration.Store, Idempotency: adminsettings.ProductionIdempotencyFactory{},
			Storage: storageFactory, MediaTool: mediaTool, Worker: administration.Worker,
			Metrics:        state.metrics,
			DirectoryProbe: adminsettings.ProductionDirectoryProbe{},
			RootDirectory:  state.options.RootDirectory, ConfigurationPath: administration.ConfigurationPath,
			Listener: adminsettings.ListenerDTO{
				IPv4: adminsettings.ListenerAddressDTO{Host: administration.IPv4ListenerHost, Port: administration.IPv4ListenerPort},
				IPv6: adminsettings.ListenerAddressDTO{Host: administration.IPv6ListenerHost, Port: administration.IPv6ListenerPort},
			},
			ApplicationVersion: administration.ApplicationVersion, StartedAt: administration.StartedAt,
		})
		if err != nil {
			return fmt.Errorf("create admin settings service: %w", err)
		}
		state.adminSettingsService = adminSettingsService
		adminSettingsRoutes, err := adminsettings.NewRoutes(adminSettingsService, state.identityService)
		if err != nil {
			return fmt.Errorf("create admin settings routes: %w", err)
		}
		state.adminSettingsRoutes = adminSettingsRoutes
	}
	return nil
}

func (state *assembly) wireAdminAuth() error {
	adminAuthRoutes, err := adminauth.NewRoutes(
		state.identityService, state.resolved, platformratelimit.NewDatabaseLimiter(state.db.Pool),
	)
	if err != nil {
		return fmt.Errorf("create admin authentication routes: %w", err)
	}
	state.adminAuthRoutes = adminAuthRoutes
	return nil
}

func (state *assembly) wireHTTP() error {
	if state.resolved.Environment == config.Production {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	adminAssets, err := adminweb.New(state.resolved.Paths.AdminWebDirectory)
	if err != nil {
		return fmt.Errorf("configure admin web assets: %w", err)
	}
	cors := httpserver.UnrestrictedCORSConfig()
	readiness := &dependencyReadiness{database: state.db, localMedia: state.localMedia}
	state.readiness = readiness
	engine, err := httpserver.New(httpserver.Options{
		CORS:           cors,
		RequestLimits:  httpserver.DefaultRequestLimits(),
		Readiness:      readiness,
		Metrics:        state.metrics,
		TrustedProxies: append([]string(nil), state.resolved.HTTP.TrustedProxyAddresses...),
		RegisterRoutes: func(engine *gin.Engine) {
			adminAssets.Register(engine)
			state.localAssetsRoutes.Register(engine)
			state.identityRoutes.Register(engine)
			state.adminMediaRoutes.Register(engine)
			state.adminMetadataRoutes.Register(engine)
			state.adminJobsRoutes.Register(engine)
			state.adminSourcesRoutes.Register(engine)
			state.tagRoutes.Register(engine)
			state.catalogRoutes.Register(engine)
			state.playbackRoutes.Register(engine)
			state.profileRoutes.Register(engine)
			state.libraryRoutes.Register(engine)
			state.playlistRoutes.Register(engine)
			state.adminAuthRoutes.Register(engine)
			state.adminManagementRoutes.Register(engine)
			state.adminCatalogRoutes.Register(engine)
			state.adminMutationRoutes.Register(engine)
			if state.adminSettingsRoutes != nil {
				state.adminSettingsRoutes.Register(engine)
			}
			if state.options.RegisterRoutes != nil {
				state.options.RegisterRoutes(engine)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("create HTTP application: %w", err)
	}
	state.engine = engine
	return nil
}

func (state *assembly) startBackground() (*backgroundGroup, error) {
	if !state.options.StartBackground {
		return nil, nil
	}
	retentionDatabase, err := retention.NewPostgresDatabase(state.db.Pool)
	if err != nil {
		return nil, fmt.Errorf("create retention database: %w", err)
	}
	retentionWorker, err := retention.NewWorker(retention.Dependencies{
		Database: retentionDatabase,
		Logger:   state.workerLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("create retention worker: %w", err)
	}
	if err := state.sourceWorker.Initialize(state.ctx); err != nil {
		return nil, fmt.Errorf("initialize local library scan worker: %w", err)
	}
	if err := state.permanentDeleteWorker.Initialize(state.ctx); err != nil {
		return nil, fmt.Errorf("initialize permanent track deletion worker: %w", err)
	}
	if err := state.tagBatchService.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("start tag scraping batch service: %w", err)
	}
	if err := state.artistArtworkBatchService.Start(context.Background()); err != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = state.tagBatchService.Close(cleanupContext)
		cleanupCancel()
		return nil, fmt.Errorf("start artist artwork scraping batch service: %w", err)
	}
	backgroundTasks := make([]backgroundTask, 0, 5+state.resolved.Scraping.WritebackWorkers)
	backgroundTasks = append(backgroundTasks,
		backgroundTask{
			name: "local-asset-cleanup",
			run:  func(ctx context.Context) (bool, error) { return state.localAssetsCleaner.RunOnce(ctx) },
		},
		backgroundTask{
			name: "library-source-scan",
			run:  func(ctx context.Context) (bool, error) { return state.sourceWorker.RunNextScan(ctx) },
		},
	)
	for index := 0; index < max(1, state.resolved.Scraping.WritebackWorkers); index++ {
		workerID := fmt.Sprintf("metadata-%d-%s", index, uuid.NewString())
		backgroundTasks = append(backgroundTasks, backgroundTask{
			name: fmt.Sprintf("metadata-writeback-%d", index),
			run:  func(ctx context.Context) (bool, error) { return state.metadataWorker.RunNext(ctx, workerID) },
		})
	}
	backgroundTasks = append(backgroundTasks,
		backgroundTask{
			name: "track-permanent-delete",
			run:  func(ctx context.Context) (bool, error) { return state.permanentDeleteWorker.RunNext(ctx) },
		},
		backgroundTask{
			name: "retention",
			run: func(ctx context.Context) (bool, error) {
				result, err := retentionWorker.RunIfDue(ctx, false)
				return result.Ran, err
			},
		},
	)
	state.background = startBackgroundGroup(state.logger, backgroundTasks...)
	return state.background, nil
}

func (runtime *Runtime) Ready(ctx context.Context) error {
	if runtime == nil || runtime.ready == nil {
		return errors.New("runtime is not initialized")
	}
	return runtime.ready.Check(ctx)
}
