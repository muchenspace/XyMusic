package adminsources

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/modules/adminmetadata"
	"xymusic/server/internal/platform/localmedia"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

func TestRecordScanMetadataRejectsInvalidLyricTimingBeforeDatabase(t *testing.T) {
	tests := []struct {
		name    string
		timing  sharedlyrics.Timing
		content string
	}{
		{name: "missing timing", content: "[00:01.00]line"},
		{name: "unknown timing", timing: "SYLLABLE", content: "[00:01.00]line"},
		{name: "content mismatch", timing: sharedlyrics.TimingLine, content: "[00:01.00]<00:01.00>word"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("recordScanMetadata panicked before validating lyrics: %v", recovered)
				}
			}()
			err := recordScanMetadata(context.Background(), nil, "track", "source", adminmetadata.MetadataSnapshot{
				Lyrics: &adminmetadata.MetadataLyrics{
					Format: "LRC", Timing: test.timing, Content: test.content, Language: "und",
				},
			}, "checksum", time.Now())
			if err == nil || !strings.Contains(err.Error(), "validate scanned local library metadata lyrics") {
				t.Fatalf("recordScanMetadata() error = %v", err)
			}
		})
	}
}

func TestSyncScannedLyricsRejectsInconsistentTimingBeforeDatabase(t *testing.T) {
	transaction := &scannedLyricTransactionStub{}
	err := syncScannedLyrics(context.Background(), transaction, "track", []scannedLyric{{
		Format: "LRC", Timing: sharedlyrics.TimingWord,
		Content: "[00:01.00]<00:01.00>word\n[00:02.00]ordinary line", Language: "und", Origin: "EXTERNAL",
	}})
	if err == nil || transaction.calls != 0 {
		t.Fatalf("syncScannedLyrics() error/calls = %v/%d", err, transaction.calls)
	}
}

type scannedLyricTransactionStub struct {
	pgx.Tx
	calls int
}

func (transaction *scannedLyricTransactionStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	transaction.calls++
	return pgconn.CommandTag{}, errors.New("unexpected database call")
}

func TestProcessPreparedFileSkipsCommitForReusableUnchangedSource(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "song.flac"
	if err := os.WriteFile(path, []byte("stable audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	synchronizer := &ProductionSynchronizer{}
	err = synchronizer.ProcessPreparedFile(context.Background(), "root", "run", DiscoveredFile{
		AudioPath: path, RelativePath: "song.flac", FileInfo: metadata,
	}, time.Now().UTC(), &preparedStandardFile{
		Metadata: metadata, ExistingFound: true, UnchangedReady: true,
	})
	if err != nil {
		t.Fatalf("reusable unchanged source commit = %v", err)
	}
}

func TestFFprobeMetadataProbeReadsRealAudioWithoutFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not available")
	}
	audioPath := filepath.Join(t.TempDir(), "probe.wav")
	command := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.25", "-c:a", "pcm_s16le", audioPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create real audio fixture: %v: %s", err, output)
	}
	probe, err := NewFFprobeMetadataProbe(ffprobe, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := probe.Probe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("probe real audio: %v", err)
	}
	if result.DurationMS == nil || *result.DurationMS < 200 || len(result.Streams) == 0 || result.Streams[0].CodecType != "audio" {
		t.Fatalf("real audio probe result=%+v", result)
	}
}

func TestStageArtworkExtractsRealEmbeddedCoverWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not available")
	}
	tempDir := t.TempDir()
	wavPath := filepath.Join(tempDir, "source.wav")
	audioFixture := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=0.2",
		"-c:a", "pcm_s16le", wavPath)
	if output, err := audioFixture.CombinedOutput(); err != nil {
		t.Fatalf("create audio fixture: %v: %s", err, output)
	}
	coverPath := filepath.Join(tempDir, "cover.jpg")
	cover := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			cover.Set(x, y, color.RGBA{R: 220, G: uint8(x * 4), B: uint8(y * 4), A: 255})
		}
	}
	file, err := os.Create(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(file, cover, &jpeg.Options{Quality: 90}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(tempDir, "embedded.mp3")
	command := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-y",
		"-i", wavPath, "-i", coverPath,
		"-map", "0:a:0", "-map", "1:v:0",
		"-c:a", "libmp3lame", "-b:a", "128k",
		"-c:v", "mjpeg", "-id3v2_version", "3",
		"-metadata:s:v", "title=Album cover",
		"-metadata:s:v", "comment=Cover (front)",
		"-disposition:v:0", "attached_pic", sourcePath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create embedded audio fixture: %v: %s", err, output)
	}
	probe, err := NewFFprobeMetadataProbe(ffprobe, nil)
	if err != nil {
		t.Fatal(err)
	}
	probed, err := probe.Probe(context.Background(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !probed.Metadata.HasArtwork {
		t.Fatalf("embedded cover was not detected: %+v", probed)
	}
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	synchronizer := &ProductionSynchronizer{
		localMedia: mediaStore, ffmpegPath: ffmpeg, artworkRunner: adminmetadata.OSProcessRunner{},
		artworkGate: make(chan struct{}, 1),
	}
	checksum, err := fileSHA256(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	artwork, err := synchronizer.stageArtwork(context.Background(), sourcePath, true, checksum)
	if err != nil {
		t.Fatalf("extract real embedded cover: %v", err)
	}
	if artwork == nil {
		t.Fatal("expected extracted artwork")
	}
	coverFile, err := mediaStore.OpenAsset(artwork.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(coverFile)
	_ = coverFile.Close()
	if err != nil {
		t.Fatalf("decode stored artwork: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("stored artwork dimensions = %v, want 32x32", decoded.Bounds())
	}
}

func TestStageArtworkStoresEmbeddedCoverAsLocalAsset(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	runner := &artworkRunnerStub{content: []byte("normalized jpeg payload")}
	synchronizer := &ProductionSynchronizer{
		localMedia: mediaStore, ffmpegPath: "ffmpeg", artworkRunner: runner,
		artworkGate: make(chan struct{}, 1),
	}
	snapshot := &sourceScanSnapshot{artworkByChecksum: make(map[string]*stagedArtwork)}
	scanContext := context.WithValue(context.Background(), sourceScanSnapshotContextKey{}, snapshot)
	artwork, err := synchronizer.stageArtwork(scanContext, filepath.Join(tempDir, "song.flac"), true, "source-checksum")
	if err != nil {
		t.Fatalf("stageArtwork() error = %v", err)
	}
	if artwork == nil || artwork.StoragePath == "" || artwork.Checksum == "" || artwork.SizeBytes <= 0 {
		t.Fatalf("unexpected staged artwork: %+v", artwork)
	}
	if !strings.HasPrefix(artwork.StoragePath, "artworks/") || !strings.HasSuffix(artwork.StoragePath, ".jpg") {
		t.Fatalf("unexpected artwork storage path: %q", artwork.StoragePath)
	}
	info, err := mediaStore.StatAsset(artwork.StoragePath)
	if err != nil || info.Size() != artwork.SizeBytes {
		t.Fatalf("stored artwork stat = %v/%v, want size %d", info, err, artwork.SizeBytes)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "ffmpeg" {
		t.Fatalf("artwork runner calls = %+v", runner.calls)
	}

	// A scan-scoped source checksum cache prevents repeated extraction when the
	// same audio source is encountered through multiple catalog mappings.
	second, err := synchronizer.stageArtwork(scanContext, filepath.Join(tempDir, "other.flac"), true, "source-checksum")
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.StoragePath != artwork.StoragePath || len(runner.calls) != 1 {
		t.Fatalf("second staged artwork = %+v, runner calls = %d", second, len(runner.calls))
	}
}

type artworkRunnerStub struct {
	content []byte
	calls   []string
}

func (runner *artworkRunnerStub) Run(_ context.Context, executable string, arguments []string, _ time.Duration) (adminmetadata.ProcessResult, error) {
	runner.calls = append(runner.calls, executable)
	if len(arguments) == 0 {
		return adminmetadata.ProcessResult{}, errors.New("missing artwork output path")
	}
	if err := os.WriteFile(arguments[len(arguments)-1], runner.content, 0o600); err != nil {
		return adminmetadata.ProcessResult{}, err
	}
	return adminmetadata.ProcessResult{}, nil
}

func TestProductionSynchronizerDoesNotRequireFFmpegForSourceScan(t *testing.T) {
	probe := metadataProbeFailureStub{err: errors.New("probe fixture")}
	synchronizer, err := NewProductionSynchronizer(ProductionSynchronizerOptions{
		Database: &pgxpool.Pool{}, Probe: probe,
	})
	if err != nil {
		t.Fatalf("source synchronizer should not require FFmpeg: %v", err)
	}
	if synchronizer == nil || synchronizer.probe == nil {
		t.Fatal("source synchronizer was not initialized")
	}
}
