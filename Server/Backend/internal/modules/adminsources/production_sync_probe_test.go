package adminsources

import (
	"encoding/json"
	"testing"

	"xymusic/server/internal/modules/adminmetadata"
)

func TestAddSourceProbeHintEncodesMediaProbeForJobs(t *testing.T) {
	durationMS := int64(1250)
	sampleRate := 44100
	payload := map[string]any{"segmentStartMs": int64(10)}
	probed := &adminmetadata.ProbedMetadataFile{
		DurationMS: &durationMS,
		Streams: []adminmetadata.StreamFingerprint{
			{CodecType: "video", CodecName: "mjpeg"},
			{CodecType: "audio", CodecName: " flac ", SampleRate: &sampleRate},
		},
	}

	addSourceProbeHint(payload, probed)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SourceProbe struct {
			DurationMS int64  `json:"durationMs"`
			Codec      string `json:"codec"`
			SampleRate *int   `json:"sampleRate"`
		} `json:"sourceProbe"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SourceProbe.DurationMS != durationMS || decoded.SourceProbe.Codec != "flac" {
		t.Fatalf("source probe=%+v", decoded.SourceProbe)
	}
	if decoded.SourceProbe.SampleRate == nil || *decoded.SourceProbe.SampleRate != sampleRate {
		t.Fatalf("sample rate=%v", decoded.SourceProbe.SampleRate)
	}
}

func TestAddSourceProbeHintSkipsIncompleteProbe(t *testing.T) {
	durationMS := int64(1250)
	invalid := []struct {
		name   string
		probed *adminmetadata.ProbedMetadataFile
	}{
		{name: "nil probe"},
		{name: "missing duration", probed: &adminmetadata.ProbedMetadataFile{}},
		{name: "no audio stream", probed: &adminmetadata.ProbedMetadataFile{
			DurationMS: &durationMS,
			Streams:    []adminmetadata.StreamFingerprint{{CodecType: "video", CodecName: "mjpeg"}},
		}},
		{name: "missing codec", probed: &adminmetadata.ProbedMetadataFile{
			DurationMS: &durationMS,
			Streams:    []adminmetadata.StreamFingerprint{{CodecType: "audio"}},
		}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			payload := map[string]any{"segmentStartMs": int64(10)}
			addSourceProbeHint(payload, test.probed)
			if _, exists := payload["sourceProbe"]; exists {
				t.Fatalf("unexpected source probe in payload=%v", payload)
			}
		})
	}
}
