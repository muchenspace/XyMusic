package adminmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

func TestMetadataSnapshotFromProbeNormalizesTagsAndStreams(t *testing.T) {
	probe := ProbeOutput{
		Format: &struct {
			Duration string         `json:"duration"`
			Tags     map[string]any `json:"tags"`
		}{
			Duration: "12.345",
			Tags: map[string]any{
				"TITLE": "Song", "artist": "Artist; Artist", "album_artist": "Album Artist",
				"album": "Album", "track": "2/9", "disc": "1/2", "genre": "Rock; Pop",
				"bpm": "123.456", "isrc": "USABC1234567", "lyrics": "[00:01.00]Line",
				"language": "en-US",
			},
		},
		Streams: []ProbeStream{
			{Index: intPointer(0), CodecType: "audio", CodecName: "flac", SampleRate: "48000", Channels: intPointer(2)},
			{Index: intPointer(1), CodecType: "video", CodecName: "mjpeg", Width: intPointer(600), Height: intPointer(600), Disposition: map[string]int{"attached_pic": 1}},
		},
	}
	result, err := MetadataSnapshotFromProbe(probe, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Song" || len(result.Credits) != 1 || result.Album == nil || *result.Album != "Album" ||
		result.TrackNumber == nil || *result.TrackNumber != 2 || result.TrackTotal == nil || *result.TrackTotal != 9 ||
		result.BPM == nil || *result.BPM != 123.46 || result.Lyrics == nil || result.Lyrics.Format != "LRC" ||
		!result.HasArtwork {
		t.Fatalf("metadata=%+v", result)
	}
	file, err := metadataFileFromProbe(probe, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if file.DurationMS == nil || *file.DurationMS != 12_345 || len(file.Streams) != 2 ||
		!file.Streams[1].AttachedPicture {
		t.Fatalf("file=%+v", file)
	}
}

func TestMetadataSnapshotRecognizesContainerArtworkMarkers(t *testing.T) {
	probe := ProbeOutput{
		Format: &struct {
			Duration string         `json:"duration"`
			Tags     map[string]any `json:"tags"`
		}{Tags: map[string]any{"title": "Song", "artist": "Artist"}},
		Streams: []ProbeStream{
			{CodecType: "audio", CodecName: "aac"},
			{CodecType: "video", CodecName: "mjpeg", Tags: map[string]any{"handler_name": "Cover Art"}},
		},
	}
	metadata, err := MetadataSnapshotFromProbe(probe, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.HasArtwork {
		t.Fatalf("expected artwork marker to be detected: %+v", metadata)
	}
}

func TestProbeAndRemuxUseBoundedMediaToolCommands(t *testing.T) {
	probeJSON, err := json.Marshal(ProbeOutput{
		Format: &struct {
			Duration string         `json:"duration"`
			Tags     map[string]any `json:"tags"`
		}{Tags: map[string]any{"title": "Song", "artist": "Artist"}},
		Streams: []ProbeStream{{CodecType: "audio", CodecName: "flac"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &metadataRunnerStub{probeOutput: string(probeJSON)}
	file, err := ProbeMetadataFile(context.Background(), "song.flac", "ffprobe", runner)
	if err != nil {
		t.Fatal(err)
	}
	if file.Metadata.Title != "Song" || runner.calls[0].executable != "ffprobe" ||
		runner.calls[0].timeout != metadataProbeTimeout {
		t.Fatalf("probe=%+v calls=%+v", file, runner.calls)
	}
	if err := RemuxMetadataToFile(
		context.Background(), "song.flac", filepath.Join(t.TempDir(), "output.tmp"), "", "ffmpeg", file.Metadata, runner,
	); err != nil {
		t.Fatal(err)
	}
	call := runner.calls[1]
	if call.executable != "ffmpeg" || call.timeout != metadataRemuxTimeout ||
		!containsSequence(call.arguments, []string{"-c", "copy"}) ||
		!containsSequence(call.arguments, []string{"-f", "ffmetadata", "-i"}) ||
		!containsSequence(call.arguments, []string{"-map_metadata", "1:g"}) ||
		!containsSequence(call.arguments, []string{"-f", "flac"}) ||
		!strings.Contains(runner.metadataInput, "title=Song\n") {
		t.Fatalf("remux call=%+v", call)
	}
}

func TestRemuxUsesContainerSpecificSafeMetadataMapping(t *testing.T) {
	metadata, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	date := "2024-05"
	metadata.ReleaseDate = &date

	tests := []struct {
		name       string
		sourcePath string
		sequences  [][]string
		forbidden  [][]string
	}{
		{
			name: "Vorbis comments are replaced on the audio stream", sourcePath: "song.ogg",
			sequences: [][]string{{"-map_metadata:s:a:0", "1:g"}, {"-f", "ogg"}},
		},
		{
			name: "OpusTags are replaced on the audio stream", sourcePath: "song.opus",
			sequences: [][]string{{"-map_metadata:s:a:0", "1:g"}, {"-f", "opus"}},
		},
		{
			name: "M4A uses artwork-compatible atoms", sourcePath: "song.m4a",
			sequences: [][]string{{"-map", "0"}, {"-f", "mp4"}},
			forbidden: [][]string{{"-movflags", "use_metadata_tags"}},
		},
		{
			name: "MP3 keeps month precision with ID3v2.4", sourcePath: "song.mp3",
			sequences: [][]string{{"-id3v2_version", "4"}, {"-write_id3v1", "0"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &metadataRunnerStub{}
			if err := RemuxMetadataToFile(
				context.Background(), test.sourcePath, filepath.Join(t.TempDir(), "output.tmp"), "", "ffmpeg", metadata, runner,
			); err != nil {
				t.Fatal(err)
			}
			arguments := runner.calls[0].arguments
			for _, sequence := range test.sequences {
				if !containsSequence(arguments, sequence) {
					t.Fatalf("arguments=%q, missing %q", arguments, sequence)
				}
			}
			for _, sequence := range test.forbidden {
				if containsSequence(arguments, sequence) {
					t.Fatalf("arguments=%q, contains forbidden %q", arguments, sequence)
				}
			}
			if !strings.Contains(runner.metadataInput, "date=2024-05\n") {
				t.Fatalf("metadata input=%q", runner.metadataInput)
			}
		})
	}
}

func TestRemuxArtworkInputIndexAccountsForFFMetadataInput(t *testing.T) {
	metadata, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	runner := &metadataRunnerStub{}
	if err := RemuxMetadataToFile(
		context.Background(), "song.m4a", filepath.Join(t.TempDir(), "output.tmp"), "cover.jpg", "ffmpeg", metadata, runner,
	); err != nil {
		t.Fatal(err)
	}
	arguments := runner.calls[0].arguments
	if !containsSequence(arguments, []string{"-i", "cover.jpg", "-map", "0:a", "-map", "2:v:0"}) ||
		!containsSequence(arguments, []string{"-disposition:v:0", "attached_pic"}) {
		t.Fatalf("arguments=%q", arguments)
	}
}

func TestRemuxRejectsInconsistentLyricsBeforeRunningFFmpeg(t *testing.T) {
	metadata, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	metadata.Lyrics = &MetadataLyrics{
		Content: "[00:01.00]<00:01.00>word\n[00:02.00]ordinary line",
		Format:  "LRC", Language: "und", Timing: sharedlyrics.TimingWord,
	}
	runner := &metadataRunnerStub{}
	err = RemuxMetadataToFile(
		context.Background(), "song.flac", filepath.Join(t.TempDir(), "output.tmp"), "", "ffmpeg", metadata, runner,
	)
	if err == nil || len(runner.calls) != 0 {
		t.Fatalf("RemuxMetadataToFile() error/calls = %v/%d", err, len(runner.calls))
	}
}

func TestRemuxWritesLongAndMultilineMetadataThroughAFile(t *testing.T) {
	metadata, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	lyrics := strings.Repeat("line = value; # comment \\\r\n", 2_000)
	metadata.Lyrics = &MetadataLyrics{Content: lyrics, Format: "PLAIN", Language: "zh-cn", Timing: sharedlyrics.TimingLine}
	runner := &metadataRunnerStub{}
	if err := RemuxMetadataToFile(
		context.Background(), "song.flac", filepath.Join(t.TempDir(), "output.tmp"), "", "ffmpeg", metadata, runner,
	); err != nil {
		t.Fatal(err)
	}
	for _, argument := range runner.calls[0].arguments {
		if strings.Contains(argument, "line = value") || len(argument) > 2_000 {
			t.Fatalf("large metadata leaked into command argument of length %d", len(argument))
		}
	}
	if !strings.Contains(runner.metadataInput, "lyrics_format=PLAIN\n") ||
		!strings.Contains(runner.metadataInput, "unsyncedlyrics=line \\= value\\; \\# comment \\\\\\\n") ||
		strings.Contains(runner.metadataInput, "\r") {
		t.Fatalf("metadata input was not escaped or normalized")
	}
}

func TestMetadataSnapshotFromProbePrefersExplicitLyricsFormat(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]any
		want string
	}{
		{name: "explicit plain", tags: map[string]any{"lyrics": "[00:01.00] literal", "lyrics_format": "PLAIN"}, want: "PLAIN"},
		{name: "explicit lrc", tags: map[string]any{"lyrics": "no timestamp", "lyrics-format": "lrc"}, want: "LRC"},
		{name: "synced key", tags: map[string]any{"syncedlyrics": "no timestamp"}, want: "LRC"},
		{name: "unsynced key", tags: map[string]any{"unsyncedlyrics": "[00:01.00] literal"}, want: "PLAIN"},
		{name: "colon fraction timestamp", tags: map[string]any{"lyrics": "[00:01:25]line"}, want: "LRC"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.tags["title"] = "Song"
			test.tags["artist"] = "Artist"
			metadata, err := MetadataSnapshotFromProbe(ProbeOutput{Format: &struct {
				Duration string         `json:"duration"`
				Tags     map[string]any `json:"tags"`
			}{Tags: test.tags}}, "fallback")
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Lyrics == nil || metadata.Lyrics.Format != test.want {
				t.Fatalf("lyrics=%+v, want format %s", metadata.Lyrics, test.want)
			}
		})
	}
}

func TestMetadataSnapshotFromProbeResolvesLyricsTiming(t *testing.T) {
	tests := []struct {
		name      string
		tags      map[string]any
		want      string
		wantError bool
	}{
		{
			name: "explicit word timing",
			tags: map[string]any{
				"lyrics": "[00:01.00]<00:01.00>word", "lyrics_format": "LRC", "lyrics_timing": "WORD",
			},
			want: "WORD",
		},
		{
			name: "enhanced LRC without timing tag",
			tags: map[string]any{"lyrics": "[00:01.00]<00:01.00>word", "lyrics_format": "LRC"},
			want: "WORD",
		},
		{
			name:      "word tag without word markers",
			tags:      map[string]any{"lyrics": "[00:01.00]ordinary line", "lyrics_format": "LRC", "lyrics_timing": "WORD"},
			wantError: true,
		},
		{
			name: "plain lyrics reject word timing",
			tags: map[string]any{
				"lyrics": "[00:01.00]<00:01.00>literal", "lyrics_format": "PLAIN", "lyrics_timing": "WORD",
			},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.tags["title"] = "Song"
			test.tags["artist"] = "Artist"
			metadata, err := MetadataSnapshotFromProbe(ProbeOutput{Format: &struct {
				Duration string         `json:"duration"`
				Tags     map[string]any `json:"tags"`
			}{Tags: test.tags}}, "fallback")
			if test.wantError {
				if err == nil {
					t.Fatal("MetadataSnapshotFromProbe() succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Lyrics == nil || string(metadata.Lyrics.Timing) != test.want {
				t.Fatalf("lyrics=%+v, want timing %s", metadata.Lyrics, test.want)
			}
		})
	}
}

func TestFFMetadataValuesWriteLyricsTiming(t *testing.T) {
	tests := []struct {
		name   string
		format string
		timing string
		want   string
	}{
		{name: "word LRC", format: "LRC", timing: "WORD", want: "WORD"},
		{name: "plain line", format: "PLAIN", timing: "LINE", want: "LINE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata := MetadataSnapshot{Lyrics: &MetadataLyrics{
				Content: "[00:01.00]<00:01.00>word", Format: test.format, Language: "und", Timing: sharedlyrics.Timing(test.timing),
			}}
			for _, pair := range ffmetadataValues(metadata) {
				if pair[0] == "lyrics_timing" && pair[1] == test.want {
					return
				}
			}
			t.Fatalf("lyrics_timing tag missing or wrong, want %s", test.want)
		})
	}
}

func TestProbeRejectsTruncatedToolOutput(t *testing.T) {
	runner := &metadataRunnerStub{probeOutput: `{}`, probeTruncated: true}
	_, err := ProbeMetadataFile(context.Background(), "song.flac", "ffprobe", runner)
	if writebackErrorCode(err) != "FFPROBE_OUTPUT_TOO_LARGE" {
		t.Fatalf("error = %v, want FFPROBE_OUTPUT_TOO_LARGE", err)
	}
}

func TestVerifyMetadataRemuxRejectsChangedStreamsAndArtwork(t *testing.T) {
	metadata, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	before := ProbedMetadataFile{
		Metadata: metadata, Streams: []StreamFingerprint{{Index: 0, CodecType: "audio", CodecName: "flac"}},
	}
	after := before
	after.Streams = []StreamFingerprint{{Index: 0, CodecType: "audio", CodecName: "mp3"}}
	if err := VerifyMetadataRemux(before, after, metadata); writebackErrorCode(err) != "STREAM_VERIFICATION_FAILED" {
		t.Fatalf("stream error=%v", err)
	}
	after = before
	after.Metadata.HasArtwork = true
	if err := VerifyMetadataRemux(before, after, metadata); writebackErrorCode(err) != "ARTWORK_VERIFICATION_FAILED" {
		t.Fatalf("artwork error=%v", err)
	}
}

type metadataCommandCall struct {
	executable string
	arguments  []string
	timeout    time.Duration
}

type metadataRunnerStub struct {
	probeOutput    string
	probeTruncated bool
	metadataInput  string
	calls          []metadataCommandCall
}

func (runner *metadataRunnerStub) Run(
	_ context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
) (ProcessResult, error) {
	runner.calls = append(runner.calls, metadataCommandCall{
		executable: executable, arguments: append([]string(nil), arguments...), timeout: timeout,
	})
	if executable == "ffprobe" {
		return ProcessResult{Stdout: runner.probeOutput, StdoutTruncated: runner.probeTruncated}, nil
	}
	if executable == "ffmpeg" {
		for index := 0; index+3 < len(arguments); index++ {
			if arguments[index] == "-f" && arguments[index+1] == "ffmetadata" && arguments[index+2] == "-i" {
				contents, err := os.ReadFile(arguments[index+3])
				if err != nil {
					return ProcessResult{}, err
				}
				runner.metadataInput = string(contents)
				break
			}
		}
		return ProcessResult{}, nil
	}
	return ProcessResult{}, errors.New("unexpected executable")
}

func containsSequence(values, sequence []string) bool {
	for index := 0; index+len(sequence) <= len(values); index++ {
		if reflect.DeepEqual(values[index:index+len(sequence)], sequence) {
			return true
		}
	}
	return false
}

func intPointer(value int) *int { return &value }
