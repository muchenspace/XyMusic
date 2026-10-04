package localassets

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AssetRecord is the persisted media asset row needed to serve artwork.
type AssetRecord struct {
	ID             string
	StoragePath    string
	Kind           string
	MimeType       string
	SizeBytes      int64
	ChecksumSHA256 *string
	Status         string
	UpdatedAt      time.Time
}

// Store is the persistence port for ready local assets.
type Store interface {
	FindReadyAsset(ctx context.Context, assetID string) (*AssetRecord, error)
}

// Repository is the PostgreSQL implementation of Store.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindReadyAsset(ctx context.Context, assetID string) (*AssetRecord, error) {
	var record AssetRecord
	err := r.pool.QueryRow(ctx, `
		SELECT id, storage_path, kind::text, mime_type, size_bytes, checksum_sha256, status::text, updated_at
		FROM media_assets
		WHERE id = $1 AND status = 'READY'`, assetID).Scan(
		&record.ID,
		&record.StoragePath,
		&record.Kind,
		&record.MimeType,
		&record.SizeBytes,
		&record.ChecksumSHA256,
		&record.Status,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query ready media asset: %w", err)
	}
	return &record, nil
}
