package adminsettings

import (
	"context"
	"fmt"
	"os"

	"xymusic/server/internal/config"
)

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
