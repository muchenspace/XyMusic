package adminmutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) ArtistsExist(ctx context.Context, ids []string) (bool, error) {
	var count int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*)::int FROM artists WHERE id = ANY($1::uuid[])`, ids).Scan(&count); err != nil {
		return false, fmt.Errorf("count admin mutation artists: %w", err)
	}
	return count == len(ids), nil
}
func (repository *Repository) AlbumExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM albums WHERE id=$1)`, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect admin mutation album: %w", err)
	}
	return exists, nil
}

func (repository *Repository) CreateArtist(ctx context.Context, input CreateArtistParams) (string, error) {
	var id string
	err := repository.pool.QueryRow(ctx, `INSERT INTO artists(name,normalized_name,description) VALUES($1,$2,$3) RETURNING id`, input.Name, input.NormalizedName, input.Description).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create admin artist: %w", err)
	}
	return id, nil
}
func (repository *Repository) UpdateArtist(ctx context.Context, input UpdateArtistParams) error {
	command, err := repository.pool.Exec(ctx, `UPDATE artists SET name=CASE WHEN $3 THEN $4 ELSE name END,normalized_name=CASE WHEN $3 THEN $5 ELSE normalized_name END,description=CASE WHEN $6 THEN $7 ELSE description END,version=version+1,updated_at=now() WHERE id=$1 AND version=$2`, input.ID, input.ExpectedVersion, input.Name != nil, input.Name, input.NormalizedName, input.SetDescription, input.Description)
	if err != nil {
		return fmt.Errorf("update admin artist: %w", err)
	}
	if command.RowsAffected() != 1 {
		return repository.versionFailure(ctx, "Artist", "artists", input.ID, input.ExpectedVersion)
	}
	return nil
}

func (repository *Repository) CreateAlbum(ctx context.Context, input CreateAlbumParams) (string, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin admin album creation: %w", err)
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO albums(title,normalized_title,release_date,description) VALUES($1,$2,$3,$4) RETURNING id`, input.Title, input.NormalizedTitle, input.ReleaseDate, input.Description).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create admin album: %w", err)
	}
	if err := insertAlbumCredits(ctx, tx, id, input.Credits); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit admin album creation: %w", err)
	}
	return id, nil
}
func (repository *Repository) UpdateAlbum(ctx context.Context, input UpdateAlbumParams) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin admin album update: %w", err)
	}
	defer tx.Rollback(ctx)
	var version int
	err = tx.QueryRow(ctx, `SELECT version FROM albums WHERE id=$1 FOR UPDATE`, input.ID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Album was not found")
	}
	if err != nil {
		return fmt.Errorf("lock admin album: %w", err)
	}
	if version != input.ExpectedVersion {
		return versionConflict("Album", input.ExpectedVersion, version, nil)
	}
	_, err = tx.Exec(ctx, `UPDATE albums SET title=CASE WHEN $3 THEN $4 ELSE title END,normalized_title=CASE WHEN $3 THEN $5 ELSE normalized_title END,release_date=CASE WHEN $6 THEN $7::date ELSE release_date END,description=CASE WHEN $8 THEN $9 ELSE description END,version=version+1,updated_at=now() WHERE id=$1 AND version=$2`, input.ID, input.ExpectedVersion, input.Title != nil, input.Title, input.NormalizedTitle, input.SetReleaseDate, input.ReleaseDate, input.SetDescription, input.Description)
	if err != nil {
		return fmt.Errorf("update admin album: %w", err)
	}
	if input.SetCredits {
		if _, err := tx.Exec(ctx, `DELETE FROM album_artists WHERE album_id=$1`, input.ID); err != nil {
			return fmt.Errorf("replace admin album credits: %w", err)
		}
		if err := insertAlbumCredits(ctx, tx, input.ID, input.Credits); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin album update: %w", err)
	}
	return nil
}

func (repository *Repository) MergeAlbums(ctx context.Context, actorID, traceID string, input MergeAlbumsInput) (MergeResultDTO, error) {
	expected := map[string]int{input.Target.AlbumID: input.Target.ExpectedVersion}
	sourceIDs := make([]string, 0, len(input.Sources))
	for _, source := range input.Sources {
		expected[source.AlbumID] = source.ExpectedVersion
		sourceIDs = append(sourceIDs, source.AlbumID)
	}
	ids := make([]string, 0, len(expected))
	for id := range expected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeResultDTO{}, fmt.Errorf("begin admin album merge: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,title,normalized_title,cover_asset_id,release_date::text,description,version FROM albums WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return MergeResultDTO{}, fmt.Errorf("lock merge albums: %w", err)
	}
	type mergeAlbum struct {
		id, title, normalized       string
		cover, release, description *string
		version                     int
	}
	byID := map[string]mergeAlbum{}
	for rows.Next() {
		var a mergeAlbum
		if err := rows.Scan(&a.id, &a.title, &a.normalized, &a.cover, &a.release, &a.description, &a.version); err != nil {
			rows.Close()
			return MergeResultDTO{}, fmt.Errorf("scan merge album: %w", err)
		}
		byID[a.id] = a
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return MergeResultDTO{}, fmt.Errorf("iterate merge albums: %w", err)
	}
	if len(byID) != len(ids) {
		return MergeResultDTO{}, apperror.NotFound("One or more albums were not found")
	}
	normalized := ""
	for _, id := range ids {
		a := byID[id]
		if a.version != expected[id] {
			return MergeResultDTO{}, versionConflict("Album", expected[id], a.version, map[string]any{"albumId": id})
		}
		if normalized == "" {
			normalized = a.normalized
		} else if normalized != a.normalized {
			return MergeResultDTO{}, apperror.Validation("Only albums with the same normalized title can be merged")
		}
	}
	creditRows, err := tx.Query(ctx, `SELECT artist_id,role::text,sort_order FROM album_artists WHERE album_id=$1 ORDER BY sort_order,artist_id`, input.FieldSources.ArtistCredits)
	if err != nil {
		return MergeResultDTO{}, fmt.Errorf("query merge credits: %w", err)
	}
	credits := []CreditInput{}
	for creditRows.Next() {
		var c CreditInput
		if err := creditRows.Scan(&c.ArtistID, &c.Role, &c.SortOrder); err != nil {
			creditRows.Close()
			return MergeResultDTO{}, fmt.Errorf("scan merge credit: %w", err)
		}
		credits = append(credits, c)
	}
	err = creditRows.Err()
	creditRows.Close()
	if err != nil {
		return MergeResultDTO{}, fmt.Errorf("iterate merge credits: %w", err)
	}
	if len(credits) == 0 {
		return MergeResultDTO{}, apperror.Validation("The selected artist credit source has no credits")
	}
	titleSource := byID[input.FieldSources.Title]
	var cover, release, description *string
	if input.FieldSources.Cover.Value != nil {
		cover = byID[*input.FieldSources.Cover.Value].cover
	}
	if input.FieldSources.ReleaseDate.Value != nil {
		release = byID[*input.FieldSources.ReleaseDate.Value].release
	}
	if input.FieldSources.Description.Value != nil {
		description = byID[*input.FieldSources.Description.Value].description
	}
	targetBefore := byID[input.Target.AlbumID]
	moved, err := tx.Exec(ctx, `UPDATE tracks SET album_id=$1,version=version+1,updated_at=now() WHERE album_id=ANY($2::uuid[])`, input.Target.AlbumID, sourceIDs)
	if err != nil {
		return MergeResultDTO{}, fmt.Errorf("move merged album tracks: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE albums SET title=$2,normalized_title=$3,cover_asset_id=$4,release_date=$5::date,description=$6,version=version+1,updated_at=now() WHERE id=$1`, input.Target.AlbumID, titleSource.title, titleSource.normalized, cover, release, description); err != nil {
		return MergeResultDTO{}, fmt.Errorf("update merge target: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM album_artists WHERE album_id=$1`, input.Target.AlbumID); err != nil {
		return MergeResultDTO{}, err
	}
	if err := insertAlbumCredits(ctx, tx, input.Target.AlbumID, credits); err != nil {
		return MergeResultDTO{}, err
	}
	if !sameString(targetBefore.cover, cover) {
		if err := scheduleArtworkCleanup(ctx, tx, targetBefore.cover, "REPLACED_ALBUM_COVER_AFTER_ADMIN_MERGE"); err != nil {
			return MergeResultDTO{}, err
		}
	}
	for _, sourceID := range sourceIDs {
		deleted, err := deleteAlbumIfEmpty(ctx, tx, sourceID, "EMPTY_ALBUM_AFTER_ADMIN_MERGE")
		if err != nil {
			return MergeResultDTO{}, err
		}
		if !deleted {
			return MergeResultDTO{}, apperror.Conflict(apperror.CodeResourceConflict, "Source album could not be deleted after moving its tracks", map[string]any{"albumId": sourceID})
		}
	}
	details := map[string]any{"sourceAlbumIds": sourceIDs, "movedTracks": int(moved.RowsAffected()), "fieldSources": input.FieldSources}
	if err := writeAudit(ctx, tx, actorID, "admin.album.merge", "album", input.Target.AlbumID, traceID, details); err != nil {
		return MergeResultDTO{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeResultDTO{}, fmt.Errorf("commit admin album merge: %w", err)
	}
	return MergeResultDTO{TargetAlbumID: input.Target.AlbumID, MergedAlbums: len(sourceIDs), MovedTracks: int(moved.RowsAffected()), TargetVersion: input.Target.ExpectedVersion + 1}, nil
}

func (repository *Repository) CreateTrack(ctx context.Context, input CreateTrackParams) (string, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin admin track creation: %w", err)
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO tracks(title,normalized_title,album_id,track_number,disc_number) VALUES($1,$2,$3,$4,$5) RETURNING id`, input.Title, input.NormalizedTitle, input.AlbumID, input.TrackNumber, input.DiscNumber).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create admin track: %w", err)
	}
	if err := insertTrackCredits(ctx, tx, id, input.Credits); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit admin track creation: %w", err)
	}
	return id, nil
}
func (repository *Repository) UpdateTrack(ctx context.Context, input UpdateTrackParams) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin admin track update: %w", err)
	}
	defer tx.Rollback(ctx)
	state, err := lockTrackMutationState(ctx, tx, input.ID)
	if err != nil {
		return err
	}
	if state.Version != input.ExpectedVersion {
		return versionConflict("Track", input.ExpectedVersion, state.Version, nil)
	}
	if state.Status == "ARCHIVED" {
		return invalidTrackTransition("Archived tracks cannot be edited")
	}
	_, err = tx.Exec(ctx, `UPDATE tracks SET title=CASE WHEN $3 THEN $4 ELSE title END,normalized_title=CASE WHEN $3 THEN $5 ELSE normalized_title END,album_id=CASE WHEN $6 THEN $7::uuid ELSE album_id END,track_number=CASE WHEN $8 THEN $9 ELSE track_number END,disc_number=CASE WHEN $10 THEN $11 ELSE disc_number END,version=version+1,updated_at=now() WHERE id=$1 AND version=$2`, input.ID, input.ExpectedVersion, input.Title != nil, input.Title, input.NormalizedTitle, input.SetAlbum, input.AlbumID, input.SetTrackNumber, input.TrackNumber, input.DiscNumber != nil, input.DiscNumber)
	if err != nil {
		return fmt.Errorf("update admin track: %w", err)
	}
	if input.SetCredits {
		if _, err := tx.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1`, input.ID); err != nil {
			return err
		}
		if err := insertTrackCredits(ctx, tx, input.ID, input.Credits); err != nil {
			return err
		}
	}
	if input.SetAlbum && !sameString(state.AlbumID, input.AlbumID) {
		if _, err := deleteAlbumIfEmpty(ctx, tx, stringValue(state.AlbumID), "EMPTY_ALBUM_AFTER_ADMIN_UPDATE"); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin track update: %w", err)
	}
	return nil
}

func (repository *Repository) PublishTrack(ctx context.Context, id string, expectedVersion int) error {
	return repository.transitionTrackToReady(ctx, id, expectedVersion, false)
}
func (repository *Repository) ArchiveTrack(ctx context.Context, id string, expectedVersion int) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin admin track archive: %w", err)
	}
	defer tx.Rollback(ctx)
	state, err := lockTrackMutationState(ctx, tx, id)
	if err != nil {
		return err
	}
	if state.Version != expectedVersion {
		return versionConflict("Track", expectedVersion, state.Version, nil)
	}
	if state.Status == "ARCHIVED" {
		return invalidTrackTransition("Track is already archived")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_writeback_jobs SET
			cancel_requested = true,
			status = CASE
				WHEN status = 'PENDING'
					THEN 'CANCELLED'::metadata_writeback_status
				ELSE status
			END,
			completed_at = CASE
				WHEN status = 'PENDING' THEN now()
				ELSE completed_at
			END,
			locked_by = CASE
				WHEN status = 'PENDING' THEN NULL
				ELSE locked_by
			END,
			locked_until = CASE
				WHEN status = 'PENDING' THEN NULL
				ELSE locked_until
			END,
			last_error_code = CASE
				WHEN status = 'PENDING' THEN NULL
				ELSE last_error_code
			END,
			last_error = CASE
				WHEN status = 'PENDING' THEN NULL
				ELSE last_error
			END,
			version = version + 1,
			updated_at = now()
		WHERE track_id = $1 AND status IN ('PENDING', 'PROCESSING')`, id); err != nil {
		return fmt.Errorf("cancel metadata writebacks while archiving track: %w", err)
	}
	command, err := tx.Exec(ctx, `UPDATE tracks SET status='ARCHIVED',version=version+1,updated_at=now()
		WHERE id=$1 AND version=$2`, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("archive admin track: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.Conflict(apperror.CodeResourceConflict, "Track changed while it was being archived", nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin track archive: %w", err)
	}
	return nil
}

func (repository *Repository) RestoreTrack(ctx context.Context, id string, expectedVersion int) error {
	return repository.transitionTrackToReady(ctx, id, expectedVersion, true)
}

type trackMutationState struct {
	Status     string
	Version    int
	DurationMS int64
	AlbumID    *string
}

func lockTrackMutationState(ctx context.Context, tx pgx.Tx, id string) (trackMutationState, error) {
	var state trackMutationState
	err := tx.QueryRow(ctx, `SELECT status::text,version,duration_ms,album_id
		FROM tracks WHERE id=$1 FOR UPDATE`, id).Scan(
		&state.Status, &state.Version, &state.DurationMS, &state.AlbumID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return trackMutationState{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return trackMutationState{}, fmt.Errorf("lock admin track state: %w", err)
	}
	return state, nil
}

func (repository *Repository) transitionTrackToReady(
	ctx context.Context,
	id string,
	expectedVersion int,
	restore bool,
) error {
	operation := "publish"
	if restore {
		operation = "restore"
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin admin track %s: %w", operation, err)
	}
	defer tx.Rollback(ctx)
	state, err := lockTrackMutationState(ctx, tx, id)
	if err != nil {
		return err
	}
	if state.Version != expectedVersion {
		return versionConflict("Track", expectedVersion, state.Version, nil)
	}
	if restore {
		if state.Status != "ARCHIVED" {
			return invalidTrackTransition("Only archived tracks can be restored")
		}
	} else if state.Status == "ARCHIVED" {
		return invalidTrackTransition("Archived tracks must be restored before publishing")
	}
	if state.DurationMS <= 0 {
		return apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Track duration must be positive", nil)
	}
	var readyVariant bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM track_variants WHERE track_id=$1 AND status='READY'
	)`, id).Scan(&readyVariant); err != nil {
		return fmt.Errorf("inspect playable variant: %w", err)
	}
	if !readyVariant {
		return apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Track has no ready playback variant", nil)
	}
	command, err := tx.Exec(ctx, `UPDATE tracks SET status='READY',published_at=now(),
		version=version+1,updated_at=now() WHERE id=$1 AND version=$2`, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("%s admin track: %w", operation, err)
	}
	if command.RowsAffected() != 1 {
		return apperror.Conflict(apperror.CodeResourceConflict,
			"Track changed during the "+operation+" operation", nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin track %s: %w", operation, err)
	}
	return nil
}

func invalidTrackTransition(detail string) error {
	return apperror.New(apperror.CodeInvalidStateTransition, detail)
}

func (repository *Repository) UpsertLyrics(ctx context.Context, trackID string, input LyricsInput) (StoredLyric, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StoredLyric{}, fmt.Errorf("begin lyrics upsert: %w", err)
	}
	defer tx.Rollback(ctx)
	state, err := lockTrackMutationState(ctx, tx, trackID)
	if err != nil {
		return StoredLyric{}, err
	}
	if state.Version != input.ExpectedVersion {
		return StoredLyric{}, versionConflict("Track", input.ExpectedVersion, state.Version, nil)
	}
	if state.Status == "ARCHIVED" {
		return StoredLyric{}, invalidTrackTransition("Archived tracks cannot edit lyrics")
	}
	var trackVersion int
	err = tx.QueryRow(ctx, `UPDATE tracks SET version=version+1,updated_at=now()
		WHERE id=$1 AND version=$2 RETURNING version`, trackID, input.ExpectedVersion).Scan(&trackVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredLyric{}, apperror.Conflict(apperror.CodeResourceConflict, "Track changed while lyrics were being updated", nil)
	}
	if err != nil {
		return StoredLyric{}, fmt.Errorf("update lyrics track version: %w", err)
	}
	if input.IsDefault.Value {
		if _, err := tx.Exec(ctx, `UPDATE lyrics SET is_default=false WHERE track_id=$1`, trackID); err != nil {
			return StoredLyric{}, fmt.Errorf("clear default lyrics: %w", err)
		}
	}
	var stored StoredLyric
	err = tx.QueryRow(ctx, `INSERT INTO lyrics(track_id,language,origin,format,content,is_default) VALUES($1,$2,'MANUAL',$3,$4,$5) ON CONFLICT(track_id,language) DO UPDATE SET format=excluded.format,content=excluded.content,origin='MANUAL',is_default=excluded.is_default,version=lyrics.version+1,updated_at=now() RETURNING id,language,format::text,coalesce(content,''),is_default,updated_at`, trackID, input.Language, input.Format, input.Content.Value, input.IsDefault.Value).Scan(&stored.ID, &stored.Language, &stored.Format, &stored.Content, &stored.IsDefault, &stored.UpdatedAt)
	if err != nil {
		return StoredLyric{}, fmt.Errorf("store admin lyrics: %w", err)
	}
	stored.TrackVersion = trackVersion
	if err := tx.Commit(ctx); err != nil {
		return StoredLyric{}, fmt.Errorf("commit lyrics upsert: %w", err)
	}
	return stored, nil
}

func (repository *Repository) UpdateUserStatus(ctx context.Context, actorID, userID string, expectedVersion int, status UserStatus) error {
	if actorID == userID && status == UserSuspended {
		return apperror.Validation("Administrators cannot suspend their own account")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin user status update: %w", err)
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `UPDATE users SET status=$3,auth_version=auth_version+1,version=version+1,updated_at=now() WHERE id=$1 AND version=$2`, userID, expectedVersion, status)
	if err != nil {
		return fmt.Errorf("update user status: %w", err)
	}
	if command.RowsAffected() != 1 {
		return repository.versionFailure(ctx, "User", "users", userID, expectedVersion)
	}
	if status == UserSuspended {
		if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens token SET revoked_at=now() FROM auth_sessions session WHERE token.session_id=session.id AND session.user_id=$1`, userID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit user status update: %w", err)
	}
	return nil
}

func (repository *Repository) FindArtist(ctx context.Context, id string) (ArtistRecord, error) {
	var r ArtistRecord
	err := repository.pool.QueryRow(ctx, `SELECT id,name,description,artwork_asset_id,version,created_at,updated_at FROM artists WHERE id=$1`, id).Scan(&r.ID, &r.Name, &r.Description, &r.ArtworkAssetID, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ArtistRecord{}, apperror.NotFound("Artist was not found")
	}
	if err != nil {
		return ArtistRecord{}, fmt.Errorf("query mutation artist: %w", err)
	}
	return r, nil
}
func (repository *Repository) FindAlbum(ctx context.Context, id string) (AlbumRecord, error) {
	var r AlbumRecord
	err := repository.pool.QueryRow(ctx, `SELECT id,title,description,cover_asset_id,release_date::text,version,created_at,updated_at FROM albums WHERE id=$1`, id).Scan(&r.ID, &r.Title, &r.Description, &r.CoverAssetID, &r.ReleaseDate, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AlbumRecord{}, apperror.NotFound("Album was not found")
	}
	if err != nil {
		return AlbumRecord{}, fmt.Errorf("query mutation album: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `SELECT artist.id,artist.name,credit.role::text,credit.sort_order FROM album_artists credit JOIN artists artist ON artist.id=credit.artist_id WHERE credit.album_id=$1 ORDER BY credit.sort_order`, id)
	if err != nil {
		return AlbumRecord{}, fmt.Errorf("query mutation album credits: %w", err)
	}
	r.Credits = []CreditRecord{}
	for rows.Next() {
		var c CreditRecord
		if err := rows.Scan(&c.ArtistID, &c.ArtistName, &c.Role, &c.SortOrder); err != nil {
			rows.Close()
			return AlbumRecord{}, err
		}
		r.Credits = append(r.Credits, c)
	}
	err = rows.Err()
	rows.Close()
	return r, err
}
func (repository *Repository) FindTrack(ctx context.Context, id string) (TrackRecord, error) {
	var r TrackRecord
	err := repository.pool.QueryRow(ctx, `SELECT t.id,t.title,t.status::text,t.album_id,al.title,al.cover_asset_id,t.duration_ms,t.track_number,t.disc_number,t.version,t.created_at,t.updated_at,(SELECT job.id FROM media_jobs job WHERE job.track_id=t.id AND job.status IN('PENDING','PROCESSING') LIMIT 1) FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=$1`, id).Scan(&r.ID, &r.Title, &r.Status, &r.AlbumID, &r.AlbumTitle, &r.AlbumCoverAssetID, &r.DurationMS, &r.TrackNumber, &r.DiscNumber, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.ActiveMediaJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackRecord{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return TrackRecord{}, fmt.Errorf("query mutation track: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `SELECT artist.id,artist.name,credit.role::text,credit.sort_order FROM track_artists credit JOIN artists artist ON artist.id=credit.artist_id WHERE credit.track_id=$1 ORDER BY credit.sort_order`, id)
	if err != nil {
		return TrackRecord{}, err
	}
	r.Credits = []CreditRecord{}
	for rows.Next() {
		var c CreditRecord
		if err := rows.Scan(&c.ArtistID, &c.ArtistName, &c.Role, &c.SortOrder); err != nil {
			rows.Close()
			return TrackRecord{}, err
		}
		r.Credits = append(r.Credits, c)
	}
	err = rows.Err()
	rows.Close()
	return r, err
}
func (repository *Repository) FindUser(ctx context.Context, id string) (UserRecord, error) {
	var r UserRecord
	err := repository.pool.QueryRow(ctx, `SELECT u.id,u.username,p.display_name,u.role::text,u.status::text,u.version,u.created_at,u.updated_at,p.updated_at FROM users u JOIN user_profiles p ON p.user_id=u.id WHERE u.id=$1`, id).Scan(&r.ID, &r.Username, &r.DisplayName, &r.Role, &r.Status, &r.Version, &r.CreatedAt, &r.UserUpdatedAt, &r.ProfileUpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRecord{}, apperror.NotFound("User was not found")
	}
	if err != nil {
		return UserRecord{}, fmt.Errorf("query mutation user: %w", err)
	}
	return r, nil
}

func (repository *Repository) WriteAudit(ctx context.Context, actorID, action, targetType, targetID, traceID string, details map[string]any) error {
	return writeAudit(ctx, repository.pool, actorID, action, targetType, targetID, traceID, details)
}

func writeAudit(ctx context.Context, executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, actorID, action, targetType, targetID, traceID string, details map[string]any) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = executor.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,target_type,target_id,result,trace_id,details) VALUES($1,$2,$3,$4,'SUCCESS',$5,$6::jsonb)`, actorID, action, targetType, targetID, traceID, encoded)
	if err != nil {
		return fmt.Errorf("write admin mutation audit: %w", err)
	}
	return nil
}

func (repository *Repository) versionFailure(ctx context.Context, label, table, id string, expected int) error {
	statement := fmt.Sprintf("SELECT version FROM %s WHERE id=$1", table)
	var current int
	err := repository.pool.QueryRow(ctx, statement, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound(label + " was not found")
	}
	if err != nil {
		return fmt.Errorf("inspect %s version: %w", label, err)
	}
	return versionConflict(label, expected, current, nil)
}
func versionConflict(label string, expected, current int, extra map[string]any) error {
	metadata := map[string]any{"expectedVersion": expected, "currentVersion": current}
	for k, v := range extra {
		metadata[k] = v
	}
	return apperror.Conflict(apperror.CodeVersionConflict, label+" version is stale", metadata)
}
func insertAlbumCredits(ctx context.Context, tx pgx.Tx, albumID string, credits []CreditInput) error {
	for _, c := range credits {
		if _, err := tx.Exec(ctx, `INSERT INTO album_artists(album_id,artist_id,role,sort_order) VALUES($1,$2,$3,$4)`, albumID, c.ArtistID, c.Role, c.SortOrder); err != nil {
			return fmt.Errorf("insert admin album credit: %w", err)
		}
	}
	return nil
}
func insertTrackCredits(ctx context.Context, tx pgx.Tx, trackID string, credits []CreditInput) error {
	for _, c := range credits {
		if _, err := tx.Exec(ctx, `INSERT INTO track_artists(track_id,artist_id,role,sort_order) VALUES($1,$2,$3,$4)`, trackID, c.ArtistID, c.Role, c.SortOrder); err != nil {
			return fmt.Errorf("insert admin track credit: %w", err)
		}
	}
	return nil
}
func deleteAlbumIfEmpty(ctx context.Context, tx pgx.Tx, albumID, reason string) (bool, error) {
	if albumID == "" {
		return false, nil
	}
	var cover *string
	err := tx.QueryRow(ctx, `DELETE FROM albums al WHERE al.id=$1 AND NOT EXISTS(SELECT 1 FROM tracks WHERE album_id=al.id) RETURNING cover_asset_id`, albumID).Scan(&cover)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("delete empty album: %w", err)
	}
	if err := scheduleArtworkCleanup(ctx, tx, cover, reason); err != nil {
		return false, err
	}
	return true, nil
}
func scheduleArtworkCleanup(ctx context.Context, tx pgx.Tx, assetID *string, reason string) error {
	if assetID == nil {
		return nil
	}
	var objectKey string
	err := tx.QueryRow(ctx, `UPDATE media_assets asset SET status='DELETE_PENDING',updated_at=now() WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM artists WHERE artwork_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM albums WHERE cover_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM playlists WHERE cover_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM user_profiles WHERE avatar_asset_id=asset.id) RETURNING object_key`, *assetID).Scan(&objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("detach album artwork: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO object_cleanup_jobs(object_key,reason) VALUES($1,$2) ON CONFLICT(object_key) DO UPDATE SET reason=excluded.reason,status='PENDING',attempts=0,attempt_id=NULL,locked_by=NULL,locked_until=NULL,next_attempt_at=now(),last_error=NULL,updated_at=now()`, objectKey, reason)
	return err
}
func sameString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ Store = (*Repository)(nil)
