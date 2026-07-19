package playback

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

type PreferredQuality string

const (
	QualityAuto      PreferredQuality = "AUTO"
	QualityDataSaver PreferredQuality = "DATA_SAVER"
	QualityStandard  PreferredQuality = "STANDARD"
	QualityHigh      PreferredQuality = "HIGH"
	QualityLossless  PreferredQuality = "LOSSLESS"
)

type Input struct {
	PreferredQuality PreferredQuality `json:"preferredQuality"`
	AcceptedCodecs   []string         `json:"acceptedCodecs,omitempty"`
}

type GrantDTO struct {
	TrackID         string           `json:"trackId"`
	VariantID       string           `json:"variantId"`
	SelectedQuality PreferredQuality `json:"selectedQuality"`
	URL             string           `json:"url"`
	ExpiresAt       string           `json:"expiresAt"`
	MimeType        string           `json:"mimeType"`
	Codec           string           `json:"codec"`
	Container       string           `json:"container"`
	Bitrate         int              `json:"bitrate"`
	SampleRate      *int             `json:"sampleRate"`
	ContentLength   int64            `json:"contentLength"`
	ChecksumSHA256  *string          `json:"checksumSha256"`
	CacheKey        string           `json:"cacheKey"`
}

type Variant struct {
	ID             string
	Quality        string
	MimeType       string
	Codec          string
	Container      string
	Bitrate        int
	SampleRate     *int
	ObjectKey      string
	ContentLength  int64
	ChecksumSHA256 *string
	AssetUpdatedAt time.Time
}

type Store interface {
	ReadyVariants(context.Context, string, []string) ([]Variant, error)
	PublishedTrackExists(context.Context, string) (bool, error)
}

type URLSigner interface {
	PresignedGet(context.Context, string, time.Duration) (string, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) ReadyVariants(ctx context.Context, trackID string, codecs []string) ([]Variant, error) {
	query := `
		select v.id, v.quality, v.mime_type, v.codec, v.container, v.bitrate,
		       v.sample_rate, a.object_key, a.size_bytes, a.checksum_sha256, a.updated_at
		from track_variants v
		join tracks t on t.id = v.track_id
		join media_assets a on a.id = v.asset_id
		where t.id = $1 and t.status = 'READY' and t.published_at is not null
		  and v.track_id = $1 and v.status = 'READY' and a.status = 'READY'`
	arguments := []any{trackID}
	if len(codecs) > 0 {
		query += " and v.codec = any($2::text[])"
		arguments = append(arguments, codecs)
	}
	query += " order by v.bitrate asc"
	rows, err := repository.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query playback variants: %w", err)
	}
	defer rows.Close()
	variants := make([]Variant, 0)
	for rows.Next() {
		var variant Variant
		if err := rows.Scan(
			&variant.ID, &variant.Quality, &variant.MimeType, &variant.Codec,
			&variant.Container, &variant.Bitrate, &variant.SampleRate, &variant.ObjectKey,
			&variant.ContentLength, &variant.ChecksumSHA256, &variant.AssetUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan playback variant: %w", err)
		}
		variants = append(variants, variant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query playback variants: %w", err)
	}
	return variants, nil
}

func (repository *Repository) PublishedTrackExists(ctx context.Context, trackID string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx, `
		select exists(
			select 1 from tracks
			where id = $1 and status = 'READY' and published_at is not null
		)`, trackID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check playable track: %w", err)
	}
	return exists, nil
}

type Service struct {
	store  Store
	signer URLSigner
	ttl    time.Duration
	now    func() time.Time
}

func NewService(store Store, signer URLSigner, ttl time.Duration) (*Service, error) {
	if store == nil || signer == nil {
		return nil, errors.New("playback store and URL signer are required")
	}
	if ttl <= 0 {
		return nil, errors.New("playback signed URL TTL must be positive")
	}
	return &Service{store: store, signer: signer, ttl: ttl, now: time.Now}, nil
}

func (service *Service) CreateGrant(ctx context.Context, trackID string, input Input) (GrantDTO, error) {
	codecs, err := normalizeCodecs(input.AcceptedCodecs)
	if err != nil {
		return GrantDTO{}, err
	}
	variants, err := service.store.ReadyVariants(ctx, trackID, codecs)
	if err != nil {
		return GrantDTO{}, err
	}
	selected := SelectVariant(variants, input.PreferredQuality)
	if selected == nil {
		exists, err := service.store.PublishedTrackExists(ctx, trackID)
		if err != nil {
			return GrantDTO{}, err
		}
		if !exists {
			return GrantDTO{}, apperror.NotFound("Track was not found")
		}
		return GrantDTO{}, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "No compatible playback variant is available", nil)
	}
	url, err := service.signer.PresignedGet(ctx, selected.ObjectKey, service.ttl)
	if err != nil {
		return GrantDTO{}, fmt.Errorf("create playback URL: %w", err)
	}
	now := service.now().UTC()
	cacheVersion := fmt.Sprintf("%d", selected.AssetUpdatedAt.UnixMilli())
	if selected.ChecksumSHA256 != nil {
		cacheVersion = *selected.ChecksumSHA256
	}
	return GrantDTO{
		TrackID: trackID, VariantID: selected.ID, SelectedQuality: PreferredQuality(selected.Quality),
		URL: url, ExpiresAt: formatTime(now.Add(service.ttl)), MimeType: selected.MimeType,
		Codec: selected.Codec, Container: selected.Container, Bitrate: selected.Bitrate,
		SampleRate: selected.SampleRate, ContentLength: selected.ContentLength,
		ChecksumSHA256: selected.ChecksumSHA256, CacheKey: selected.ID + ":" + cacheVersion,
	}, nil
}

func SelectVariant(variants []Variant, preferred PreferredQuality) *Variant {
	if len(variants) == 0 {
		return nil
	}
	ordered := append([]Variant(nil), variants...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftRank, rightRank := qualityRank(ordered[left].Quality), qualityRank(ordered[right].Quality)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return ordered[left].Bitrate < ordered[right].Bitrate
	})
	if preferred == QualityAuto {
		selected := ordered[len(ordered)-1]
		return &selected
	}
	maximum, ok := qualityRanks[preferred]
	if !ok {
		return nil
	}
	for index := len(ordered) - 1; index >= 0; index-- {
		if qualityRank(ordered[index].Quality) <= maximum {
			selected := ordered[index]
			return &selected
		}
	}
	selected := ordered[0]
	return &selected
}

func normalizeCodecs(values []string) ([]string, error) {
	if len(values) > 10 {
		return nil, apperror.Validation("acceptedCodecs must contain at most ten unique values")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 1 || len(value) > 30 {
			return nil, apperror.Validation("acceptedCodecs contains an invalid codec")
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, apperror.Validation("acceptedCodecs must contain at most ten unique values")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func qualityRank(value string) int {
	if rank, ok := qualityRanks[PreferredQuality(value)]; ok {
		return rank
	}
	return -1
}

var qualityRanks = map[PreferredQuality]int{QualityAuto: 4, QualityDataSaver: 0, QualityStandard: 1, QualityHigh: 2, QualityLossless: 3}

func formatTime(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
