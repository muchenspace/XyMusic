package mediapayload

import (
	"encoding/json"
	"strings"
)

const SourceProbeKey = "sourceProbe"

// SourceProbe is the immutable media information discovered while scanning a
// local source. It is embedded in media job payloads so the media worker can
// avoid probing the same source a second time.
type SourceProbe struct {
	DurationMS int64  `json:"durationMs"`
	Codec      string `json:"codec"`
	SampleRate *int   `json:"sampleRate,omitempty"`
}

// DecodeSourceProbe accepts payloads from both the current scanner and older
// jobs. Invalid or incomplete hints deliberately fall back to ffprobe.
func DecodeSourceProbe(payload json.RawMessage) (SourceProbe, bool) {
	if len(payload) == 0 {
		return SourceProbe{}, false
	}
	var envelope struct {
		SourceProbe *SourceProbe `json:"sourceProbe"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.SourceProbe == nil {
		return SourceProbe{}, false
	}
	hint := *envelope.SourceProbe
	hint.Codec = strings.TrimSpace(hint.Codec)
	if hint.DurationMS <= 0 || hint.Codec == "" {
		return SourceProbe{}, false
	}
	if hint.SampleRate != nil && *hint.SampleRate <= 0 {
		hint.SampleRate = nil
	}
	return hint, true
}
