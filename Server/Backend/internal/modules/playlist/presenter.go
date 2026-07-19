package playlist

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

type artworkURLSigner interface {
	PresignedGet(context.Context, string, time.Duration) (string, error)
}

type userProjection struct {
	ID            string
	Username      string
	DisplayName   string
	AvatarAssetID *string
}

type artworkProjection struct {
	ID             string
	ObjectKey      string
	MimeType       string
	ChecksumSHA256 *string
	Width          *int
	Height         *int
	UpdatedAt      time.Time
}

type userProjectionStore interface {
	Users(context.Context, []string) ([]userProjection, error)
	Artworks(context.Context, []string) ([]artworkProjection, error)
}

type postgresUserProjectionStore struct {
	pool *pgxpool.Pool
}

func (store postgresUserProjectionStore) Users(ctx context.Context, userIDs []string) ([]userProjection, error) {
	if len(userIDs) == 0 {
		return []userProjection{}, nil
	}
	rows, err := store.pool.Query(ctx, `
		SELECT u.id, u.username, p.display_name, p.avatar_asset_id
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		WHERE u.id = ANY($1::uuid[])
	`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("query playlist users: %w", err)
	}
	defer rows.Close()
	result := make([]userProjection, 0, len(userIDs))
	for rows.Next() {
		var record userProjection
		if err := rows.Scan(&record.ID, &record.Username, &record.DisplayName, &record.AvatarAssetID); err != nil {
			return nil, fmt.Errorf("scan playlist user: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playlist users: %w", err)
	}
	return result, nil
}

func (store postgresUserProjectionStore) Artworks(ctx context.Context, assetIDs []string) ([]artworkProjection, error) {
	if len(assetIDs) == 0 {
		return []artworkProjection{}, nil
	}
	rows, err := store.pool.Query(ctx, `
		SELECT id, object_key, mime_type, checksum_sha256, width, height, updated_at
		FROM media_assets
		WHERE id = ANY($1::uuid[]) AND status = 'READY'
	`, assetIDs)
	if err != nil {
		return nil, fmt.Errorf("query playlist artwork: %w", err)
	}
	defer rows.Close()
	result := make([]artworkProjection, 0, len(assetIDs))
	for rows.Next() {
		var record artworkProjection
		if err := rows.Scan(
			&record.ID, &record.ObjectKey, &record.MimeType, &record.ChecksumSHA256,
			&record.Width, &record.Height, &record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan playlist artwork: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playlist artwork: %w", err)
	}
	return result, nil
}

// ProductionUserPresenter supplies the profile and artwork projections used
// by playlist responses without coupling playlist persistence to identity.
type ProductionUserPresenter struct {
	repository userProjectionStore
	signer     artworkURLSigner
	ttl        time.Duration
	clock      Clock
}

func NewProductionUserPresenter(
	pool *pgxpool.Pool,
	signer artworkURLSigner,
	ttl time.Duration,
) (*ProductionUserPresenter, error) {
	if pool == nil {
		return nil, errors.New("playlist user presenter database is required")
	}
	return newProductionUserPresenter(postgresUserProjectionStore{pool: pool}, signer, ttl, SystemClock{})
}

func newProductionUserPresenter(
	repository userProjectionStore,
	signer artworkURLSigner,
	ttl time.Duration,
	clock Clock,
) (*ProductionUserPresenter, error) {
	if repository == nil {
		return nil, errors.New("playlist user projection repository is required")
	}
	if signer == nil {
		return nil, errors.New("playlist artwork URL signer is required")
	}
	if ttl <= 0 {
		return nil, errors.New("playlist artwork URL TTL must be positive")
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &ProductionUserPresenter{repository: repository, signer: signer, ttl: ttl, clock: clock}, nil
}

func (presenter *ProductionUserPresenter) UserSummary(ctx context.Context, userID string) (UserSummaryDTO, error) {
	summaries, err := presenter.UserSummaries(ctx, []string{userID})
	if err != nil {
		return UserSummaryDTO{}, err
	}
	result, exists := summaries[userID]
	if !exists {
		return UserSummaryDTO{}, apperror.NotFound("User no longer exists")
	}
	return result, nil
}

func (presenter *ProductionUserPresenter) UserSummaries(
	ctx context.Context,
	userIDs []string,
) (map[string]UserSummaryDTO, error) {
	unique := uniqueNonEmpty(userIDs)
	users, err := presenter.repository.Users(ctx, unique)
	if err != nil {
		return nil, err
	}
	assetIDs := make([]string, 0, len(users))
	for _, user := range users {
		if user.AvatarAssetID != nil {
			assetIDs = append(assetIDs, *user.AvatarAssetID)
		}
	}
	artworks, err := presenter.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]UserSummaryDTO, len(users))
	for _, user := range users {
		var avatar *ArtworkDTO
		if user.AvatarAssetID != nil {
			if artwork, exists := artworks[*user.AvatarAssetID]; exists {
				copy := artwork
				avatar = &copy
			}
		}
		result[user.ID] = UserSummaryDTO{
			ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Avatar: avatar,
		}
	}
	return result, nil
}

func (presenter *ProductionUserPresenter) Artworks(
	ctx context.Context,
	assetIDs []string,
) (map[string]ArtworkDTO, error) {
	records, err := presenter.repository.Artworks(ctx, uniqueNonEmpty(assetIDs))
	if err != nil {
		return nil, err
	}
	expiresAt := formatTimestamp(presenter.clock.Now().UTC().Add(presenter.ttl))
	result := make(map[string]ArtworkDTO, len(records))
	for _, record := range records {
		url, err := presenter.signer.PresignedGet(ctx, record.ObjectKey, presenter.ttl)
		if err != nil {
			return nil, fmt.Errorf("sign playlist artwork URL: %w", err)
		}
		cacheVersion := strconv.FormatInt(record.UpdatedAt.UnixMilli(), 10)
		if record.ChecksumSHA256 != nil {
			cacheVersion = *record.ChecksumSHA256
		}
		result[record.ID] = ArtworkDTO{
			AssetID: record.ID, URL: url, CacheKey: record.ID + ":" + cacheVersion,
			MimeType: record.MimeType, ExpiresAt: &expiresAt, Width: record.Width, Height: record.Height,
		}
	}
	return result, nil
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

var _ UserPresenter = (*ProductionUserPresenter)(nil)
