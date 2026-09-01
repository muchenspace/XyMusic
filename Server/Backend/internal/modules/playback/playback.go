package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"xymusic/server/internal/shared/apperror"
)

type Input struct {
	PreferredQuality PreferredQuality `json:"preferredQuality"`
	AcceptedCodecs   []string         `json:"acceptedCodecs,omitempty"`
	StreamProtocol   StreamProtocol   `json:"streamProtocol,omitempty"`
	StartPositionMs  int64            `json:"startPositionMs,omitempty"`
}

type DescriptorDTO struct {
	TrackID         string           `json:"trackId"`
	SessionID       string           `json:"sessionId"`
	SelectedQuality PreferredQuality `json:"selectedQuality"`
	StreamURL       string           `json:"streamUrl"`
	DurationMs      int64            `json:"durationMs"`
	ExpiresAt       string           `json:"expiresAt"`
	MimeType        string           `json:"mimeType"`
	Codec           string           `json:"codec"`
	Container       string           `json:"container"`
	Bitrate         int              `json:"bitrate"`
	SampleRate      *int             `json:"sampleRate"`
	ContentLength   *int64           `json:"contentLength"`
	CacheKey        string           `json:"cacheKey"`
	StreamProtocol  StreamProtocol   `json:"streamProtocol"`
	StartPositionMs int64            `json:"startPositionMs"`
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
	if input.StartPositionMs < 0 {
		return DescriptorDTO{}, apperror.Validation("startPositionMs must not be negative")
	}
	rawProtocol := strings.TrimSpace(string(input.StreamProtocol))
	requestedProtocol, err := normalizeStreamProtocol(input.StreamProtocol)
	if err != nil {
		return DescriptorDTO{}, apperror.Validation(err.Error())
	}
	streamProtocol := requestedProtocol

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
	hasCue := source.CueStartTimeMs != nil || source.CueEndTimeMs != nil
	if hasCue && profile.Direct {
		// The LOSSLESS profile intentionally returns the original file. A cue
		// cannot use that representation because the original file contains
		// audio outside the cue. Re-select a truthful encoded profile instead of
		// changing Direct after protocol selection and accidentally leaving the
		// session on the progressive path.
		cueQuality := input.PreferredQuality
		if cueQuality == QualityLossless {
			cueQuality = QualityStandard
		}
		profile, err = s.selector.SelectProfile(
			cueQuality,
			input.AcceptedCodecs,
			source.SourcePath,
			source.Bitrate,
			source.SampleRate,
		)
		if err != nil {
			return DescriptorDTO{}, err
		}
	}

	// A direct source is already a finite, byte-addressable representation and
	// must stay progressive. A transcoded representation is HLS by default so
	// playback can begin from the first segment and seek by time while FFmpeg
	// continues in the background. Callers that explicitly request progressive
	// are asking for a complete file (for example, an offline download).
	if profile.Direct {
		streamProtocol = StreamProtocolProgressive
	} else {
		if rawProtocol == "" {
			streamProtocol = StreamProtocolHLS
		} else {
			streamProtocol = requestedProtocol
		}
	}
	if input.StartPositionMs > 0 {
		if streamProtocol != StreamProtocolHLS {
			return DescriptorDTO{}, apperror.Validation("startPositionMs requires HLS playback")
		}
		if input.StartPositionMs >= source.DurationMs {
			return DescriptorDTO{}, apperror.Validation("startPositionMs must be before the end of the track")
		}
	}
	if streamProtocol == StreamProtocolHLS && !strings.EqualFold(profile.Codec, "aac") {
		return DescriptorDTO{}, apperror.Unprocessable(
			apperror.CodeTrackNotPlayable,
			"HLS playback requires AAC output",
			nil,
		)
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
	cacheKey := fmt.Sprintf("track:%s:%s:%s:%s:%s:%s:%s", trackID, cacheVersion, profile.Quality, profile.Codec, profile.Container, streamProtocol, segmentVersion)
	sessionCacheKey := cacheKey
	if streamProtocol == StreamProtocolHLS && input.StartPositionMs > 0 {
		// A directed-start HLS job begins at the requested position. The same
		// target reuses an in-flight/short-lived job across sessions, but it is
		// never added to the long-lived byte cache.
		sessionCacheKey = fmt.Sprintf("%s%s:start:%d", hlsDirectedKeyPrefix, cacheKey, input.StartPositionMs)
	}

	s.transcoder.RegisterSession(TranscodeSessionParams{
		SessionID:       sessionID,
		UserID:          userID,
		TrackID:         trackID,
		SourcePath:      source.SourcePath,
		CacheKey:        sessionCacheKey,
		DurationMs:      source.DurationMs,
		CueStartMs:      source.CueStartTimeMs,
		CueEndMs:        source.CueEndTimeMs,
		Profile:         profile,
		Delivery:        streamProtocol,
		StartPositionMs: input.StartPositionMs,
		ExpiresAt:       expiresAt,
	})

	streamURL := fmt.Sprintf("/api/v1/playback/streams/%s?ticket=%s", sessionID, ticket)
	if streamProtocol == StreamProtocolHLS {
		streamURL = fmt.Sprintf("/api/v1/playback/streams/%s/index.m3u8?ticket=%s", sessionID, ticket)
	}
	var contentLength *int64
	if profile.Direct && source.SizeBytes > 0 {
		contentLength = &source.SizeBytes
	}

	return DescriptorDTO{
		TrackID:         trackID,
		SessionID:       sessionID,
		SelectedQuality: profile.Quality,
		StreamURL:       streamURL,
		DurationMs:      source.DurationMs,
		ExpiresAt:       formatTime(expiresAt),
		MimeType:        profile.MimeType,
		Codec:           profile.Codec,
		Container:       profile.Container,
		Bitrate:         profile.Bitrate,
		SampleRate:      profile.SampleRate,
		ContentLength:   contentLength,
		CacheKey:        sessionCacheKey,
		StreamProtocol:  streamProtocol,
		StartPositionMs: input.StartPositionMs,
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
