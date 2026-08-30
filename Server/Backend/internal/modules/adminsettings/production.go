package adminsettings

import (
	"context"
	"fmt"
	"os"

	"xymusic/server/internal/config"
)

type ProductionMediaStorageFactory struct{}

func (ProductionMediaStorageFactory) Open(cfg config.MediaStorage) (MediaStorageProbe, error) {
	return &productionMediaStorage{
		assetDir:     cfg.AssetDirectory,
		transcodeDir: cfg.TranscodeDirectory,
	}, nil
}

type productionMediaStorage struct {
	assetDir     string
	transcodeDir string
}

func (storage *productionMediaStorage) Probe(ctx context.Context) error {
	if fi, err := os.Stat(storage.assetDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("media asset directory does not exist or is not a directory: %s", storage.assetDir)
	}
	if fi, err := os.Stat(storage.transcodeDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("media transcode directory does not exist or is not a directory: %s", storage.transcodeDir)
	}
	return nil
}

func (storage *productionMediaStorage) EnsureDirectories(ctx context.Context) error {
	if err := os.MkdirAll(storage.assetDir, 0755); err != nil {
		return fmt.Errorf("create media asset directory: %w", err)
	}
	if err := os.MkdirAll(storage.transcodeDir, 0755); err != nil {
		return fmt.Errorf("create media transcode directory: %w", err)
	}
	return nil
}

func (storage *productionMediaStorage) Close() {}
