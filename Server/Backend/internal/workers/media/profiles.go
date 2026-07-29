package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var losslessCodecs = map[string]struct{}{
	"alac": {}, "ape": {}, "flac": {}, "mlp": {}, "shorten": {}, "tak": {},
	"truehd": {}, "tta": {}, "wavpack": {},
}

func AudioVariantProfiles(sourceCodec string) []AudioVariantProfile {
	profiles := []AudioVariantProfile{
		aacProfile("DATA_SAVER", 64_000),
		aacProfile("STANDARD", 128_000),
		aacProfile("HIGH", 256_000),
	}
	if IsLosslessCodec(sourceCodec) {
		profiles = append(profiles, AudioVariantProfile{
			Quality: "LOSSLESS", Extension: "flac", MIMEType: "audio/flac",
			Codec: "flac", Container: "flac",
			FFmpegArgs: []string{"-c:a", "flac", "-compression_level", "8"},
		})
	}
	return profiles
}

func IsLosslessCodec(codec string) bool {
	normalized := strings.ToLower(strings.TrimSpace(codec))
	if strings.HasPrefix(normalized, "pcm_") {
		return true
	}
	_, found := losslessCodecs[normalized]
	return found
}

func EstimatedBitrate(sizeBytes, durationMS int64) int {
	if durationMS <= 0 {
		return 1
	}
	value := math.Round(float64(sizeBytes) * 8_000 / float64(durationMS))
	if value < 1 {
		return 1
	}
	return int(value)
}

func VariantObjectKey(trackID, jobID, attemptID string, profile AudioVariantProfile) string {
	return fmt.Sprintf(
		"media/variants/%s/%s/%s/%s.%s",
		trackID, jobID, attemptID, strings.ToLower(profile.Quality), profile.Extension,
	)
}

type mediaRange struct {
	StartMS int64
	EndMS   int64
}

func mediaSegment(payload json.RawMessage, durationMS int64) (mediaRange, error) {
	values := map[string]any{}
	if len(payload) > 0 {
		decoder := json.NewDecoder(strings.NewReader(string(payload)))
		decoder.UseNumber()
		if err := decoder.Decode(&values); err != nil {
			return mediaRange{}, newWorkerError("INVALID_SEGMENT", "CUE segment is outside the source duration")
		}
	}
	startMS := int64(0)
	if value, found := values["segmentStartMs"]; found {
		parsed, err := javascriptInteger(value)
		if err != nil {
			return mediaRange{}, newWorkerError("INVALID_SEGMENT", "CUE segment is outside the source duration")
		}
		startMS = parsed
	}
	endMS := durationMS
	if value, found := values["segmentEndMs"]; found && value != nil {
		parsed, err := javascriptInteger(value)
		if err != nil {
			return mediaRange{}, newWorkerError("INVALID_SEGMENT", "CUE segment is outside the source duration")
		}
		endMS = parsed
	}
	if startMS < 0 || endMS <= startMS || endMS > durationMS+1_000 {
		return mediaRange{}, newWorkerError("INVALID_SEGMENT", "CUE segment is outside the source duration")
	}
	if endMS > durationMS {
		endMS = durationMS
	}
	return mediaRange{StartMS: startMS, EndMS: endMS}, nil
}

func javascriptInteger(value any) (int64, error) {
	var number float64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseFloat(string(typed), 64)
		if err != nil {
			return 0, err
		}
		number = parsed
	case float64:
		number = typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			number = 0
		} else {
			parsed, err := strconv.ParseFloat(trimmed, 64)
			if err != nil {
				return 0, err
			}
			number = parsed
		}
	case bool:
		if typed {
			number = 1
		}
	case nil:
		number = 0
	default:
		return 0, errors.New("not numeric")
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
		math.Abs(number) > 9_007_199_254_740_991 {
		return 0, errors.New("not a safe integer")
	}
	return int64(number), nil
}

func seconds(milliseconds int64) string {
	return strconv.FormatFloat(float64(milliseconds)/1_000, 'f', 3, 64)
}

func aacProfile(quality string, bitrate int) AudioVariantProfile {
	target := bitrate
	return AudioVariantProfile{
		Quality: quality, Extension: "m4a", MIMEType: "audio/mp4", Codec: "aac", Container: "m4a",
		TargetBitrate: &target,
		FFmpegArgs:    []string{"-c:a", "aac", "-b:a", strconv.Itoa(bitrate/1_000) + "k", "-movflags", "+faststart"},
	}
}
