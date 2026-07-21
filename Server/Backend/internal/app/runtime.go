package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/adminaudit"
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
	"xymusic/server/internal/modules/playback"
	"xymusic/server/internal/modules/playlist"
	"xymusic/server/internal/modules/profile"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/artworkstore"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/ossproxy"
	"xymusic/server/internal/platform/runtimemetrics"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/platform/storage"
	"xymusic/server/internal/platform/workerstatus"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
	"xymusic/server/internal/shared/pagination"
	"xymusic/server/internal/shared/ratelimit"
	"xymusic/server/internal/shared/sse"
	mediaworker "xymusic/server/internal/workers/media"
	"xymusic/server/internal/workers/retention"
)

type Runtime struct {
	Config                    config.Config
	DB                        *database.Pool
	Storage                   *storage.Client
	Identity                  *identity.Service
	AdminAudit                *adminaudit.Service
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
	mediaWorker               *mediaworker.Worker
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
	Storage            adminsettings.StorageFactory
	MediaTool          adminsettings.MediaTool
	ConfigurationPath  string
	IPv4ListenerHost   string
	IPv4ListenerPort   int
	IPv6ListenerHost   string
	IPv6ListenerPort   int
	ApplicationVersion string
	StartedAt          time.Time
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
	db, err := database.Open(ctx, resolved.Database)
	if err != nil {
		return nil, err
	}
	failed := true
	var events *sse.Broadcaster
	var background *backgroundGroup
	var mediaWorker *mediaworker.Worker
	var metrics *runtimemetrics.Collector
	defer func() {
		if failed {
			if background != nil {
				closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = background.Close(closeContext)
				cancel()
			}
			if events != nil {
				events.Close()
			}
			if mediaWorker != nil {
				_ = mediaWorker.Close()
			}
			if metrics != nil {
				metrics.Close()
			}
			db.Close()
		}
	}()
	metrics, err = runtimemetrics.New(runtimemetrics.Options{})
	if err != nil {
		return nil, fmt.Errorf("create runtime metrics: %w", err)
	}
	if err := database.RunMigrations(ctx, db.Pool, resolved.Paths.MigrationsDirectory); err != nil {
		return nil, fmt.Errorf("run database migrations: %w", err)
	}
	objects, err := storage.Open(resolved.Storage)
	if err != nil {
		return nil, err
	}
	artworkResolver, err := artworkstore.NewPostgresResolver(db.Pool)
	if err != nil {
		return nil, fmt.Errorf("configure artwork resolver: %w", err)
	}
	objectProxy, err := ossproxy.New(
		resolved.Storage.Endpoint,
		resolved.Storage.PublicBaseURL,
		ossproxy.WithArtworkGateway(artworkResolver, objects),
	)
	if err != nil {
		return nil, fmt.Errorf("configure object storage proxy: %w", err)
	}
	if err := objects.Ping(ctx); err != nil {
		return nil, err
	}
	payloadCipher, err := platformsecurity.NewPayloadCipher(resolved.Security.IdempotencyEncryptionSecret)
	if err != nil {
		return nil, err
	}
	idempotencyService := sharedidempotency.New(db.Pool, payloadCipher)
	events, err = sse.New(sse.Options{})
	if err != nil {
		return nil, fmt.Errorf("create SSE broadcaster: %w", err)
	}
	identityService, err := identity.NewService(resolved, identity.ServiceDependencies{
		Repository: identity.NewRepository(db.Pool),
		AccessTokens: platformsecurity.NewAccessTokenService(
			resolved.Security.AccessTokenSecret,
			time.Duration(resolved.Security.AccessTokenTTLSeconds)*time.Second,
		),
		Idempotency: identity.NewPersistentRefreshIdempotency(idempotencyService),
		ArtworkURLs: objectProxy,
	})
	if err != nil {
		return nil, fmt.Errorf("create identity service: %w", err)
	}
	identityRoutes := identity.NewRoutes(identityService, ratelimit.NewDatabaseLimiter(db.Pool))
	mediaObjects, err := adminmedia.NewMinIOObjectStorage(resolved.Storage)
	if err != nil {
		return nil, fmt.Errorf("create admin media object storage: %w", err)
	}
	adminMediaService, err := adminmedia.NewService(resolved, adminmedia.ServiceDependencies{
		Repository: adminmedia.NewRepository(db.Pool),
		Storage:    mediaObjects,
	})
	if err != nil {
		return nil, fmt.Errorf("create admin media service: %w", err)
	}
	adminMediaRoutes, err := adminmedia.NewRoutes(
		adminMediaService,
		identityService,
		adminmedia.NewPersistentIdempotency(idempotencyService),
	)
	if err != nil {
		return nil, fmt.Errorf("create admin media routes: %w", err)
	}
	metadataRepository := adminmetadata.NewRepository(db.Pool)
	adminMetadataService, err := adminmetadata.NewService(metadataRepository)
	if err != nil {
		return nil, fmt.Errorf("create admin metadata service: %w", err)
	}
	adminMetadataRoutes, err := adminmetadata.NewRoutes(
		adminMetadataService,
		identityService,
		adminmetadata.NewPersistentIdempotency(idempotencyService),
	)
	if err != nil {
		return nil, fmt.Errorf("create admin metadata routes: %w", err)
	}
	metadataJobs, err := adminmetadata.NewAdminJobsAdapter(adminMetadataService)
	if err != nil {
		return nil, fmt.Errorf("create admin metadata job adapter: %w", err)
	}
	adminJobsService, err := adminjobs.NewService(adminjobs.NewRepository(db.Pool), metadataJobs)
	if err != nil {
		return nil, fmt.Errorf("create admin jobs service: %w", err)
	}
	adminJobsRoutes, err := adminjobs.NewRoutes(
		adminJobsService,
		identityService,
		adminjobs.NewPersistentIdempotency(idempotencyService),
		events,
	)
	if err != nil {
		return nil, fmt.Errorf("create admin jobs routes: %w", err)
	}
	metadataWorker, err := adminmetadata.NewWritebackWorker(adminmetadata.WorkerDependencies{
		Store: metadataRepository, FFmpegPath: resolved.Media.FFmpegPath, FFprobePath: resolved.Media.FFprobePath,
		Artwork: objects, Logger: workerLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("create metadata writeback worker: %w", err)
	}
	sourceRepository := adminsources.NewRepository(db.Pool)
	sourceProbe, err := adminsources.NewFFprobeMetadataProbe(resolved.Media.FFprobePath, nil)
	if err != nil {
		return nil, fmt.Errorf("create local library metadata probe: %w", err)
	}
	sourceSynchronizer, err := adminsources.NewProductionSynchronizer(adminsources.ProductionSynchronizerOptions{
		Database: db.Pool, Storage: mediaObjects, Probe: sourceProbe, FFmpegPath: resolved.Media.FFmpegPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create local library synchronizer: %w", err)
	}
	sourceScanner, err := adminsources.NewFilesystemScanner(sourceSynchronizer)
	if err != nil {
		return nil, fmt.Errorf("create local library scanner: %w", err)
	}
	sourceWorker, err := adminsources.NewWorker(adminsources.WorkerOptions{
		Store: sourceRepository, Scanner: sourceScanner, RootDirectory: options.RootDirectory,
		DefaultRoot: resolved.LocalLibrary,
	})
	if err != nil {
		return nil, fmt.Errorf("create local library scan worker: %w", err)
	}
	workerAvailable := func(ctx context.Context) (bool, error) {
		if options.StartBackground {
			return true, nil
		}
		if administration := options.Administration; administration != nil && administration.Worker != nil {
			status := administration.Worker.Status(ctx, workerstatus.ConfigurationFingerprint(raw))
			return status.Available, nil
		}
		return false, nil
	}
	adminSourcesService, err := adminsources.NewService(adminsources.ServiceDependencies{
		Store: sourceRepository, RootDirectory: options.RootDirectory, WorkerAvailability: workerAvailable,
	})
	if err != nil {
		return nil, fmt.Errorf("create administrator sources service: %w", err)
	}
	adminSourcesRoutes, err := adminsources.NewRoutes(
		adminSourcesService,
		identityService,
		adminsources.NewPersistentIdempotency(idempotencyService),
		events,
	)
	if err != nil {
		return nil, fmt.Errorf("create administrator sources routes: %w", err)
	}
	tagRepository := admintagscraping.NewRepository(db.Pool)
	tagArtwork, err := admintagscraping.NewAdminMediaArtworkApplier(adminMediaService)
	if err != nil {
		return nil, fmt.Errorf("create tag scraping artwork adapter: %w", err)
	}
	tagScrapingService, err := admintagscraping.NewService(admintagscraping.ServiceDependencies{
		Store:         tagRepository,
		Music:         admintagscraping.NewMusicPlatformClient(nil, resolved.Scraping.AcoustIDClient),
		Fingerprinter: admintagscraping.ConfiguredFingerprinter(resolved.Scraping.FPcalcPath),
		Artwork:       tagArtwork, DefaultLibraryDirectory: resolved.LocalLibrary.Directory,
	})
	if err != nil {
		return nil, fmt.Errorf("create tag scraping service: %w", err)
	}
	tagBatchService, err := admintagscraping.NewBatchService(admintagscraping.BatchServiceDependencies{
		Store: tagRepository, Processor: tagScrapingService, Logger: workerLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("create tag scraping batch service: %w", err)
	}
	artistArtworkBatchService, err := admintagscraping.NewArtistArtworkBatchService(
		admintagscraping.ArtistArtworkBatchServiceDependencies{
			Store: tagRepository, Processor: tagScrapingService, Logger: workerLogger,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create artist artwork scraping batch service: %w", err)
	}
	tagRoutes, err := admintagscraping.NewRoutes(
		tagScrapingService,
		tagBatchService,
		artistArtworkBatchService,
		identityService,
		admintagscraping.NewPersistentIdempotency(idempotencyService),
	)
	if err != nil {
		return nil, fmt.Errorf("create tag scraping routes: %w", err)
	}
	catalogService, err := catalog.NewService(catalog.ServiceDependencies{
		Repository:  catalog.NewRepository(db.Pool),
		Cursors:     pagination.NewCursorCodec(resolved.Security.CursorSigningSecret),
		ArtworkURLs: objectProxy,
	})
	if err != nil {
		return nil, fmt.Errorf("create catalog service: %w", err)
	}
	catalogRoutes, err := catalog.NewRoutes(catalogService, catalog.AuthenticateFunc(
		func(ctx context.Context, authorization string) (string, error) {
			actor, err := identityService.Authenticate(ctx, authorization)
			if err != nil {
				return "", err
			}
			return actor.UserID, nil
		},
	))
	if err != nil {
		return nil, fmt.Errorf("create catalog routes: %w", err)
	}
	playbackService, err := playback.NewService(
		playback.NewRepository(db.Pool), objects,
		time.Duration(resolved.Storage.SignedURLTTLSeconds)*time.Second,
	)
	if err != nil {
		return nil, fmt.Errorf("create playback service: %w", err)
	}
	playbackRoutes, err := playback.NewRoutes(playbackService, playback.AuthenticateFunc(
		func(ctx context.Context, authorization string) error {
			_, err := identityService.Authenticate(ctx, authorization)
			return err
		},
	))
	if err != nil {
		return nil, fmt.Errorf("create playback routes: %w", err)
	}
	avatarObjects, err := profile.NewMinIOObjectStorage(resolved.Storage)
	if err != nil {
		return nil, fmt.Errorf("create profile object storage: %w", err)
	}
	profileService, err := profile.NewService(resolved, profile.ServiceDependencies{
		Repository:   profile.NewRepository(db.Pool),
		CurrentUsers: identityService,
		Idempotency:  profile.NewPersistentIdempotency(idempotencyService),
		Storage:      avatarObjects,
	})
	if err != nil {
		return nil, fmt.Errorf("create profile service: %w", err)
	}
	profileRoutes := profile.NewRoutes(identityService, profileService)
	authenticateUserID := func(ctx context.Context, authorization string) (string, error) {
		actor, err := identityService.Authenticate(ctx, authorization)
		if err != nil {
			return "", err
		}
		return actor.UserID, nil
	}
	libraryService, err := library.NewService(library.ServiceDependencies{
		Repository:  library.NewRepository(db.Pool),
		Cursors:     pagination.NewCursorCodec(resolved.Security.CursorSigningSecret),
		Tracks:      catalogService,
		Idempotency: library.NewPersistentIdempotency(idempotencyService),
	})
	if err != nil {
		return nil, fmt.Errorf("create library service: %w", err)
	}
	libraryRoutes, err := library.NewRoutes(libraryService, library.AuthenticateFunc(authenticateUserID))
	if err != nil {
		return nil, fmt.Errorf("create library routes: %w", err)
	}
	playlistUsers, err := playlist.NewProductionUserPresenter(
		db.Pool,
		objectProxy,
	)
	if err != nil {
		return nil, fmt.Errorf("create playlist user presenter: %w", err)
	}
	playlistService, err := playlist.NewService(playlist.ServiceDependencies{
		Repository: playlist.NewRepository(db.Pool),
		Cursors:    pagination.NewCursorCodec(resolved.Security.CursorSigningSecret),
		Catalog:    catalogService,
		Users:      playlistUsers,
	})
	if err != nil {
		return nil, fmt.Errorf("create playlist service: %w", err)
	}
	playlistRoutes, err := playlist.NewRoutes(
		playlistService,
		playlist.AuthenticateFunc(authenticateUserID),
		playlist.NewPersistentIdempotency(idempotencyService),
	)
	if err != nil {
		return nil, fmt.Errorf("create playlist routes: %w", err)
	}
	adminManagementIdempotency := adminmanagement.NewPersistentIdempotency(idempotencyService)
	adminManagementService, err := adminmanagement.NewService(adminmanagement.ServiceDependencies{
		Store:     adminmanagement.NewRepository(db.Pool),
		Artworks:  adminArtworkPresenter{delegate: playlistUsers},
		Passwords: identity.SecurityPasswordManager{},
	})
	if err != nil {
		return nil, fmt.Errorf("create admin management service: %w", err)
	}
	adminManagementRoutes, err := adminmanagement.NewRoutes(
		adminManagementService, identityService, adminManagementIdempotency,
	)
	if err != nil {
		return nil, fmt.Errorf("create admin management routes: %w", err)
	}
	adminCatalogService, err := admincatalog.NewService(
		admincatalog.NewRepository(db.Pool), adminArtworkPresenter{delegate: playlistUsers},
	)
	if err != nil {
		return nil, fmt.Errorf("create admin catalog service: %w", err)
	}
	adminCatalogRoutes, err := admincatalog.NewRoutes(adminCatalogService, identityService)
	if err != nil {
		return nil, fmt.Errorf("create admin catalog routes: %w", err)
	}
	adminMutationIdempotency := adminmutation.NewPersistentIdempotency(idempotencyService)
	adminMutationRepository := adminmutation.NewRepository(db.Pool)
	adminMutationService, err := adminmutation.NewService(
		adminMutationRepository, adminArtworkPresenter{delegate: playlistUsers},
		resolved.LocalLibrary.Directory,
	)
	if err != nil {
		return nil, fmt.Errorf("create admin mutation service: %w", err)
	}
	permanentDeleteWorker, err := adminmutation.NewPermanentDeleteBatchWorker(
		adminmutation.PermanentDeleteBatchWorkerDependencies{
			Store: adminMutationRepository, Deleter: adminMutationRepository,
			LibraryDirectory: resolved.LocalLibrary.Directory, Logger: workerLogger,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create permanent track deletion worker: %w", err)
	}
	adminMutationRoutes, err := adminmutation.NewRoutes(
		adminMutationService, identityService, adminMutationIdempotency,
	)
	if err != nil {
		return nil, fmt.Errorf("create admin mutation routes: %w", err)
	}
	adminAuditService, err := adminaudit.NewService(db.Pool)
	if err != nil {
		return nil, fmt.Errorf("create admin audit service: %w", err)
	}
	adminAuditRoutes, err := adminaudit.NewRoutes(adminAuditService, identityService)
	if err != nil {
		return nil, fmt.Errorf("create admin audit routes: %w", err)
	}
	var adminSettingsService *adminsettings.Service
	var adminSettingsRoutes *adminsettings.Routes
	if administration := options.Administration; administration != nil {
		storageFactory := administration.Storage
		if storageFactory == nil {
			storageFactory = adminsettings.ProductionStorageFactory{}
		}
		mediaTool := administration.MediaTool
		if mediaTool == nil {
			mediaTool = setup.CommandMediaTool{}
		}
		adminSettingsService, err = adminsettings.NewService(adminsettings.ServiceDependencies{
			Database: db, Runtime: administration.Runtime, Store: administration.Store,
			Storage: storageFactory, MediaTool: mediaTool, Worker: administration.Worker,
			Metrics:       metrics,
			RootDirectory: options.RootDirectory, ConfigurationPath: administration.ConfigurationPath,
			Listener: adminsettings.ListenerDTO{
				IPv4: adminsettings.ListenerAddressDTO{Host: administration.IPv4ListenerHost, Port: administration.IPv4ListenerPort},
				IPv6: adminsettings.ListenerAddressDTO{Host: administration.IPv6ListenerHost, Port: administration.IPv6ListenerPort},
			},
			ApplicationVersion: administration.ApplicationVersion, StartedAt: administration.StartedAt,
		})
		if err != nil {
			return nil, fmt.Errorf("create admin settings service: %w", err)
		}
		adminSettingsRoutes, err = adminsettings.NewRoutes(adminSettingsService, identityService)
		if err != nil {
			return nil, fmt.Errorf("create admin settings routes: %w", err)
		}
	}
	adminAuthRoutes, err := adminauth.NewRoutes(identityService, resolved, ratelimit.NewDatabaseLimiter(db.Pool))
	if err != nil {
		return nil, fmt.Errorf("create admin authentication routes: %w", err)
	}

	if resolved.Environment == config.Production {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	adminAssets, err := adminweb.New(resolved.Paths.AdminWebDirectory)
	if err != nil {
		return nil, fmt.Errorf("configure admin web assets: %w", err)
	}
	cors := httpserver.UnrestrictedCORSConfig()
	readiness := &dependencyReadiness{database: db, storage: objects}
	engine, err := httpserver.New(httpserver.Options{
		CORS:           cors,
		RequestLimits:  httpserver.DefaultRequestLimits(),
		Readiness:      readiness,
		Metrics:        metrics,
		TrustedProxies: append([]string(nil), resolved.HTTP.TrustedProxyAddresses...),
		RegisterRoutes: func(engine *gin.Engine) {
			adminAssets.Register(engine)
			objectProxy.Register(engine)
			identityRoutes.Register(engine)
			adminMediaRoutes.Register(engine)
			adminMetadataRoutes.Register(engine)
			adminJobsRoutes.Register(engine)
			adminSourcesRoutes.Register(engine)
			tagRoutes.Register(engine)
			catalogRoutes.Register(engine)
			playbackRoutes.Register(engine)
			profileRoutes.Register(engine)
			libraryRoutes.Register(engine)
			playlistRoutes.Register(engine)
			adminAuthRoutes.Register(engine)
			adminManagementRoutes.Register(engine)
			adminCatalogRoutes.Register(engine)
			adminMutationRoutes.Register(engine)
			adminAuditRoutes.Register(engine)
			if adminSettingsRoutes != nil {
				adminSettingsRoutes.Register(engine)
			}
			if options.RegisterRoutes != nil {
				options.RegisterRoutes(engine)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create HTTP application: %w", err)
	}
	if options.StartBackground {
		mediaWorker, err = mediaworker.NewProduction(mediaworker.ProductionOptions{
			Database: db.Pool, Storage: resolved.Storage, Media: resolved.Media,
			Logger: workerLogger,
		})
		if err != nil {
			return nil, fmt.Errorf("create media worker: %w", err)
		}
		retentionDatabase, err := retention.NewPostgresDatabase(db.Pool)
		if err != nil {
			return nil, fmt.Errorf("create retention database: %w", err)
		}
		retentionWorker, err := retention.NewWorker(retention.Dependencies{
			Database: retentionDatabase,
			Logger:   workerLogger,
		})
		if err != nil {
			return nil, fmt.Errorf("create retention worker: %w", err)
		}
		if err := sourceWorker.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize local library scan worker: %w", err)
		}
		if err := permanentDeleteWorker.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize permanent track deletion worker: %w", err)
		}
		if err := tagBatchService.Start(context.Background()); err != nil {
			return nil, fmt.Errorf("start tag scraping batch service: %w", err)
		}
		if err := artistArtworkBatchService.Start(context.Background()); err != nil {
			cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = tagBatchService.Close(cleanupContext)
			cleanupCancel()
			return nil, fmt.Errorf("start artist artwork scraping batch service: %w", err)
		}
		metadataWorkerID := "metadata-" + uuid.NewString()
		background = startBackgroundGroup(
			logger,
			backgroundTask{
				name: "media",
				run:  func(ctx context.Context) (bool, error) { return mediaWorker.RunNext(ctx) },
			},
			backgroundTask{
				name: "library-source-scan",
				run:  func(ctx context.Context) (bool, error) { return sourceWorker.RunNextScan(ctx) },
			},
			backgroundTask{
				name: "metadata-writeback",
				run:  func(ctx context.Context) (bool, error) { return metadataWorker.RunNext(ctx, metadataWorkerID) },
			},
			backgroundTask{
				name: "track-permanent-delete",
				run:  func(ctx context.Context) (bool, error) { return permanentDeleteWorker.RunNext(ctx) },
			},
			backgroundTask{
				name: "retention",
				run: func(ctx context.Context) (bool, error) {
					result, err := retentionWorker.RunIfDue(ctx, false)
					return result.Ran, err
				},
			},
		)
	}

	failed = false
	return &Runtime{
		Config: resolved, DB: db, Storage: objects, Identity: identityService,
		AdminAudit: adminAuditService, AdminCatalog: adminCatalogService, AdminJobs: adminJobsService,
		AdminManagement: adminManagementService, AdminMedia: adminMediaService,
		AdminMetadata: adminMetadataService, AdminMutation: adminMutationService,
		AdminSettings: adminSettingsService, AdminSources: adminSourcesService,
		AdminTagScraping: tagScrapingService, AdminTagBatches: tagBatchService,
		AdminArtistArtworkBatches: artistArtworkBatchService,
		Catalog:                   catalogService, Library: libraryService, Playback: playbackService,
		Playlist: playlistService, Profile: profileService, Metrics: metrics, Handler: engine, ready: readiness,
		events: events, background: background, mediaWorker: mediaWorker,
	}, nil
}

type adminArtworkPresenter struct {
	delegate interface {
		Artworks(context.Context, []string) (map[string]playlist.ArtworkDTO, error)
	}
}

func (presenter adminArtworkPresenter) Artworks(
	ctx context.Context,
	assetIDs []string,
) (map[string]catalog.ArtworkDTO, error) {
	items, err := presenter.delegate.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]catalog.ArtworkDTO, len(items))
	for assetID, item := range items {
		result[assetID] = catalog.ArtworkDTO(item)
	}
	return result, nil
}

func (runtime *Runtime) Ready(ctx context.Context) error {
	if runtime == nil || runtime.ready == nil {
		return errors.New("runtime is not initialized")
	}
	return runtime.ready.Check(ctx)
}

func (runtime *Runtime) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = runtime.CloseContext(ctx)
}

func (runtime *Runtime) CloseContext(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	var closeErr error
	if runtime.background != nil {
		closeErr = runtime.background.Close(ctx)
	}
	if runtime.mediaWorker != nil {
		closeErr = errors.Join(closeErr, runtime.mediaWorker.Close())
	}
	if runtime.AdminTagBatches != nil {
		closeErr = errors.Join(closeErr, runtime.AdminTagBatches.Close(ctx))
	}
	if runtime.AdminArtistArtworkBatches != nil {
		closeErr = errors.Join(closeErr, runtime.AdminArtistArtworkBatches.Close(ctx))
	}
	if runtime.events != nil {
		runtime.events.Close()
	}
	if runtime.Metrics != nil {
		runtime.Metrics.Close()
	}
	if runtime.DB != nil {
		runtime.DB.Close()
	}
	return closeErr
}

type dependencyReadiness struct {
	database *database.Pool
	storage  *storage.Client
}

func (check *dependencyReadiness) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- check.database.Ping(ctx) }()
	go func() { errorsChannel <- check.storage.Ping(ctx) }()
	for range 2 {
		if err := <-errorsChannel; err != nil {
			return errors.Join(errors.New("runtime dependency is unavailable"), err)
		}
	}
	return nil
}
