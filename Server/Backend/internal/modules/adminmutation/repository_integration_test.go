package adminmutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/testsupport"
)

func TestAdminMutationProductionLifecycle(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production admin mutation lifecycle")
	}
	testsupport.RequireWriteIntegration(t)
	absolute, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolute).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absolute))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var actorID string
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE role='ADMIN' AND status='ACTIVE' ORDER BY id LIMIT 1`).Scan(&actorID); err != nil {
		t.Skipf("no active administrator: %v", err)
	}
	repository := NewRepository(pool.Pool)
	service, err := NewService(repository, mutationArtworkStub{}, cfg.LocalLibrary.Directory)
	if err != nil {
		t.Fatal(err)
	}
	createdIDs := []string{}
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if len(createdIDs) > 0 {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit_logs WHERE target_id=ANY($1::uuid[])`, createdIDs)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM tracks WHERE title LIKE '__admin_mutation_it_%'`)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM media_assets WHERE object_key LIKE 'integration/admin-mutation/%'`)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM albums WHERE title LIKE '__admin_mutation_it_%'`)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM artists WHERE name LIKE '__admin_mutation_it_%'`)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE username LIKE 'admin_mutation_it_%'`)
	}
	t.Cleanup(cleanup)
	suffix := strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	artist, err := service.CreateArtist(ctx, actorID, "mutation-create-artist", CreateArtistInput{Name: "__admin_mutation_it_artist_" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	createdIDs = append(createdIDs, artist.ID)
	artist, err = service.UpdateArtist(ctx, actorID, "mutation-update-artist", artist.ID, UpdateArtistInput{ExpectedVersion: artist.Version, Description: OptionalNullableString{Set: true, Value: stringPointer("integration")}})
	if err != nil || artist.Version != 2 {
		t.Fatalf("UpdateArtist=%#v,%v", artist, err)
	}
	credit := CreditInput{ArtistID: artist.ID, Role: CreditPrimary, SortOrder: 0, SortOrderSet: true}
	title := "__admin_mutation_it_album_" + suffix
	target, err := service.CreateAlbum(ctx, actorID, "mutation-create-album-1", CreateAlbumInput{Title: title, ArtistCredits: []CreditInput{credit}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.CreateAlbum(ctx, actorID, "mutation-create-album-2", CreateAlbumInput{Title: title, ArtistCredits: []CreditInput{credit}})
	if err != nil {
		t.Fatal(err)
	}
	createdIDs = append(createdIDs, target.ID, source.ID)
	targetTrack, err := service.CreateTrack(ctx, actorID, "mutation-create-track-1", CreateTrackInput{Title: "__admin_mutation_it_target_" + suffix, AlbumID: OptionalNullableString{Set: true, Value: &target.ID}, ArtistCredits: []CreditInput{credit}, DiscNumber: OptionalInt{Set: true, Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	sourceTrack, err := service.CreateTrack(ctx, actorID, "mutation-create-track-2", CreateTrackInput{Title: "__admin_mutation_it_source_" + suffix, AlbumID: OptionalNullableString{Set: true, Value: &source.ID}, ArtistCredits: []CreditInput{credit}, DiscNumber: OptionalInt{Set: true, Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	createdIDs = append(createdIDs, targetTrack.ID, sourceTrack.ID)
	content := "[00:00.00] integration"
	lyric, err := service.UpsertLyrics(ctx, actorID, "mutation-lyrics", targetTrack.ID, LyricsInput{ExpectedVersion: targetTrack.Version, Language: "zh", Format: "LRC", Content: OptionalString{Set: true, Value: content}, IsDefault: OptionalBool{Set: true, Value: true}})
	if err != nil || lyric.TrackVersion != 2 {
		t.Fatalf("UpsertLyrics=%#v,%v", lyric, err)
	}
	newTitle := "__admin_mutation_it_target_updated_" + suffix
	targetTrack, err = service.UpdateTrack(ctx, actorID, "mutation-update-track", targetTrack.ID, UpdateTrackInput{ExpectedVersion: lyric.TrackVersion, Title: OptionalString{Set: true, Value: newTitle}})
	if err != nil || targetTrack.Version != 3 {
		t.Fatalf("UpdateTrack=%#v,%v", targetTrack, err)
	}
	merge, err := service.MergeAlbums(ctx, actorID, "mutation-merge", MergeAlbumsInput{Target: AlbumVersionInput{AlbumID: target.ID, ExpectedVersion: target.Version}, Sources: []AlbumVersionInput{{AlbumID: source.ID, ExpectedVersion: source.Version}}, FieldSources: AlbumMergeFieldSources{Title: target.ID, Cover: OptionalNullableString{Set: true}, ArtistCredits: target.ID, ReleaseDate: OptionalNullableString{Set: true}, Description: OptionalNullableString{Set: true}}})
	if err != nil || merge.MovedTracks != 1 || merge.MergedAlbums != 1 {
		t.Fatalf("MergeAlbums=%#v,%v", merge, err)
	}
	if _, err := service.PublishTrack(ctx, actorID, "mutation-publish", targetTrack.ID, targetTrack.Version); !apperror.IsCode(err, apperror.CodeTrackNotPlayable) {
		t.Fatalf("PublishTrack error=%v", err)
	}
	sourceTrack, err = service.ArchiveTrack(ctx, actorID, "mutation-archive", sourceTrack.ID, sourceTrack.Version+1)
	if err != nil {
		t.Fatalf("ArchiveTrack=%v", err)
	}
	if _, err := service.ArchiveTrack(ctx, actorID, "mutation-archive-repeat", sourceTrack.ID, sourceTrack.Version); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("ArchiveTrack repeated error=%v", err)
	}
	if _, err := service.PublishTrack(ctx, actorID, "mutation-publish-archived", sourceTrack.ID, sourceTrack.Version); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("PublishTrack archived error=%v", err)
	}
	if _, err := service.UpdateTrack(ctx, actorID, "mutation-update-archived", sourceTrack.ID, UpdateTrackInput{
		ExpectedVersion: sourceTrack.Version,
		Title:           OptionalString{Set: true, Value: "Archived title must not change"},
	}); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("UpdateTrack archived error=%v", err)
	}
	if _, err := service.UpsertLyrics(ctx, actorID, "mutation-lyrics-archived", sourceTrack.ID, LyricsInput{
		ExpectedVersion: sourceTrack.Version, Language: "zh", Format: "LRC",
		Content: OptionalString{Set: true, Value: "[00:00.00] archived"}, IsDefault: OptionalBool{Set: true, Value: true},
	}); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("UpsertLyrics archived error=%v", err)
	}
	if _, err := service.RestoreTrack(ctx, actorID, "mutation-restore-not-playable", sourceTrack.ID, sourceTrack.Version); !apperror.IsCode(err, apperror.CodeTrackNotPlayable) {
		t.Fatalf("RestoreTrack unplayable error=%v", err)
	}
	playableAssetID := uuid.NewString()
	if _, err := pool.Exec(ctx, `UPDATE tracks SET duration_ms=120000 WHERE id=$1`, sourceTrack.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_assets(
		id,object_key,kind,mime_type,size_bytes,status
	) VALUES($1,$2,'AUDIO_VARIANT','audio/ogg',1,'READY')`,
		playableAssetID, "integration/admin-mutation/"+playableAssetID+".ogg"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO track_variants(
		track_id,asset_id,quality,mime_type,codec,container,bitrate,status
	) VALUES($1,$2,'STANDARD','audio/ogg','opus','ogg',128000,'READY')`, sourceTrack.ID, playableAssetID); err != nil {
		t.Fatal(err)
	}
	sourceTrack, err = service.RestoreTrack(ctx, actorID, "mutation-restore", sourceTrack.ID, sourceTrack.Version)
	if err != nil || sourceTrack.Status != "READY" {
		t.Fatalf("RestoreTrack=%#v,%v", sourceTrack, err)
	}
	var restoreAudited bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_logs
		WHERE action='admin.track.restore' AND target_id=$1 AND trace_id='mutation-restore')`, sourceTrack.ID).Scan(&restoreAudited); err != nil {
		t.Fatal(err)
	}
	if !restoreAudited {
		t.Fatal("RestoreTrack audit was not written")
	}
	if _, err := service.RestoreTrack(ctx, actorID, "mutation-restore-repeat", sourceTrack.ID, sourceTrack.Version); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("RestoreTrack repeated error=%v", err)
	}
	if _, err := service.ArchiveTrack(ctx, actorID, "mutation-archive-stale", sourceTrack.ID, sourceTrack.Version-1); !apperror.IsCode(err, apperror.CodeVersionConflict) {
		t.Fatalf("ArchiveTrack stale error=%v", err)
	}
	sourceTrack, err = service.ArchiveTrack(ctx, actorID, "mutation-archive-after-restore", sourceTrack.ID, sourceTrack.Version)
	if err != nil || sourceTrack.Status != "ARCHIVED" {
		t.Fatalf("ArchiveTrack after restore=%#v,%v", sourceTrack, err)
	}
	probeAssetID := uuid.NewString()
	probeJobID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO media_assets(
		id,object_key,kind,mime_type,size_bytes,status
	) VALUES($1,$2,'AUDIO_SOURCE','audio/flac',1,'READY')`,
		probeAssetID, "integration/admin-mutation/"+probeAssetID+".flac"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_jobs(
		id,type,source_asset_id,track_id,status,idempotency_key,next_attempt_at
	) VALUES($1,'INGEST_TRACK',$2,$3,'PENDING',$4,now()+interval '1 hour')`,
		probeJobID, probeAssetID, sourceTrack.ID, "admin-mutation-probe-"+probeJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteTrackPermanently(ctx, actorID, "mutation-delete-conflict", sourceTrack.ID, sourceTrack.Version); !apperror.IsCode(err, apperror.CodeResourceConflict) {
		t.Fatalf("DeleteTrack active media error=%v", err)
	} else if applicationError, ok := apperror.As(err); !ok || applicationError.Metadata["conflictResourceType"] != "media_job" {
		t.Fatalf("DeleteTrack active media metadata=%v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM media_jobs WHERE id=$1`, probeJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM media_assets WHERE id=$1`, probeAssetID); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.DeleteTrackPermanently(ctx, actorID, "mutation-delete", sourceTrack.ID, sourceTrack.Version)
	if err != nil || !deleted.Deleted {
		t.Fatalf("DeleteTrack=%#v,%v", deleted, err)
	}
	userID := uuid.NewString()
	username := "admin_mutation_it_" + suffix
	createdIDs = append(createdIDs, userID)
	passwordHash, err := platformsecurity.HashPassword("admin-mutation-integration-" + userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,normalized_username,password_hash,role,status) VALUES($1,$2,$2,$3,'USER','ACTIVE')`, userID, username, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(user_id,display_name) VALUES($1,'Mutation Integration')`, userID); err != nil {
		t.Fatal(err)
	}
	user, err := service.UpdateUserStatus(ctx, actorID, "mutation-user-status", userID, UserStatusInput{ExpectedVersion: 1, Status: UserSuspended, Reason: "integration"})
	if err != nil || user.Status != UserSuspended || user.Version != 2 {
		t.Fatalf("UpdateUserStatus=%#v,%v", user, err)
	}
}

func TestArchiveAndPermanentDeleteCoordinateMetadataWritebacks(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run writeback deletion integration")
	}
	testsupport.RequireWriteIntegration(t)
	absolute, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolute).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absolute))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	rootPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	var trackID, rootID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tracks(title,normalized_title,duration_ms,status)
		VALUES($1,$2,1000,'READY') RETURNING id::text`,
		"__admin_mutation_it_writeback_"+suffix,
		"__admin_mutation_it_writeback_"+suffix,
	).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO library_roots(name,path,normalized_path,mode,enabled,status,scan_on_startup)
		VALUES($1,$2,$2,'READ_WRITE',true,'READY',false) RETURNING id::text`,
		"__admin_mutation_it_root_"+suffix, rootPath,
	).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM library_roots WHERE id=$1`, rootID)
	})

	type sourceFixture struct {
		id, relative, absolute string
	}
	createSource := func(label string) sourceFixture {
		t.Helper()
		relative := label + "-" + suffix + ".flac"
		absolutePath := filepath.Join(rootPath, relative)
		if err := os.WriteFile(absolutePath, []byte("audio-"+label), 0o600); err != nil {
			t.Fatal(err)
		}
		var sourceID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO local_music_sources(
				source_path,normalized_source_path,checksum_sha256,size_bytes,
				modified_at,track_id,status,root_id
			) VALUES($1,$1,$2,100,now(),$3,'READY',$4) RETURNING id::text`,
			relative, strings.Repeat("a", 64), trackID, rootID,
		).Scan(&sourceID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO local_music_source_tracks(source_id,track_id,segment_index,start_ms)
			VALUES($1,$2,0,0)`, sourceID, trackID); err != nil {
			t.Fatal(err)
		}
		return sourceFixture{id: sourceID, relative: relative, absolute: absolutePath}
	}
	pendingSource := createSource("pending")
	processingSource := createSource("processing")
	recoverySource := createSource("recovery")

	createWriteback := func(source sourceFixture, status string) string {
		t.Helper()
		var jobID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO metadata_writeback_jobs(
				track_id,source_id,reason,metadata_snapshot,metadata_version,
				expected_source_checksum,root_path_snapshot,source_path_snapshot,status
			) VALUES($1,$2,'integration writeback','{}'::jsonb,1,$3,$4,$5,$6::metadata_writeback_status)
			RETURNING id::text`, trackID, source.id, strings.Repeat("a", 64),
			rootPath, source.relative, status,
		).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		return jobID
	}
	pendingJobID := createWriteback(pendingSource, "PENDING")
	processingJobID := createWriteback(processingSource, "PROCESSING")
	recoveryJobID := createWriteback(recoverySource, "PENDING")
	processingAttemptID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		UPDATE metadata_writeback_jobs SET attempt_id=$2,stage='PREPARING',
			locked_by='integration-worker',locked_until=now()+interval '1 hour'
		WHERE id=$1`, processingJobID, processingAttemptID); err != nil {
		t.Fatal(err)
	}
	recoveryBackup := filepath.Join(rootPath, ".recovery-"+suffix+".legacy.bak")
	if err := os.WriteFile(recoveryBackup, []byte("recovery-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE metadata_writeback_jobs SET backup_path=$2,stage='PREPARED'
		WHERE id=$1`, recoveryJobID, recoveryBackup); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	if err := repository.ArchiveTrack(ctx, trackID, 1); err != nil {
		t.Fatal(err)
	}
	assertWriteback := func(jobID, expectedStatus string, expectedCancel bool) {
		t.Helper()
		var status string
		var requested bool
		if err := pool.QueryRow(ctx, `
			SELECT status::text,cancel_requested FROM metadata_writeback_jobs WHERE id=$1`,
			jobID,
		).Scan(&status, &requested); err != nil {
			t.Fatal(err)
		}
		if status != expectedStatus || requested != expectedCancel {
			t.Fatalf("writeback %s=%s/%v want %s/%v", jobID, status, requested, expectedStatus, expectedCancel)
		}
	}
	assertWriteback(pendingJobID, "CANCELLED", true)
	assertWriteback(processingJobID, "PROCESSING", true)
	assertWriteback(recoveryJobID, "CANCELLED", true)

	if _, err := repository.DeleteTrackPermanently(ctx, trackID, 2, rootPath); !apperror.IsCode(err, apperror.CodeResourceConflict) {
		t.Fatalf("processing delete error=%v", err)
	} else if applicationError, ok := apperror.As(err); !ok ||
		applicationError.Metadata["conflictResourceType"] != "metadata_writeback_job" ||
		applicationError.Metadata["conflictResourceId"] != processingJobID ||
		applicationError.Metadata["cancellationRequested"] != true {
		t.Fatalf("processing delete metadata=%v", err)
	}

	processingBackup := filepath.Join(rootPath, ".processing-"+suffix+".legacy.bak")
	if err := os.WriteFile(processingBackup, []byte("processing-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE metadata_writeback_jobs SET status='FAILED',backup_path=$2,stage='PREPARED',
			locked_by=NULL,locked_until=NULL,completed_at=now()
		WHERE id=$1`, processingJobID, processingBackup); err != nil {
		t.Fatal(err)
	}
	readyJobID := createWriteback(pendingSource, "READY")
	readyBackup := filepath.Join(rootPath, ".ready-"+suffix+".legacy.bak")
	if err := os.WriteFile(readyBackup, []byte("ready-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE metadata_writeback_jobs SET backup_path=$2,stage='COMMITTED',completed_at=now()
		WHERE id=$1`, readyJobID, readyBackup); err != nil {
		t.Fatal(err)
	}

	deleted, err := repository.DeleteTrackPermanently(ctx, trackID, 2, rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedFiles != 3 || deleted.QuarantinedFiles != 0 {
		t.Fatalf("delete result=%+v", deleted)
	}
	for _, path := range []string{pendingSource.absolute, processingSource.absolute, recoverySource.absolute} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted path remains %q: %v", path, err)
		}
	}
	for path, expected := range map[string]string{
		processingBackup: "processing-backup",
		recoveryBackup:   "recovery-backup",
		readyBackup:      "ready-backup",
	} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != expected {
			t.Fatalf("legacy backup was touched %q: contents=%q error=%v", path, contents, err)
		}
	}
}

type mutationArtworkStub struct{}

func (mutationArtworkStub) Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error) {
	return map[string]catalog.ArtworkDTO{}, nil
}
func stringPointer(value string) *string { return &value }
