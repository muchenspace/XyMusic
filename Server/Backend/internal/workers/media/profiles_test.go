package media

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/platform/processio"
)

func TestAudioVariantProfilesMatchLegacyContract(t *testing.T) {
	lossy := AudioVariantProfiles("aac")
	if len(lossy) != 3 {
		t.Fatalf("lossy profiles = %#v", lossy)
	}
	if lossy[0].Quality != "DATA_SAVER" || lossy[0].Extension != "m4a" ||
		strings.Join(lossy[0].FFmpegArgs, " ") != "-c:a aac -b:a 64k -movflags +faststart" {
		t.Fatalf("data saver profile = %#v", lossy[0])
	}
	lossless := AudioVariantProfiles(" PCM_S24LE ")
	if len(lossless) != 4 || lossless[3].Quality != "LOSSLESS" ||
		strings.Join(lossless[3].FFmpegArgs, " ") != "-c:a flac -compression_level 8" {
		t.Fatalf("lossless profiles = %#v", lossless)
	}
	if !IsLosslessCodec("wavpack") || IsLosslessCodec("aac") {
		t.Fatal("lossless codec classification differs from the legacy worker")
	}
}

func TestMediaSegmentMatchesCueBounds(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		duration int64
		want     mediaRange
		code     string
	}{
		{name: "whole source", payload: `{}`, duration: 3_000, want: mediaRange{EndMS: 3_000}},
		{name: "cue segment", payload: `{"segmentStartMs":250,"segmentEndMs":1250}`, duration: 3_000, want: mediaRange{StartMS: 250, EndMS: 1_250}},
		{name: "end tolerance", payload: `{"segmentStartMs":2000,"segmentEndMs":3500}`, duration: 3_000, want: mediaRange{StartMS: 2_000, EndMS: 3_000}},
		{name: "numeric strings", payload: `{"segmentStartMs":"1","segmentEndMs":"2"}`, duration: 3_000, want: mediaRange{StartMS: 1, EndMS: 2}},
		{name: "past tolerance", payload: `{"segmentStartMs":2000,"segmentEndMs":4001}`, duration: 3_000, code: "INVALID_SEGMENT"},
		{name: "fraction", payload: `{"segmentStartMs":1.5}`, duration: 3_000, code: "INVALID_SEGMENT"},
		{name: "empty", payload: `{"segmentStartMs":3000}`, duration: 3_000, code: "INVALID_SEGMENT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := mediaSegment(json.RawMessage(test.payload), test.duration)
			if test.code != "" {
				if workerErrorCode(err) != test.code {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("segment = %#v error=%v", got, err)
			}
		})
	}
}

func TestRetryDelayAndErrorRedactionMatchLegacyLimits(t *testing.T) {
	if retryDelay(1) != 10*time.Second || retryDelay(10) != time.Hour || retryDelay(20) != time.Hour {
		t.Fatal("retry backoff does not match the legacy exponential schedule")
	}
	secret := strings.Repeat("a", 64)
	message := safeWorkerError(newWorkerError("FAILED", "see https://example.test/path token "+secret))
	if strings.Contains(message, "https://") || strings.Contains(message, secret) ||
		!strings.Contains(message, "[REDACTED_URL]") || !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("safe error = %q", message)
	}
}

func TestProcessOutputBuffersKeepExpectedSides(t *testing.T) {
	head := processio.NewHeadBuffer(4)
	_, _ = head.Write([]byte("abcdef"))
	if head.String() != "abcd" || !head.Truncated() {
		t.Fatalf("head = %q truncated=%v", head.String(), head.Truncated())
	}
	tail := processio.NewTailBuffer(4)
	_, _ = tail.Write([]byte("ab"))
	_, _ = tail.Write([]byte("cdef"))
	if tail.String() != "cdef" {
		t.Fatalf("tail = %q", tail.String())
	}
	environment := mediaProcessEnvironment([]string{
		"Path=C:\\tools", "DATABASE_URL=secret", "TEMP=C:\\temp", "HOME=C:\\home",
	})
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "DATABASE_URL") || !strings.Contains(joined, "PATH=C:\\tools") ||
		!strings.Contains(joined, "TEMP=C:\\temp") {
		t.Fatalf("media process environment = %#v", environment)
	}
}

func TestProbeParserRejectsTrailingOutput(t *testing.T) {
	_, err := parseProbe(`{"streams":[],"format":{"duration":"1"}} trailing`)
	if workerErrorCode(err) != "FFPROBE_INVALID_OUTPUT" {
		t.Fatalf("error = %v", err)
	}
}
