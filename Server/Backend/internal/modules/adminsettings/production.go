package adminsettings

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	platformidempotency "xymusic/server/internal/platform/idempotency"
	"xymusic/server/internal/platform/security"
)

// ProductionDirectoryProbe implements DirectoryProbe with the os package.
type ProductionDirectoryProbe struct{}

func (ProductionDirectoryProbe) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }

func (ProductionDirectoryProbe) Open(path string) (io.Closer, error) { return os.Open(path) }

type ProductionMediaStorageFactory struct{}

func (ProductionMediaStorageFactory) Open(cfg config.MediaStorage) (MediaStorageProbe, error) {
	return &productionMediaStorage{assetDir: cfg.AssetDirectory}, nil
}

type productionMediaStorage struct {
	assetDir string
}

func (storage *productionMediaStorage) Probe(ctx context.Context) error {
	if fi, err := os.Stat(storage.assetDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("media asset directory does not exist or is not a directory: %s", storage.assetDir)
	}
	return nil
}

func (storage *productionMediaStorage) EnsureDirectories(ctx context.Context) error {
	if err := os.MkdirAll(storage.assetDir, 0755); err != nil {
		return fmt.Errorf("create media asset directory: %w", err)
	}
	return nil
}

func (storage *productionMediaStorage) Close() {}

// ProductionDatabaseFactory adapts internal/platform/database to the settings
// DatabaseFactory port.
type ProductionDatabaseFactory struct{}

func (ProductionDatabaseFactory) Open(ctx context.Context, cfg config.Database) (Database, error) {
	pool, err := database.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &productionSettingsDatabase{pool: pool}, nil
}

func (ProductionDatabaseFactory) Migrate(ctx context.Context, candidate Database, directory string) error {
	adapted, ok := candidate.(*productionSettingsDatabase)
	if !ok {
		return fmt.Errorf("admin settings database adapter is invalid")
	}
	return database.RunMigrations(ctx, adapted.pool.Pool, directory)
}

type productionSettingsDatabase struct {
	pool *database.Pool
}

// NewProductionDatabase adapts a platform database pool to the settings
// Database port.
func NewProductionDatabase(pool *database.Pool) Database {
	return &productionSettingsDatabase{pool: pool}
}

func (connection *productionSettingsDatabase) Ping(ctx context.Context) error {
	return connection.pool.Ping(ctx)
}

func (connection *productionSettingsDatabase) Close() { connection.pool.Close() }

func (connection *productionSettingsDatabase) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return connection.pool.QueryRow(ctx, sql, args...)
}

func (connection *productionSettingsDatabase) poolReference() *pgxpool.Pool {
	return connection.pool.Pool
}

// ProductionIdempotencyFactory builds idempotency executors for candidate
// databases.
type ProductionIdempotencyFactory struct{}

func (ProductionIdempotencyFactory) New(candidate Database, encryptionSecret string) (Idempotency, error) {
	adapted, ok := candidate.(*productionSettingsDatabase)
	if !ok {
		return nil, fmt.Errorf("admin settings database adapter is invalid")
	}
	cipher, err := security.NewPayloadCipher(encryptionSecret)
	if err != nil {
		return nil, err
	}
	return productionIdempotency{service: platformidempotency.New(adapted.poolReference(), cipher)}, nil
}

type productionIdempotency struct {
	service *platformidempotency.Service
}

func (idempotency productionIdempotency) Execute(
	ctx context.Context,
	actorID, scope, key string,
	payload any,
	operation func() (int, SettingsDTO, error),
) (IdempotentSettingsResult, error) {
	result, err := platformidempotency.Execute(ctx, idempotency.service, platformidempotency.Input{
		ActorID: actorID, Scope: scope, Key: key, Payload: payload,
	}, func() (platformidempotency.HTTPResult[SettingsDTO], error) {
		status, body, applyErr := operation()
		return platformidempotency.HTTPResult[SettingsDTO]{Status: status, Body: body}, applyErr
	})
	if err != nil {
		return IdempotentSettingsResult{}, err
	}
	return IdempotentSettingsResult{Status: result.Status, Body: result.Body, Replayed: result.Replayed}, nil
}
