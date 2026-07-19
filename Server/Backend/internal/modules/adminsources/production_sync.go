package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/modules/adminmetadata"
)

type SourceObjectStorage interface {
	UploadFile(context.Context, string, string, string, string) (int64, error)
	StatObject(context.Context, string) (sizeBytes int64, checksumSHA256 string, exists bool, err error)
}

type SourceMetadataProbe interface {
	Probe(context.Context, string) (adminmetadata.ProbedMetadataFile, error)
}

type FFprobeMetadataProbe struct {
	executable string
	runner     adminmetadata.ProcessRunner
}

func NewFFprobeMetadataProbe(executable string, runner adminmetadata.ProcessRunner) (*FFprobeMetadataProbe, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("local library ffprobe path is required")
	}
	if runner == nil {
		runner = adminmetadata.OSProcessRunner{}
	}
	return &FFprobeMetadataProbe{executable: executable, runner: runner}, nil
}

func (probe *FFprobeMetadataProbe) Probe(ctx context.Context, path string) (adminmetadata.ProbedMetadataFile, error) {
	return adminmetadata.ProbeMetadataFile(ctx, path, probe.executable, probe.runner)
}

type ProductionSynchronizerOptions struct {
	Database   *pgxpool.Pool
	Storage    SourceObjectStorage
	Probe      SourceMetadataProbe
	FFmpegPath string
	Runner     adminmetadata.ProcessRunner
	Now        func() time.Time
}

type ProductionSynchronizer struct {
	database   syncDatabase
	storage    SourceObjectStorage
	probe      SourceMetadataProbe
	ffmpegPath string
	runner     adminmetadata.ProcessRunner
	now        func() time.Time
}

type syncDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}

var _ FileSynchronizer = (*ProductionSynchronizer)(nil)

func NewProductionSynchronizer(options ProductionSynchronizerOptions) (*ProductionSynchronizer, error) {
	if options.Database == nil {
		return nil, errors.New("local library synchronizer database is required")
	}
	if options.Storage == nil {
		return nil, errors.New("local library synchronizer object storage is required")
	}
	if options.Probe == nil {
		return nil, errors.New("local library synchronizer metadata probe is required")
	}
	if options.Runner == nil {
		options.Runner = adminmetadata.OSProcessRunner{}
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ProductionSynchronizer{
		database: options.Database, storage: options.Storage, probe: options.Probe,
		ffmpegPath: strings.TrimSpace(options.FFmpegPath), runner: options.Runner, now: options.Now,
	}, nil
}

func (synchronizer *ProductionSynchronizer) ProcessFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
) error {
	normalizedPath := normalizePlatformPath(file.RelativePath)
	if err := synchronizer.touchDiscoveredSource(ctx, rootID, normalizedPath, seenAt); err != nil {
		return err
	}
	var processErr error
	if file.ScanError != nil {
		processErr = file.ScanError
	} else if file.CuePath != "" {
		processErr = synchronizer.syncCueFile(ctx, rootID, scanRunID, file, seenAt)
	} else {
		_, processErr = synchronizer.syncStandardFile(ctx, rootID, scanRunID, file, seenAt, false)
	}
	if processErr == nil || errors.Is(processErr, context.Canceled) || errors.Is(processErr, ErrScanCancelled) {
		return processErr
	}
	_ = synchronizer.markSourceFailed(ctx, rootID, normalizedPath, processErr, seenAt)
	return processErr
}

func (synchronizer *ProductionSynchronizer) touchDiscoveredSource(
	ctx context.Context,
	rootID, normalizedPath string,
	seenAt time.Time,
) error {
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin discovered local library source touch: %w", err)
	}
	defer transaction.Rollback(ctx)
	var sourceID string
	var status SourceFileStatus
	var sourceUpdatedAt time.Time
	err = transaction.QueryRow(ctx, `SELECT id,status,updated_at
		FROM local_music_sources
		WHERE root_id=$1 AND normalized_source_path=$2
		FOR UPDATE`, rootID, normalizedPath).Scan(&sourceID, &status, &sourceUpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock discovered local library source: %w", err)
	}
	now := synchronizer.now()
	if status == SourceFileMissing {
		if _, err := transaction.Exec(ctx, `UPDATE tracks track SET
			status=CASE WHEN track.published_at IS NOT NULL AND track.duration_ms>0 AND EXISTS(
				SELECT 1 FROM track_variants variant
				JOIN media_assets asset ON asset.id=variant.asset_id
				WHERE variant.track_id=track.id AND variant.status='READY' AND asset.status='READY'
			) THEN 'READY'::catalog_status ELSE 'ERROR'::catalog_status END,
			version=track.version+1,updated_at=$3
			WHERE track.status='ARCHIVED' AND track.updated_at=$2
			AND EXISTS(
				SELECT 1 FROM local_music_source_tracks mapping
				WHERE mapping.source_id=$1 AND mapping.track_id=track.id
			)
			AND NOT EXISTS(
				SELECT 1 FROM audit_logs audit
				WHERE audit.action='admin.track.archive' AND audit.target_type='track'
				AND audit.target_id=track.id AND audit.result='SUCCESS'
			)`, sourceID, sourceUpdatedAt, now); err != nil {
			return fmt.Errorf("restore incorrectly archived local library tracks: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		last_seen_at=$2,updated_at=$3 WHERE id=$1`, sourceID, seenAt, now); err != nil {
		return fmt.Errorf("touch discovered local library source: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit discovered local library source touch: %w", err)
	}
	return nil
}

func (synchronizer *ProductionSynchronizer) ArchiveMissing(
	ctx context.Context,
	rootID string,
	scanStartedAt, now time.Time,
) (int, error) {
	var archived int
	err := synchronizer.database.QueryRow(ctx, `WITH stale_sources AS MATERIALIZED(
		SELECT id FROM local_music_sources WHERE root_id=$1 AND last_seen_at<$2
	), missing_sources AS(
		UPDATE local_music_sources SET status='MISSING',updated_at=$3
		WHERE id IN(SELECT id FROM stale_sources) AND status<>'MISSING' RETURNING id
	), archived_tracks AS(
		UPDATE tracks track SET status='ARCHIVED',version=track.version+1,updated_at=$3
		WHERE track.status<>'ARCHIVED' AND EXISTS(
			SELECT 1 FROM local_music_source_tracks mapping
			JOIN stale_sources missing ON missing.id=mapping.source_id WHERE mapping.track_id=track.id
		) AND NOT EXISTS(
			SELECT 1 FROM local_music_source_tracks active_mapping
			JOIN local_music_sources active_source ON active_source.id=active_mapping.source_id
			WHERE active_mapping.track_id=track.id AND active_source.status<>'MISSING'
			AND NOT EXISTS(SELECT 1 FROM stale_sources stale WHERE stale.id=active_source.id)
		) RETURNING track.id
	) SELECT count(*)::int FROM missing_sources`, rootID, scanStartedAt, now).Scan(&archived)
	if err != nil {
		return 0, fmt.Errorf("archive missing local library files: %w", err)
	}
	return archived, nil
}

type localSourceRecord struct {
	ID             string
	RootID         string
	SourcePath     string
	NormalizedPath string
	Checksum       string
	SizeBytes      int64
	ModifiedAt     time.Time
	TrackID        string
	SourceAssetID  *string
	MediaJobID     *string
	Status         SourceFileStatus
	LastSeenAt     time.Time
}

func (synchronizer *ProductionSynchronizer) readySourceAssetReusable(
	ctx context.Context,
	source localSourceRecord,
) (bool, error) {
	if source.SourceAssetID == nil {
		return false, nil
	}
	var objectKey string
	var sizeBytes int64
	var checksum *string
	err := synchronizer.database.QueryRow(ctx, `SELECT object_key,size_bytes,checksum_sha256
		FROM media_assets WHERE id=$1 AND kind='AUDIO_SOURCE' AND status='READY'`, *source.SourceAssetID).Scan(
		&objectKey, &sizeBytes, &checksum,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find reusable local library source asset: %w", err)
	}
	if sizeBytes != source.SizeBytes || (checksum != nil && !strings.EqualFold(*checksum, source.Checksum)) {
		return false, nil
	}
	storedSize, storedChecksum, exists, err := synchronizer.storage.StatObject(ctx, objectKey)
	if err != nil {
		return false, fmt.Errorf("inspect reusable local library source asset: %w", err)
	}
	if !exists || storedSize != sizeBytes {
		return false, nil
	}
	if checksum != nil && storedChecksum != "" && !strings.EqualFold(storedChecksum, *checksum) {
		return false, nil
	}
	return true, nil
}

type stagedArtwork struct {
	ObjectKey string
	Path      string
	SizeBytes int64
	Checksum  string
}

type scannedLyric struct {
	Content   string
	Format    string
	Language  string
	Origin    string
	IsDefault bool
}

func (synchronizer *ProductionSynchronizer) syncStandardFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
	preserveCueMappings bool,
) (localSourceRecord, error) {
	metadata, err := os.Stat(file.AudioPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	normalizedPath := normalizePlatformPath(file.RelativePath)
	existing, found, err := synchronizer.findSource(ctx, rootID, normalizedPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	unchanged := found && existing.SizeBytes == metadata.Size() &&
		existing.ModifiedAt.UnixMilli() == metadata.ModTime().UnixMilli()
	assetChecked := false
	assetReusable := false
	checkReadyAsset := func() (bool, error) {
		if assetChecked {
			return assetReusable, nil
		}
		assetChecked = true
		var checkErr error
		assetReusable, checkErr = synchronizer.readySourceAssetReusable(ctx, existing)
		return assetReusable, checkErr
	}
	if unchanged && existing.Status == SourceFileReady {
		reusable, err := checkReadyAsset()
		if err != nil {
			return localSourceRecord{}, err
		}
		if reusable {
			externalLyrics, err := synchronizer.sourceHasExternalLyrics(ctx, existing.ID)
			if err != nil {
				return localSourceRecord{}, err
			}
			sidecars, err := readSidecarLyrics(file.AudioPath)
			if err != nil {
				return localSourceRecord{}, err
			}
			if len(sidecars) > 0 || externalLyrics {
				if err := synchronizer.syncUnchangedSidecars(ctx, existing, sidecars, seenAt); err != nil {
					return localSourceRecord{}, err
				}
			}
			if _, err := synchronizer.database.Exec(ctx,
				`UPDATE local_music_sources SET last_seen_at=$2,updated_at=$3 WHERE id=$1`,
				existing.ID, seenAt, synchronizer.now()); err != nil {
				return localSourceRecord{}, fmt.Errorf("touch unchanged local library file: %w", err)
			}
			existing.LastSeenAt = seenAt
			return existing, nil
		}
	}
	if unchanged && existing.Status == SourceFileProcessing && existing.MediaJobID != nil {
		var status string
		err := synchronizer.database.QueryRow(ctx, `SELECT status::text FROM media_jobs WHERE id=$1`, *existing.MediaJobID).Scan(&status)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return localSourceRecord{}, fmt.Errorf("find local library media job: %w", err)
		}
		if status == "PENDING" || status == "PROCESSING" {
			_, err := synchronizer.database.Exec(ctx,
				`UPDATE local_music_sources SET last_seen_at=$2,updated_at=$3 WHERE id=$1`, existing.ID, seenAt, synchronizer.now())
			return existing, err
		}
		if status == "READY" {
			reusable, checkErr := checkReadyAsset()
			if checkErr != nil {
				return localSourceRecord{}, checkErr
			}
			if reusable {
				_, err := synchronizer.database.Exec(ctx, `UPDATE local_music_sources SET
					status='READY',last_error=NULL,last_seen_at=$2,updated_at=$3 WHERE id=$1`, existing.ID, seenAt, synchronizer.now())
				existing.Status = SourceFileReady
				return existing, err
			}
		}
	}
	checksum, err := fileSHA256(file.AudioPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	if !found {
		candidates, err := synchronizer.findRenameCandidates(ctx, rootID, checksum, seenAt)
		if err != nil {
			return localSourceRecord{}, err
		}
		if len(candidates) == 1 {
			existing, found = candidates[0], true
		}
	}
	if found && existing.Checksum == checksum && existing.Status == SourceFileReady {
		reusable, err := checkReadyAsset()
		if err != nil {
			return localSourceRecord{}, err
		}
		if reusable {
			transaction, err := synchronizer.database.Begin(ctx)
			if err != nil {
				return localSourceRecord{}, fmt.Errorf("begin unchanged local library rename: %w", err)
			}
			defer transaction.Rollback(ctx)
			locked, err := scanLocalSource(transaction.QueryRow(ctx, `SELECT `+localSourceColumns+`
				FROM local_music_sources WHERE id=$1 FOR UPDATE`, existing.ID))
			if err != nil {
				return localSourceRecord{}, fmt.Errorf("lock unchanged local library rename: %w", err)
			}
			if locked.Checksum != checksum || locked.Status != SourceFileReady {
				return localSourceRecord{}, fmt.Errorf("local library source changed during rename detection")
			}
			pathChanging := locked.RootID != rootID || locked.NormalizedPath != normalizedPath
			if pathChanging {
				var blocked bool
				if err := transaction.QueryRow(ctx, `SELECT EXISTS(
					SELECT 1 FROM metadata_writeback_jobs
					WHERE source_id=$1 AND status IN ('PENDING','PROCESSING')
				)`, locked.ID).Scan(&blocked); err != nil {
					return localSourceRecord{}, fmt.Errorf("check Tag writeback path freeze: %w", err)
				}
				if blocked {
					return localSourceRecord{}, fmt.Errorf("Tag writeback keeps the local source path frozen")
				}
			}
			now := synchronizer.now()
			_, err = transaction.Exec(ctx, `UPDATE local_music_sources SET
				source_path=$2,normalized_source_path=$3,size_bytes=$4,modified_at=$5,
				last_seen_at=$6,updated_at=$7 WHERE id=$1`,
				locked.ID, file.RelativePath, normalizedPath, metadata.Size(), metadata.ModTime(), seenAt, now)
			if err != nil {
				return localSourceRecord{}, fmt.Errorf("rename unchanged local library file: %w", err)
			}
			if err := transaction.Commit(ctx); err != nil {
				return localSourceRecord{}, fmt.Errorf("commit unchanged local library rename: %w", err)
			}
			locked.SourcePath, locked.NormalizedPath = file.RelativePath, normalizedPath
			locked.SizeBytes, locked.ModifiedAt, locked.LastSeenAt = metadata.Size(), metadata.ModTime(), seenAt
			return locked, nil
		}
	}
	probed, err := synchronizer.probe.Probe(ctx, file.AudioPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	raw := probed.Metadata
	sidecars, err := readSidecarLyrics(file.AudioPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	lyrics := mergeLyrics(sidecars, raw.Lyrics)
	if len(lyrics) > 0 {
		defaultLyric := lyrics[0]
		for _, lyric := range lyrics {
			if lyric.IsDefault {
				defaultLyric = lyric
				break
			}
		}
		raw.Lyrics = &adminmetadata.MetadataLyrics{
			Content: defaultLyric.Content, Format: defaultLyric.Format, Language: defaultLyric.Language,
		}
	}
	trackID := uuid.NewString()
	if found {
		trackID = existing.TrackID
	}
	objectKey := fmt.Sprintf("library/sources/%s/%s%s", trackID, checksum, strings.ToLower(filepath.Ext(file.AudioPath)))
	mimeType, err := sourceMediaType(file.AudioPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	uploadedSize, err := synchronizer.storage.UploadFile(ctx, objectKey, file.AudioPath, mimeType, checksum)
	if err != nil {
		return localSourceRecord{}, err
	}
	artwork, err := synchronizer.stageArtwork(ctx, file.AudioPath, raw.HasArtwork)
	if err != nil {
		_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), objectKey, "ABANDONED_LIBRARY_SOURCE")
		return localSourceRecord{}, err
	}
	source, artworkUsed, err := synchronizer.storeStandardFile(ctx, standardFileMutation{
		RootID: rootID, ScanRunID: scanRunID, File: file, SeenAt: seenAt, Metadata: metadata, Existing: existing,
		ExistingFound: found, Raw: raw, Lyrics: lyrics, TrackID: trackID, ObjectKey: objectKey,
		MimeType: mimeType, UploadedSize: uploadedSize, Checksum: checksum,
		PreserveCueMappings: preserveCueMappings, Artwork: artwork,
	})
	if err != nil {
		_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), objectKey, "ABANDONED_LIBRARY_SOURCE")
		if artwork != nil {
			_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), artwork.ObjectKey, "ABANDONED_LIBRARY_ARTWORK")
		}
		return localSourceRecord{}, err
	}
	if artwork != nil && !artworkUsed {
		_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), artwork.ObjectKey, "UNUSED_LIBRARY_ARTWORK")
	}
	return source, nil
}

type standardFileMutation struct {
	RootID              string
	ScanRunID           string
	File                DiscoveredFile
	SeenAt              time.Time
	Metadata            os.FileInfo
	Existing            localSourceRecord
	ExistingFound       bool
	Raw                 adminmetadata.MetadataSnapshot
	Lyrics              []scannedLyric
	TrackID             string
	ObjectKey           string
	MimeType            string
	UploadedSize        int64
	Checksum            string
	PreserveCueMappings bool
	Artwork             *stagedArtwork
}

func (synchronizer *ProductionSynchronizer) markSourceFailed(
	ctx context.Context,
	rootID, normalizedPath string,
	failure error,
	seenAt time.Time,
) error {
	message := truncateError(failure)
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	var sourceID *string
	err = transaction.QueryRow(ctx, `UPDATE local_music_sources SET
		status='FAILED',last_error=$3,last_seen_at=$4,updated_at=now()
		WHERE root_id=$1 AND normalized_source_path=$2 RETURNING id`,
		rootID, normalizedPath, message, seenAt).Scan(&sourceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if sourceID != nil {
		if _, err := transaction.Exec(ctx, `UPDATE tracks SET
			status='ERROR',version=version+1,updated_at=now()
			WHERE id IN(SELECT track_id FROM local_music_source_tracks WHERE source_id=$1)
			AND status<>'ARCHIVED'`, *sourceID); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}
