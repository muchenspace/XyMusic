package playback

import (
	"mime"
	"path/filepath"
	"strings"

	"xymusic/server/internal/shared/apperror"
)

type PreferredQuality string

const (
	QualityDataSaver PreferredQuality = "DATA_SAVER"
	QualityStandard  PreferredQuality = "STANDARD"
	QualityHigh      PreferredQuality = "HIGH"
	QualityLossless  PreferredQuality = "LOSSLESS"
)

type OutputProfile struct {
	Quality    PreferredQuality
	Codec      string
	Container  string
	MimeType   string
	Bitrate    int
	SampleRate *int
	IsLossless bool
	Direct     bool
}

type ProfileSelector struct{}

func NewProfileSelector() *ProfileSelector {
	return &ProfileSelector{}
}

func (s *ProfileSelector) SelectProfile(
	preferred PreferredQuality,
	acceptedCodecs []string,
	sourcePath string,
	sourceBitrate int,
	sourceSampleRate *int,
) (OutputProfile, error) {
	codecs, err := normalizeCodecs(acceptedCodecs)
	if err != nil {
		return OutputProfile{}, err
	}

	sourceFormat := sourceFormatForPath(sourcePath)

	// LOSSLESS means "use the original source", not "manufacture a
	// lossless version". A lossy source such as MP3 must therefore also be
	// sent as-is; transcoding cannot restore information that is not there.
	// The accepted codec list is deliberately not consulted for this branch: a
	// LOSSLESS request must never silently turn into AAC/MP3 re-encoding.
	if preferred == QualityLossless {
		if sourceFormat.codec == "" {
			return OutputProfile{}, apperror.Unprocessable(
				apperror.CodeTrackNotPlayable,
				"Original audio format cannot be identified for direct playback",
				nil,
			)
		}
		return OutputProfile{
			Quality:    QualityLossless,
			Codec:      sourceFormat.codec,
			Container:  sourceFormat.container,
			MimeType:   sourceFormat.mimeType,
			Bitrate:    max(sourceBitrate, 1),
			SampleRate: sourceSampleRate,
			IsLossless: sourceFormat.lossless,
			Direct:     true,
		}, nil
	}

	// LOSSLESS returns above, so all profiles reaching this branch are
	// explicitly bitrate-constrained transcoded variants.
	effectiveQuality := preferred

	targetBitrate := 128000
	switch effectiveQuality {
	case QualityDataSaver:
		targetBitrate = 64000
	case QualityStandard:
		targetBitrate = 128000
	case QualityHigh:
		targetBitrate = 256000
	}

	// Codec priority: aac -> mp3 -> opus -> (flac if accepted)
	selectedCodec := selectCodec(codecs)
	if selectedCodec == "" {
		return OutputProfile{}, apperror.Unprocessable(
			apperror.CodeTrackNotPlayable,
			"No compatible audio codec supported by server and client",
			nil,
		)
	}

	container, mimeType := containerAndMime(selectedCodec)
	sampleRate := sourceSampleRate
	if sampleRate == nil {
		defaultSR := 44100
		sampleRate = &defaultSR
	}

	return OutputProfile{
		Quality:    effectiveQuality,
		Codec:      selectedCodec,
		Container:  container,
		MimeType:   mimeType,
		Bitrate:    targetBitrate,
		SampleRate: sampleRate,
		IsLossless: selectedCodec == "flac",
	}, nil
}

func selectCodec(accepted []string) string {
	if len(accepted) == 0 {
		return "aac" // default priority
	}
	// Only codecs with an explicit FFmpeg mapping may be advertised. Returning
	// an arbitrary client token here used to produce AAC bytes with a false
	// codec/container descriptor.
	for _, supported := range []string{"aac", "mp3", "opus", "flac"} {
		if containsCodec(accepted, supported) {
			return supported
		}
	}
	return ""
}

func containerAndMime(codec string) (container string, mimeType string) {
	switch strings.ToLower(codec) {
	case "aac":
		return "m4a", "audio/mp4"
	case "aac_raw":
		return "aac", "audio/aac"
	case "mp3":
		return "mp3", "audio/mpeg"
	case "opus":
		return "ogg", "audio/ogg"
	case "flac":
		return "flac", "audio/flac"
	case "ogg":
		return "ogg", "audio/ogg"
	case "m4a", "mp4":
		return "m4a", "audio/mp4"
	case "wav":
		return "wav", "audio/wav"
	case "aiff", "aif":
		return "aiff", "audio/aiff"
	case "ape":
		return "ape", "audio/ape"
	case "alac":
		return "m4a", "audio/mp4"
	default:
		codec = strings.ToLower(strings.TrimSpace(codec))
		if codec == "" {
			return "", ""
		}
		return codec, mime.TypeByExtension("." + codec)
	}
}

type sourceFormat struct {
	codec     string
	container string
	mimeType  string
	lossless  bool
}

func sourceFormatForPath(path string) sourceFormat {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "flac":
		return sourceFormat{codec: "flac", container: "flac", mimeType: "audio/flac", lossless: true}
	case "mp3":
		return sourceFormat{codec: "mp3", container: "mp3", mimeType: "audio/mpeg"}
	case "m4a", "mp4":
		return sourceFormat{codec: "m4a", container: "m4a", mimeType: "audio/mp4"}
	case "ogg":
		return sourceFormat{codec: "ogg", container: "ogg", mimeType: "audio/ogg"}
	case "opus":
		return sourceFormat{codec: "opus", container: "ogg", mimeType: "audio/ogg"}
	case "aac":
		return sourceFormat{codec: "aac_raw", container: "aac", mimeType: "audio/aac"}
	case "wav":
		return sourceFormat{codec: "wav", container: "wav", mimeType: "audio/wav", lossless: true}
	case "aiff", "aif":
		return sourceFormat{codec: "aiff", container: "aiff", mimeType: "audio/aiff", lossless: true}
	case "ape":
		return sourceFormat{codec: "ape", container: "ape", mimeType: "audio/ape", lossless: true}
	case "alac":
		return sourceFormat{codec: "alac", container: "m4a", mimeType: "audio/mp4", lossless: true}
	default:
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		if ext == "" {
			return sourceFormat{}
		}
		mimeType := mime.TypeByExtension("." + ext)
		return sourceFormat{codec: ext, container: ext, mimeType: mimeType}
	}
}

func isLosslessExtension(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "flac", "wav", "ape", "aiff", "aif", "alac":
		return true
	default:
		return false
	}
}

func containsCodec(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
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
