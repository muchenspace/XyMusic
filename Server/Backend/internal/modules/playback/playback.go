package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"xymusic/server/internal/shared/apperror"
)

type Input struct {
	PreferredQuality PreferredQuality `json:"preferredQuality"`
	AcceptedCodecs   []string         `json:"acceptedCodecs,omitempty"`
}

type DescriptorDTO struct {
	TrackID         string           `json:"trackId"`
	SessionID       string           `json:"sessionId"`
	SelectedQuality PreferredQuality `json:"selectedQuality"`
	StreamURL       string           `json:"streamUrl"`
	ExpiresAt       string           `json:"expiresAt"`
	MimeType        string           `json:"mimeType"`
	Codec           string           `json:"codec"`
	Container       string           `json:"container"`
	Bitrate         int              `json:"bitrate"`
	SampleRate      *int             `json:"sampleRate"`
	ContentLength   *int64           `json:"contentLength"`
	CacheKey        string           `json:"cacheKey"`
}

type Service struct {
	resolver   SourceResolver
	selector   *ProfileSelector
	signer     *TicketSigner
	transcoder *TranscodeSessionManager
	ttl        time.Duration
	now        func() time.Time
}

func NewService(
	resolver SourceResolver,
	selector *ProfileSelector,
	signer *TicketSigner,
	transcoder *TranscodeSessionManager,
	ttl time.Duration,
) (*Service, error) {
	if resolver == nil || selector == nil || signer == nil || transcoder == nil {
		return nil, errors.New("playback service requires resolver, selector, signer, and transcoder")
	}
	if ttl <= 0 {
		return nil, errors.New("playback stream TTL must be positive")
	}
	return &Service{
		resolver:   resolver,
		selector:   selector,
		signer:     signer,
		transcoder: transcoder,
		ttl:        ttl,
		now:        time.Now,
	}, nil
}

func (s *Service) CreateGrant(ctx context.Context, userID, trackID string, input Input) (DescriptorDTO, error) {
	if !validQuality(input.PreferredQuality) {
		return DescriptorDTO{}, apperror.Validation("preferredQuality is invalid")
	}

	source, err := s.resolver.ResolveSource(ctx, trackID)
	if err != nil {
		return DescriptorDTO{}, err
	}

	profile, err := s.selector.SelectProfile(
		input.PreferredQuality,
		input.AcceptedCodecs,
		source.SourcePath,
		source.Bitrate,
		source.SampleRate,
	)
	if err != nil {
		return DescriptorDTO{}, err
	}
	// A cue track is a segment of a larger source file. Serving the source
	// directly would expose the complete image, so retain trimming via FFmpeg.
	if profile.Direct && (source.CueStartTimeMs != nil || source.CueEndTimeMs != nil) {
		profile.Direct = false
	}

	sessionID := uuid.NewString()
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)

	ticket, err := s.signer.Sign(TicketClaims{
		UserID:    userID,
		TrackID:   trackID,
		SessionID: sessionID,
		Quality:   string(profile.Quality),
		Codec:     profile.Codec,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return DescriptorDTO{}, fmt.Errorf("sign playback ticket: %w", err)
	}

	cacheVersion := source.ChecksumSHA256
	if cacheVersion == "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", source.SourcePath, source.SizeBytes)))
		cacheVersion = hex.EncodeToString(digest[:])
	}
	segmentVersion := fmt.Sprintf("%d:%d:%d", optionalInt64Value(source.CueStartTimeMs), optionalInt64Value(source.CueEndTimeMs), source.DurationMs)
	cacheKey := fmt.Sprintf("track:%s:%s:%s:%s:%s:%s", trackID, cacheVersion, profile.Quality, profile.Codec, profile.Container, segmentVersion)

	s.transcoder.RegisterSession(TranscodeSessionParams{
		SessionID:  sessionID,
		TrackID:    trackID,
		SourcePath: source.SourcePath,
		CacheKey:   cacheKey,
		CueStartMs: source.CueStartTimeMs,
		CueEndMs:   source.CueEndTimeMs,
		Profile:    profile,
		ExpiresAt:  expiresAt,
	})

	streamURL := fmt.Sprintf("/api/v1/playback/streams/%s?ticket=%s", sessionID, ticket)
	var contentLength *int64
	if profile.Direct && source.SizeBytes > 0 {
		contentLength = &source.SizeBytes
	}

	return DescriptorDTO{
		TrackID:         trackID,
		SessionID:       sessionID,
		SelectedQuality: profile.Quality,
		StreamURL:       streamURL,
		ExpiresAt:       formatTime(expiresAt),
		MimeType:        profile.MimeType,
		Codec:           profile.Codec,
		Container:       profile.Container,
		Bitrate:         profile.Bitrate,
		SampleRate:      profile.SampleRate,
		ContentLength:   contentLength,
		CacheKey:        cacheKey,
	}, nil
}

func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return -1
	}
	return *value
}

func validQuality(value PreferredQuality) bool {
	return value == QualityDataSaver || value == QualityStandard || value == QualityHigh || value == QualityLossless
}

func formatTime(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
