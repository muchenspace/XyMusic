package playback

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/timeformat"
)

type DescriptorDTO struct {
	TrackID       string `json:"trackId"`
	StreamURL     string `json:"streamUrl"`
	DurationMs    int64  `json:"durationMs"`
	ExpiresAt     string `json:"expiresAt"`
	MimeType      string `json:"mimeType"`
	Codec         string `json:"codec"`
	Container     string `json:"container"`
	Bitrate       int    `json:"bitrate"`
	SampleRate    *int   `json:"sampleRate"`
	ContentLength *int64 `json:"contentLength"`
}

type Service struct {
	resolver SourceResolver
	signer   *TicketSigner
	ttl      time.Duration
	now      func() time.Time
}

func NewService(
	resolver SourceResolver,
	signer *TicketSigner,
	ttl time.Duration,
) (*Service, error) {
	if resolver == nil || signer == nil {
		return nil, errors.New("playback service requires resolver and signer")
	}
	if ttl <= 0 {
		return nil, errors.New("playback stream TTL must be positive")
	}
	return &Service{
		resolver: resolver,
		signer:   signer,
		ttl:      ttl,
		now:      time.Now,
	}, nil
}

func (s *Service) ResolveSource(ctx context.Context, trackID string) (*ResolvedAudioSource, error) {
	return s.resolver.ResolveSource(ctx, trackID)
}

func (s *Service) CreateGrant(ctx context.Context, userID, trackID string) (DescriptorDTO, error) {
	source, err := s.resolver.ResolveSource(ctx, trackID)
	if err != nil {
		return DescriptorDTO{}, err
	}

	format := sourceFormatForPath(source.SourcePath)
	if format.codec == "" {
		return DescriptorDTO{}, apperror.Unprocessable(
			apperror.CodeTrackNotPlayable,
			"Audio format cannot be identified for direct playback",
			nil,
		)
	}

	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)

	ticket, err := s.signer.Sign(TicketClaims{
		UserID:    userID,
		TrackID:   trackID,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return DescriptorDTO{}, fmt.Errorf("sign playback ticket: %w", err)
	}

	streamURL := fmt.Sprintf("/api/v1/playback/streams/%s?ticket=%s", trackID, ticket)
	var contentLength *int64
	if source.SizeBytes > 0 {
		contentLength = &source.SizeBytes
	}

	return DescriptorDTO{
		TrackID:       trackID,
		StreamURL:     streamURL,
		DurationMs:    source.DurationMs,
		ExpiresAt:     formatTime(expiresAt),
		MimeType:      format.mimeType,
		Codec:         format.codec,
		Container:     format.container,
		Bitrate:       source.Bitrate,
		SampleRate:    source.SampleRate,
		ContentLength: contentLength,
	}, nil
}

type sourceFormat struct {
	codec     string
	container string
	mimeType  string
}

func sourceFormatForPath(path string) sourceFormat {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "flac":
		return sourceFormat{codec: "flac", container: "flac", mimeType: "audio/flac"}
	case "mp3":
		return sourceFormat{codec: "mp3", container: "mp3", mimeType: "audio/mpeg"}
	case "m4a", "mp4":
		return sourceFormat{codec: "m4a", container: "m4a", mimeType: "audio/mp4"}
	case "ogg":
		return sourceFormat{codec: "ogg", container: "ogg", mimeType: "audio/ogg"}
	case "opus":
		return sourceFormat{codec: "opus", container: "ogg", mimeType: "audio/ogg"}
	case "aac":
		return sourceFormat{codec: "aac", container: "aac", mimeType: "audio/aac"}
	case "wav":
		return sourceFormat{codec: "wav", container: "wav", mimeType: "audio/wav"}
	case "aiff", "aif":
		return sourceFormat{codec: "aiff", container: "aiff", mimeType: "audio/aiff"}
	case "ape":
		return sourceFormat{codec: "ape", container: "ape", mimeType: "audio/ape"}
	case "alac":
		return sourceFormat{codec: "alac", container: "m4a", mimeType: "audio/mp4"}
	default:
		if ext == "" {
			return sourceFormat{}
		}
		mimeType := mime.TypeByExtension("." + ext)
		return sourceFormat{codec: ext, container: ext, mimeType: mimeType}
	}
}

func formatTime(value time.Time) string {
	return timeformat.Timestamp(value)
}
