package artworkstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/platform/ossproxy"
)

type PostgresResolver struct {
	pool *pgxpool.Pool
}

func NewPostgresResolver(pool *pgxpool.Pool) (*PostgresResolver, error) {
	if pool == nil {
		return nil, errors.New("artwork database is required")
	}
	return &PostgresResolver{pool: pool}, nil
}

func (resolver *PostgresResolver) ResolveArtwork(
	ctx context.Context,
	assetID string,
) (ossproxy.ArtworkResource, error) {
	var resource ossproxy.ArtworkResource
	err := resolver.pool.QueryRow(ctx, `
		SELECT object_key, mime_type, size_bytes, checksum_sha256, updated_at
		FROM media_assets
		WHERE id = $1 AND kind = 'ARTWORK' AND status = 'READY'
	`, assetID).Scan(
		&resource.ObjectKey,
		&resource.MimeType,
		&resource.SizeBytes,
		&resource.ChecksumSHA256,
		&resource.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ossproxy.ArtworkResource{}, ossproxy.ErrArtworkNotFound
	}
	if err != nil {
		return ossproxy.ArtworkResource{}, fmt.Errorf("resolve artwork resource: %w", err)
	}
	return resource, nil
}

var _ ossproxy.ArtworkResolver = (*PostgresResolver)(nil)
