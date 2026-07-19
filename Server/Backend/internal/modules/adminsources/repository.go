package adminsources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type Repository struct {
	database repositoryDatabase
}

type repositoryDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}

var (
	_ Store       = (*Repository)(nil)
	_ WorkerStore = (*Repository)(nil)
)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{database: pool} }

func (repository *Repository) ListRootViews(ctx context.Context, query RootQuery) ([]RootView, int, error) {
	roots, total, err := listRoots(ctx, repository.database, query)
	if err != nil {
		return nil, 0, err
	}
	rootIDs := make([]string, 0, len(roots))
	for _, root := range roots {
		rootIDs = append(rootIDs, root.ID)
	}
	counts, err := repository.rootCounts(ctx, rootIDs)
	if err != nil {
		return nil, 0, err
	}
	runs, err := repository.latestRuns(ctx, rootIDs)
	if err != nil {
		return nil, 0, err
	}
	views := make([]RootView, 0, len(roots))
	for _, root := range roots {
		view := RootView{Root: root, Counts: counts[root.ID]}
		if run, exists := runs[root.ID]; exists {
			copy := run
			view.LatestRun = &copy
		}
		views = append(views, view)
	}
	return views, total, nil
}

func (repository *Repository) FindRootView(ctx context.Context, rootID string) (RootView, error) {
	root, err := repository.FindRoot(ctx, rootID)
	if err != nil {
		return RootView{}, err
	}
	counts, err := rootCount(ctx, repository.database, rootID)
	if err != nil {
		return RootView{}, err
	}
	view := RootView{Root: root, Counts: counts}
	run, err := scanRun(repository.database.QueryRow(ctx, `
		SELECT `+scanRunColumns+` FROM library_scan_runs
		WHERE root_id=$1 ORDER BY created_at DESC, id DESC LIMIT 1`, rootID))
	if err == nil {
		view.LatestRun = &run
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return RootView{}, fmt.Errorf("find latest library scan: %w", err)
	}
	return view, nil
}

func (repository *Repository) FindRoot(ctx context.Context, rootID string) (Root, error) {
	root, err := scanRoot(repository.database.QueryRow(ctx,
		`SELECT `+rootColumns+` FROM library_roots WHERE id=$1`, rootID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Root{}, apperror.NotFound("Music source was not found")
	}
	if err != nil {
		return Root{}, fmt.Errorf("find music source: %w", err)
	}
	return root, nil
}

func (repository *Repository) CreateRoot(
	ctx context.Context,
	actorID, traceID string,
	mutation RootMutation,
) (RootView, error) {
	includePatterns, excludePatterns, err := encodePatterns(mutation)
	if err != nil {
		return RootView{}, err
	}
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return RootView{}, fmt.Errorf("begin music source creation: %w", err)
	}
	defer transaction.Rollback(ctx)
	root, err := scanRoot(transaction.QueryRow(ctx, `
		INSERT INTO library_roots (
			name,path,normalized_path,mode,tag_writeback_enabled,enabled,scan_on_startup,
			scan_interval_minutes,include_patterns,exclude_patterns,status
		) VALUES ($1,$2,$3,$4,false,$5,$6,$7,$8::jsonb,$9::jsonb,$10)
		RETURNING `+rootColumns,
		mutation.Name, mutation.Path, mutation.NormalizedPath, mutation.Mode,
		mutation.Enabled, mutation.ScanOnStartup, mutation.ScanIntervalMinutes,
		includePatterns, excludePatterns, mutation.Status,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return RootView{}, apperror.Conflict(apperror.CodeResourceConflict, "Music source path already exists", nil)
		}
		return RootView{}, fmt.Errorf("create music source: %w", err)
	}
	if err := writeAudit(ctx, transaction, actorID, "admin.library-root.create", root.ID, traceID, map[string]any{
		"path": mutation.Path, "mode": mutation.Mode,
	}); err != nil {
		return RootView{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RootView{}, fmt.Errorf("commit music source creation: %w", err)
	}
	return repository.FindRootView(ctx, root.ID)
}

func (repository *Repository) UpdateRoot(ctx context.Context, command UpdateRootCommand) (RootView, error) {
	includePatterns, excludePatterns, err := encodePatterns(command.Mutation)
	if err != nil {
		return RootView{}, err
	}
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return RootView{}, fmt.Errorf("begin music source update: %w", err)
	}
	defer transaction.Rollback(ctx)
	locked, err := scanRoot(transaction.QueryRow(ctx,
		`SELECT `+rootColumns+` FROM library_roots WHERE id=$1 FOR UPDATE`, command.RootID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RootView{}, apperror.NotFound("Music source was not found")
	}
	if err != nil {
		return RootView{}, fmt.Errorf("lock music source update: %w", err)
	}
	if locked.Version != command.ExpectedVersion {
		return RootView{}, versionConflict(command.ExpectedVersion, locked.Version)
	}
	if locked.NormalizedPath != command.Mutation.NormalizedPath {
		var writeback bool
		if err := transaction.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM metadata_writeback_jobs job
			JOIN local_music_sources source ON source.id=job.source_id
			WHERE source.root_id=$1
			  AND job.status IN ('PENDING','PROCESSING')
		)`, command.RootID).Scan(&writeback); err != nil {
			return RootView{}, fmt.Errorf("check music source writeback before path update: %w", err)
		}
		if writeback {
			return RootView{}, apperror.Conflict(
				apperror.CodeInvalidStateTransition,
				"Complete or cancel active Tag writeback before changing this source path", nil,
			)
		}
	}
	var activeID, activeStatus string
	err = transaction.QueryRow(ctx, `
		SELECT id,status::text FROM library_scan_runs
		WHERE root_id=$1 AND status IN ('PENDING','RUNNING') LIMIT 1 FOR UPDATE`, command.RootID,
	).Scan(&activeID, &activeStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return RootView{}, fmt.Errorf("check active music source scan: %w", err)
	}
	active := err == nil
	if active && command.Mutation.Enabled {
		return RootView{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Cancel the active scan before modifying this source", nil,
		)
	}
	now := time.Now().UTC()
	if active {
		if activeStatus == string(ScanStatusPending) {
			_, err = transaction.Exec(ctx, `UPDATE library_scan_runs SET
				cancel_requested=true,status='CANCELLED',completed_at=$2,
				locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,updated_at=$2 WHERE id=$1`, activeID, now)
		} else {
			_, err = transaction.Exec(ctx,
				`UPDATE library_scan_runs SET cancel_requested=true,updated_at=$2 WHERE id=$1`, activeID, now)
		}
		if err != nil {
			return RootView{}, fmt.Errorf("cancel active music source scan for update: %w", err)
		}
	}
	status := locked.Status
	if !command.Mutation.Enabled {
		status = RootStatusDisabled
	} else if locked.Status == RootStatusDisabled {
		status = RootStatusUnknown
	}
	root, err := scanRoot(transaction.QueryRow(ctx, `
		UPDATE library_roots SET
			name=$2,path=$3,normalized_path=$4,mode=$5,tag_writeback_enabled=false,
			enabled=$6,scan_on_startup=$7,scan_interval_minutes=$8,
			include_patterns=$9::jsonb,exclude_patterns=$10::jsonb,status=$11,
			version=version+1,updated_at=$12
		WHERE id=$1 AND version=$13 RETURNING `+rootColumns,
		command.RootID, command.Mutation.Name, command.Mutation.Path, command.Mutation.NormalizedPath,
		command.Mutation.Mode, command.Mutation.Enabled, command.Mutation.ScanOnStartup,
		command.Mutation.ScanIntervalMinutes, includePatterns, excludePatterns, status, now,
		command.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return RootView{}, versionConflict(command.ExpectedVersion, locked.Version)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return RootView{}, apperror.Conflict(apperror.CodeResourceConflict, "Music source path already exists", nil)
		}
		return RootView{}, fmt.Errorf("update music source: %w", err)
	}
	if err := writeAudit(ctx, transaction, command.ActorID, "admin.library-root.update", root.ID, command.TraceID,
		map[string]any{"fields": command.ChangedFields}); err != nil {
		return RootView{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RootView{}, fmt.Errorf("commit music source update: %w", err)
	}
	return repository.FindRootView(ctx, root.ID)
}

func (repository *Repository) DeleteRoot(ctx context.Context, command DeleteRootCommand) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin music source deletion: %w", err)
	}
	defer transaction.Rollback(ctx)
	root, err := scanRoot(transaction.QueryRow(ctx,
		`SELECT `+rootColumns+` FROM library_roots WHERE id=$1 FOR UPDATE`, command.RootID))
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Music source was not found")
	}
	if err != nil {
		return fmt.Errorf("lock music source deletion: %w", err)
	}
	if root.Version != command.ExpectedVersion {
		return versionConflict(command.ExpectedVersion, root.Version)
	}
	var active bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM library_scan_runs WHERE root_id=$1 AND status IN ('PENDING','RUNNING'))`, command.RootID).Scan(&active); err != nil {
		return fmt.Errorf("check active music source scan for deletion: %w", err)
	}
	if active {
		return apperror.Conflict(apperror.CodeInvalidStateTransition, "Cancel the active scan before deleting this source", nil)
	}
	var writeback bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM metadata_writeback_jobs job
		JOIN local_music_sources source ON source.id=job.source_id
		WHERE source.root_id=$1 AND job.status IN ('PENDING','PROCESSING')
	)`, command.RootID).Scan(&writeback); err != nil {
		return fmt.Errorf("check active music source writeback: %w", err)
	}
	if writeback {
		return apperror.Conflict(apperror.CodeInvalidStateTransition,
			"Complete or cancel active Tag writeback before deleting this source", nil)
	}
	if command.ArchiveCatalog {
		if _, err := transaction.Exec(ctx, `UPDATE tracks SET
			status='ARCHIVED',version=version+1,updated_at=now()
			WHERE id IN (
				SELECT mapping.track_id FROM local_music_source_tracks mapping
				JOIN local_music_sources source ON source.id=mapping.source_id WHERE source.root_id=$1
			)`, command.RootID); err != nil {
			return fmt.Errorf("archive deleted music source catalog: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM library_roots WHERE id=$1`, command.RootID); err != nil {
		return fmt.Errorf("delete music source: %w", err)
	}
	if err := writeAudit(ctx, transaction, command.ActorID, "admin.library-root.delete", command.RootID,
		command.TraceID, map[string]any{"archiveCatalog": command.ArchiveCatalog}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit music source deletion: %w", err)
	}
	return nil
}

func (repository *Repository) ListFiles(
	ctx context.Context,
	rootID string,
	query FileQuery,
) ([]SourceFile, int, error) {
	if _, err := repository.FindRoot(ctx, rootID); err != nil {
		return nil, 0, err
	}
	offset, err := pagination.ParseOffset(query.Page, query.PageSize, 25)
	if err != nil {
		return nil, 0, err
	}
	conditions := []string{"source.root_id=$1"}
	arguments := []any{rootID}
	if strings.TrimSpace(query.Query) != "" {
		arguments = append(arguments, "%"+strings.TrimSpace(query.Query)+"%")
		conditions = append(conditions, fmt.Sprintf("source.source_path ILIKE $%d", len(arguments)))
	}
	if query.Status != "" {
		arguments = append(arguments, query.Status)
		conditions = append(conditions, fmt.Sprintf("source.status=$%d", len(arguments)))
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := repository.database.QueryRow(ctx,
		`SELECT count(*)::int FROM local_music_sources source WHERE `+where, arguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count music source files: %w", err)
	}
	arguments = append(arguments, offset.PageSize, offset.Offset)
	rows, err := repository.database.Query(ctx, `
		SELECT source.id,source.source_path,source.status,source.last_error,
			source.size_bytes,source.modified_at,track.id,track.title,track.status::text,
			(SELECT count(*)::int FROM local_music_source_tracks count_mapping
			 WHERE count_mapping.source_id=source.id) AS track_count,
			EXISTS(SELECT 1 FROM local_music_source_tracks cue_mapping
			 WHERE cue_mapping.source_id=source.id AND cue_mapping.cue_path IS NOT NULL) AS cue
		FROM local_music_sources source
		JOIN local_music_source_tracks mapping ON mapping.source_id=source.id AND mapping.segment_index=0
		JOIN tracks track ON track.id=mapping.track_id
		WHERE `+where+`
		ORDER BY source.source_path ASC LIMIT $`+fmt.Sprint(len(arguments)-1)+` OFFSET $`+fmt.Sprint(len(arguments)), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("list music source files: %w", err)
	}
	defer rows.Close()
	files := make([]SourceFile, 0)
	for rows.Next() {
		var file SourceFile
		if err := rows.Scan(
			&file.ID, &file.Path, &file.Status, &file.LastError, &file.SizeBytes, &file.ModifiedAt,
			&file.TrackID, &file.TrackTitle, &file.TrackStatus, &file.TrackCount, &file.Cue,
		); err != nil {
			return nil, 0, fmt.Errorf("scan music source file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate music source files: %w", err)
	}
	return files, total, nil
}

func (repository *Repository) ProcessingSummary(ctx context.Context, rootID string) (ProcessingSummary, error) {
	if _, err := repository.FindRoot(ctx, rootID); err != nil {
		return ProcessingSummary{}, err
	}
	result := ProcessingSummary{Jobs: []ProcessingJob{}}
	rows, err := repository.database.Query(ctx, `
		SELECT job.status::text,count(*)::int FROM media_jobs job
		WHERE job.scan_run_id=(
			SELECT run.id FROM library_scan_runs run WHERE run.root_id=$1
			ORDER BY run.created_at DESC,run.id DESC LIMIT 1
		) GROUP BY job.status`, rootID)
	if err != nil {
		return ProcessingSummary{}, fmt.Errorf("summarize music source processing: %w", err)
	}
	for rows.Next() {
		var status string
		var total int
		if err := rows.Scan(&status, &total); err != nil {
			rows.Close()
			return ProcessingSummary{}, fmt.Errorf("scan music source processing summary: %w", err)
		}
		switch status {
		case "PENDING":
			result.Queued = total
		case "PROCESSING":
			result.Processing = total
		case "READY":
			result.Completed = total
		case "FAILED":
			result.Failed = total
		case "CANCELLED":
			result.Cancelled = total
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ProcessingSummary{}, fmt.Errorf("iterate music source processing summary: %w", err)
	}
	rows.Close()
	jobRows, err := repository.database.Query(ctx, `
		SELECT job.id,job.status::text,track.title,job.attempts,job.max_attempts,
			job.last_error,job.last_error_code,job.created_at,job.updated_at
		FROM media_jobs job JOIN tracks track ON track.id=job.track_id
		WHERE job.scan_run_id=(
			SELECT run.id FROM library_scan_runs run WHERE run.root_id=$1
			ORDER BY run.created_at DESC,run.id DESC LIMIT 1
		) ORDER BY job.updated_at DESC,job.id DESC LIMIT 12`, rootID)
	if err != nil {
		return ProcessingSummary{}, fmt.Errorf("list recent music source processing jobs: %w", err)
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var job ProcessingJob
		if err := jobRows.Scan(
			&job.ID, &job.Status, &job.Title, &job.Attempts, &job.MaxAttempts,
			&job.LastError, &job.LastErrorCode, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return ProcessingSummary{}, fmt.Errorf("scan recent music source processing job: %w", err)
		}
		result.Jobs = append(result.Jobs, job)
	}
	if err := jobRows.Err(); err != nil {
		return ProcessingSummary{}, fmt.Errorf("iterate recent music source processing jobs: %w", err)
	}
	if len(result.Jobs) > 0 {
		value := result.Jobs[0].UpdatedAt
		result.UpdatedAt = &value
	}
	return result, nil
}

func (repository *Repository) ListRuns(
	ctx context.Context,
	rootID string,
	query PageQuery,
) ([]ScanRun, int, error) {
	if _, err := repository.FindRoot(ctx, rootID); err != nil {
		return nil, 0, err
	}
	offset, err := pagination.ParseOffset(query.Page, query.PageSize, 25)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := repository.database.QueryRow(ctx,
		`SELECT count(*)::int FROM library_scan_runs WHERE root_id=$1`, rootID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count music source scans: %w", err)
	}
	rows, err := repository.database.Query(ctx, `SELECT `+scanRunColumns+`
		FROM library_scan_runs WHERE root_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`,
		rootID, offset.PageSize, offset.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list music source scans: %w", err)
	}
	defer rows.Close()
	runs := make([]ScanRun, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan music source scan: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate music source scans: %w", err)
	}
	return runs, total, nil
}

func (repository *Repository) FindRun(ctx context.Context, rootID, runID string) (ScanRun, error) {
	if _, err := repository.FindRoot(ctx, rootID); err != nil {
		return ScanRun{}, err
	}
	run, err := scanRun(repository.database.QueryRow(ctx, `SELECT `+scanRunColumns+`
		FROM library_scan_runs WHERE id=$1 AND root_id=$2`, runID, rootID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ScanRun{}, apperror.NotFound("Library scan was not found")
	}
	if err != nil {
		return ScanRun{}, fmt.Errorf("find music source scan: %w", err)
	}
	return run, nil
}

func (repository *Repository) EnqueueScan(ctx context.Context, command EnqueueScanCommand) (ScanRun, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return ScanRun{}, fmt.Errorf("begin music source scan enqueue: %w", err)
	}
	defer transaction.Rollback(ctx)
	root, err := scanRoot(transaction.QueryRow(ctx,
		`SELECT `+rootColumns+` FROM library_roots WHERE id=$1 FOR UPDATE`, command.RootID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ScanRun{}, apperror.NotFound("Music source was not found")
	}
	if err != nil {
		return ScanRun{}, fmt.Errorf("lock music source scan enqueue: %w", err)
	}
	if !root.Enabled {
		return ScanRun{}, apperror.Conflict(apperror.CodeInvalidStateTransition, "Disabled sources cannot be scanned", nil)
	}
	run, err := scanRun(transaction.QueryRow(ctx, `INSERT INTO library_scan_runs(root_id,root_version,triggered_by)
		VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING `+scanRunColumns, command.RootID, root.Version, command.ActorID))
	if errors.Is(err, pgx.ErrNoRows) {
		if command.Deduplicate {
			active, lookupErr := scanRun(transaction.QueryRow(ctx, `SELECT `+scanRunColumns+`
				FROM library_scan_runs WHERE root_id=$1 AND status IN ('PENDING','RUNNING') LIMIT 1`, command.RootID))
			if lookupErr == nil {
				if err := transaction.Commit(ctx); err != nil {
					return ScanRun{}, fmt.Errorf("commit deduplicated music source scan enqueue: %w", err)
				}
				return active, nil
			}
			if !errors.Is(lookupErr, pgx.ErrNoRows) {
				return ScanRun{}, fmt.Errorf("find deduplicated music source scan: %w", lookupErr)
			}
		}
		return ScanRun{}, apperror.Conflict(apperror.CodeResourceConflict, "A scan is already queued for this source", nil)
	}
	if err != nil {
		return ScanRun{}, fmt.Errorf("enqueue music source scan: %w", err)
	}
	if command.ActorID != nil && command.TraceID != "" {
		if err := writeAudit(ctx, transaction, *command.ActorID, "admin.library-root.scan", command.RootID,
			command.TraceID, map[string]any{"runId": run.ID}); err != nil {
			return ScanRun{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return ScanRun{}, fmt.Errorf("commit music source scan enqueue: %w", err)
	}
	return run, nil
}

func (repository *Repository) CancelScan(ctx context.Context, command CancelScanCommand) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin music source scan cancellation: %w", err)
	}
	defer transaction.Rollback(ctx)
	run, err := scanRun(transaction.QueryRow(ctx, `SELECT `+scanRunColumns+`
		FROM library_scan_runs WHERE id=$1 AND root_id=$2 FOR UPDATE`, command.RunID, command.RootID))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && run.Status != ScanStatusPending && run.Status != ScanStatusRunning) {
		return apperror.NotFound("Active library scan was not found")
	}
	if err != nil {
		return fmt.Errorf("lock music source scan cancellation: %w", err)
	}
	now := time.Now().UTC()
	if run.Status == ScanStatusPending {
		if _, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET
			cancel_requested=true,status='CANCELLED',completed_at=$2,locked_by=NULL,
			locked_until=NULL,heartbeat_at=NULL,updated_at=$2 WHERE id=$1`, run.ID, now); err != nil {
			return fmt.Errorf("cancel pending music source scan: %w", err)
		}
		if _, err := transaction.Exec(ctx, `UPDATE library_roots SET
			status='READY',last_error=NULL,updated_at=$3 WHERE id=$1 AND version=$2`,
			command.RootID, run.RootVersion, now); err != nil {
			return fmt.Errorf("restore pending scan music source state: %w", err)
		}
	} else if _, err := transaction.Exec(ctx,
		`UPDATE library_scan_runs SET cancel_requested=true,updated_at=$2 WHERE id=$1`, run.ID, now); err != nil {
		return fmt.Errorf("request running music source scan cancellation: %w", err)
	}
	if command.ActorID != nil && command.TraceID != "" {
		if err := writeAudit(ctx, transaction, *command.ActorID, "admin.library-root.scan.cancel", command.RootID,
			command.TraceID, map[string]any{"runId": command.RunID}); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit music source scan cancellation: %w", err)
	}
	return nil
}

func (repository *Repository) InitializeScans(ctx context.Context, now time.Time) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin music source scan recovery: %w", err)
	}
	defer transaction.Rollback(ctx)
	rows, err := transaction.Query(ctx, `UPDATE library_scan_runs SET
		status='FAILED',completed_at=$1,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		last_error='The scan worker lease expired before completion',updated_at=$1
		WHERE status='RUNNING' AND (locked_until IS NULL OR locked_until<$1) RETURNING root_id`, now)
	if err != nil {
		return fmt.Errorf("recover expired music source scans: %w", err)
	}
	rootIDs := make([]string, 0)
	for rows.Next() {
		var rootID string
		if err := rows.Scan(&rootID); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired music source root: %w", err)
		}
		rootIDs = append(rootIDs, rootID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired music source roots: %w", err)
	}
	rows.Close()
	if len(rootIDs) > 0 {
		if _, err := transaction.Exec(ctx, `UPDATE library_roots root SET
			status='ERROR',last_error='The previous scan stopped before completion',updated_at=$2
			WHERE root.id=ANY($1::uuid[]) AND root.status='SCANNING' AND NOT EXISTS(
				SELECT 1 FROM library_scan_runs run WHERE run.root_id=root.id
				AND run.status IN ('PENDING','RUNNING'))`, rootIDs, now); err != nil {
			return fmt.Errorf("mark expired scan music sources failed: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit music source scan recovery: %w", err)
	}
	return nil
}

func (repository *Repository) EnsureDefaultRoot(ctx context.Context, mutation RootMutation) (Root, error) {
	includePatterns, excludePatterns, err := encodePatterns(mutation)
	if err != nil {
		return Root{}, err
	}
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return Root{}, fmt.Errorf("begin default music source initialization: %w", err)
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"library-default-root"); err != nil {
		return Root{}, fmt.Errorf("lock default music source initialization: %w", err)
	}
	// The local-library settings describe the configured default root. Match by
	// path instead of reusing an unrelated first root when more roots exist.
	root, err := scanRoot(transaction.QueryRow(ctx,
		`SELECT `+rootColumns+` FROM library_roots WHERE normalized_path=$1 FOR UPDATE`, mutation.NormalizedPath))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Root{}, fmt.Errorf("find existing default music source: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		root, err = scanRoot(transaction.QueryRow(ctx, `INSERT INTO library_roots(
			name,path,normalized_path,mode,tag_writeback_enabled,enabled,scan_on_startup,
			scan_interval_minutes,include_patterns,exclude_patterns,status
		) VALUES($1,$2,$3,$4,false,$5,$6,$7,$8::jsonb,$9::jsonb,$10)
		ON CONFLICT(normalized_path) DO UPDATE SET normalized_path=EXCLUDED.normalized_path
		RETURNING `+rootColumns,
			mutation.Name, mutation.Path, mutation.NormalizedPath, mutation.Mode,
			mutation.Enabled, mutation.ScanOnStartup, mutation.ScanIntervalMinutes,
			includePatterns, excludePatterns, mutation.Status,
		))
		if err != nil {
			return Root{}, fmt.Errorf("create default music source: %w", err)
		}
		if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET root_id=$1 WHERE root_id IS NULL`, root.ID); err != nil {
			return Root{}, fmt.Errorf("attach legacy files to default music source: %w", err)
		}
	} else {
		updated, updateErr := scanRoot(transaction.QueryRow(ctx, `
			UPDATE library_roots SET
				name=$2,path=$3,mode=$4,enabled=$5,scan_on_startup=$6,
				scan_interval_minutes=$7,include_patterns=$8::jsonb,exclude_patterns=$9::jsonb,
				status=CASE
					WHEN NOT $5 THEN 'DISABLED'::library_root_status
					WHEN enabled=false AND $5 THEN 'UNKNOWN'::library_root_status
					ELSE status
				END,
				version=version+1,updated_at=now()
			WHERE id=$1 AND (
				name IS DISTINCT FROM $2 OR path IS DISTINCT FROM $3 OR mode IS DISTINCT FROM $4::library_root_mode OR
				enabled IS DISTINCT FROM $5 OR scan_on_startup IS DISTINCT FROM $6 OR
				scan_interval_minutes IS DISTINCT FROM $7 OR include_patterns IS DISTINCT FROM $8::jsonb OR
				exclude_patterns IS DISTINCT FROM $9::jsonb
			)
			RETURNING `+rootColumns,
			root.ID, mutation.Name, mutation.Path, mutation.Mode, mutation.Enabled, mutation.ScanOnStartup,
			mutation.ScanIntervalMinutes, includePatterns, excludePatterns,
		))
		if updateErr == nil {
			root = updated
		} else if !errors.Is(updateErr, pgx.ErrNoRows) {
			return Root{}, fmt.Errorf("synchronize configured default music source: %w", updateErr)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return Root{}, fmt.Errorf("commit default music source initialization: %w", err)
	}
	return root, nil
}

func (repository *Repository) StartupRootIDs(ctx context.Context) ([]string, error) {
	rows, err := repository.database.Query(ctx,
		`SELECT id FROM library_roots WHERE enabled=true AND scan_on_startup=true ORDER BY name,id`)
	if err != nil {
		return nil, fmt.Errorf("list startup music sources: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan startup music source: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate startup music sources: %w", err)
	}
	return ids, nil
}

func (repository *Repository) EnqueueScheduledScans(ctx context.Context, now time.Time) error {
	_, err := repository.database.Exec(ctx, `INSERT INTO library_scan_runs(root_id,root_version)
		SELECT root.id,root.version FROM library_roots root
		WHERE root.enabled=true AND root.scan_interval_minutes IS NOT NULL
		AND (root.last_scan_at IS NULL OR root.last_scan_at + make_interval(mins=>root.scan_interval_minutes) <= $1)
		AND NOT EXISTS(SELECT 1 FROM library_scan_runs active
			WHERE active.root_id=root.id AND active.status IN ('PENDING','RUNNING'))
		ON CONFLICT DO NOTHING`, now)
	if err != nil {
		return fmt.Errorf("enqueue scheduled music source scans: %w", err)
	}
	return nil
}

func (repository *Repository) ClaimNextScan(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (*ClaimedScan, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin music source scan claim: %w", err)
	}
	defer transaction.Rollback(ctx)
	run, err := scanRun(transaction.QueryRow(ctx, `SELECT `+scanRunColumns+`
		FROM library_scan_runs WHERE status='PENDING' OR (status='RUNNING' AND locked_until<$1)
		ORDER BY created_at ASC,id ASC FOR UPDATE SKIP LOCKED LIMIT 1`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty music source scan claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select music source scan claim: %w", err)
	}
	root, err := scanRoot(transaction.QueryRow(ctx,
		`SELECT `+rootColumns+` FROM library_roots WHERE id=$1 FOR UPDATE`, run.RootID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lock claimed music source: %w", err)
	}
	invalid := errors.Is(err, pgx.ErrNoRows) || !root.Enabled || root.Version != run.RootVersion || run.CancelRequested
	if invalid {
		var lastError *string
		if err == nil && !root.Enabled {
			value := "Music source was disabled"
			lastError = &value
		}
		run, err = scanRun(transaction.QueryRow(ctx, `UPDATE library_scan_runs SET
			status='CANCELLED',cancel_requested=true,completed_at=$2,locked_by=NULL,
			locked_until=NULL,heartbeat_at=NULL,last_error=$3,updated_at=$2 WHERE id=$1
			RETURNING `+scanRunColumns, run.ID, now, lastError))
		if err != nil {
			return nil, fmt.Errorf("cancel invalid music source scan claim: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit invalid music source scan claim: %w", err)
		}
		return &ClaimedScan{Run: run, Root: root}, nil
	}
	attemptID := uuid.NewString()
	lockedUntil := now.Add(lease)
	run, err = scanRun(transaction.QueryRow(ctx, `UPDATE library_scan_runs SET
		status='RUNNING',attempt_id=$2,locked_by=$3,locked_until=$4,heartbeat_at=$5,
		started_at=COALESCE(started_at,$5),completed_at=NULL,last_error=NULL,updated_at=$5
		WHERE id=$1 RETURNING `+scanRunColumns, run.ID, attemptID, workerID, lockedUntil, now))
	if err != nil {
		return nil, fmt.Errorf("claim music source scan: %w", err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE library_roots SET
		status='SCANNING',last_error=NULL,updated_at=$4
		WHERE id=$1 AND version=$2 AND enabled=$3`, root.ID, run.RootVersion, true, now); err != nil {
		return nil, fmt.Errorf("mark claimed music source scanning: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit music source scan claim: %w", err)
	}
	return &ClaimedScan{Run: run, Root: root}, nil
}

func (repository *Repository) HeartbeatScan(
	ctx context.Context,
	runID, attemptID, workerID string,
	now time.Time,
	lease time.Duration,
) (bool, error) {
	command, err := repository.database.Exec(ctx, `UPDATE library_scan_runs SET
		heartbeat_at=$4,locked_until=$5
		WHERE id=$1 AND status='RUNNING' AND attempt_id=$2 AND locked_by=$3 AND cancel_requested=false`,
		runID, attemptID, workerID, now, now.Add(lease))
	if err != nil {
		return false, fmt.Errorf("heartbeat music source scan: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (repository *Repository) ScanControl(
	ctx context.Context,
	runID, attemptID, workerID string,
) (bool, bool, error) {
	var status ScanStatus
	var lockedBy *string
	var cancelRequested bool
	err := repository.database.QueryRow(ctx, `SELECT status,locked_by,cancel_requested
		FROM library_scan_runs WHERE id=$1 AND attempt_id=$2`, runID, attemptID).Scan(&status, &lockedBy, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("read music source scan control: %w", err)
	}
	owned := status == ScanStatusRunning && lockedBy != nil && *lockedBy == workerID
	return cancelRequested || !owned, owned, nil
}

func (repository *Repository) UpdateScanProgress(
	ctx context.Context,
	runID, attemptID, workerID string,
	progress ScanProgress,
	now time.Time,
) (bool, error) {
	command, err := repository.database.Exec(ctx, `UPDATE library_scan_runs SET
		discovered_files=$4,processed_files=$5,failed_files=$6,updated_at=$7
		WHERE id=$1 AND status='RUNNING' AND attempt_id=$2 AND locked_by=$3`,
		runID, attemptID, workerID, progress.DiscoveredFiles, progress.ProcessedFiles, progress.FailedFiles, now)
	if err != nil {
		return false, fmt.Errorf("update music source scan progress: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (repository *Repository) CompleteScan(
	ctx context.Context,
	claim ClaimedScan,
	attemptID, workerID string,
	result ScanResult,
	now time.Time,
) (bool, error) {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin music source scan completion: %w", err)
	}
	defer transaction.Rollback(ctx)
	command, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET
		discovered_files=$4,processed_files=$5,failed_files=$6,status='COMPLETED',
		completed_at=$7,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,updated_at=$7
		WHERE id=$1 AND status='RUNNING' AND attempt_id=$2 AND locked_by=$3`,
		claim.Run.ID, attemptID, workerID, result.DiscoveredFiles, result.ProcessedFiles, result.FailedFiles, now)
	if err != nil {
		return false, fmt.Errorf("complete music source scan: %w", err)
	}
	if command.RowsAffected() != 1 {
		if err := transaction.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit lost music source scan completion: %w", err)
		}
		return false, nil
	}
	if _, err := transaction.Exec(ctx, `UPDATE library_roots SET
		status='READY',last_scan_at=$3,last_error=NULL,updated_at=$3
		WHERE id=$1 AND version=$2 AND enabled=true`, claim.Root.ID, claim.Run.RootVersion, now); err != nil {
		return false, fmt.Errorf("mark completed music source ready: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit music source scan completion: %w", err)
	}
	return true, nil
}

func (repository *Repository) FinalizeScanFailure(
	ctx context.Context,
	claim ClaimedScan,
	attemptID, workerID string,
	status ScanStatus,
	lastError *string,
	now time.Time,
) (bool, error) {
	if status != ScanStatusPending && status != ScanStatusCancelled && status != ScanStatusFailed {
		return false, errors.New("music source scan final status is invalid")
	}
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin music source scan failure finalization: %w", err)
	}
	defer transaction.Rollback(ctx)
	command, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET
		status=$4,completed_at=CASE WHEN $4='PENDING' THEN NULL ELSE $5 END,
		locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,last_error=$6,updated_at=$5
		WHERE id=$1 AND status='RUNNING' AND attempt_id=$2 AND locked_by=$3`,
		claim.Run.ID, attemptID, workerID, status, now, lastError)
	if err != nil {
		return false, fmt.Errorf("finalize music source scan failure: %w", err)
	}
	if command.RowsAffected() != 1 {
		if err := transaction.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit lost music source scan failure: %w", err)
		}
		return false, nil
	}
	rootStatus := RootStatusError
	if status == ScanStatusPending {
		rootStatus = RootStatusUnknown
	} else if status == ScanStatusCancelled {
		rootStatus = RootStatusReady
	}
	if status == ScanStatusPending {
		_, err = transaction.Exec(ctx, `UPDATE library_roots SET status=$3,last_error=NULL,updated_at=$4
			WHERE id=$1 AND version=$2`, claim.Root.ID, claim.Run.RootVersion, rootStatus, now)
	} else {
		_, err = transaction.Exec(ctx, `UPDATE library_roots SET status=$3,last_scan_at=$4,last_error=$5,updated_at=$4
			WHERE id=$1 AND version=$2`, claim.Root.ID, claim.Run.RootVersion, rootStatus, now, lastError)
	}
	if err != nil {
		return false, fmt.Errorf("finalize failed music source state: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit music source scan failure: %w", err)
	}
	return true, nil
}

func listRoots(ctx context.Context, database repositoryDatabase, query RootQuery) ([]Root, int, error) {
	rows, err := database.Query(ctx, `SELECT `+rootColumns+`
		FROM library_roots ORDER BY name ASC,id ASC LIMIT $1 OFFSET $2`, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list music sources: %w", err)
	}
	roots := make([]Root, 0)
	for rows.Next() {
		root, err := scanRoot(rows)
		if err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan music source: %w", err)
		}
		roots = append(roots, root)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("iterate music sources: %w", err)
	}
	rows.Close()
	var total int
	if err := database.QueryRow(ctx, `SELECT count(*)::int FROM library_roots`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count music sources: %w", err)
	}
	return roots, total, nil
}

func (repository *Repository) rootCounts(ctx context.Context, rootIDs []string) (map[string]RootCounts, error) {
	result := make(map[string]RootCounts)
	if len(rootIDs) == 0 {
		return result, nil
	}
	rows, err := repository.database.Query(ctx, `SELECT root_id,
		count(*)::int,count(*) FILTER(WHERE status='FAILED')::int
		FROM local_music_sources WHERE root_id = ANY($1::uuid[]) GROUP BY root_id`, rootIDs)
	if err != nil {
		return nil, fmt.Errorf("count music source files: %w", err)
	}
	for rows.Next() {
		var rootID string
		var counts RootCounts
		if err := rows.Scan(&rootID, &counts.FileCount, &counts.FailedFileCount); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan music source file counts: %w", err)
		}
		result[rootID] = counts
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate music source file counts: %w", err)
	}
	rows.Close()
	rows, err = repository.database.Query(ctx, `SELECT source.root_id,
		count(*)::int,count(DISTINCT mapping.source_id) FILTER(WHERE mapping.cue_path IS NOT NULL)::int
		FROM local_music_source_tracks mapping JOIN local_music_sources source ON source.id=mapping.source_id
		WHERE source.root_id = ANY($1::uuid[]) GROUP BY source.root_id`, rootIDs)
	if err != nil {
		return nil, fmt.Errorf("count music source mappings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rootID string
		var tracks, cues int
		if err := rows.Scan(&rootID, &tracks, &cues); err != nil {
			return nil, fmt.Errorf("scan music source mapping counts: %w", err)
		}
		counts := result[rootID]
		counts.TrackCount, counts.CueFileCount = tracks, cues
		result[rootID] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate music source mapping counts: %w", err)
	}
	return result, nil
}

func rootCount(ctx context.Context, database repositoryDatabase, rootID string) (RootCounts, error) {
	var counts RootCounts
	err := database.QueryRow(ctx, `SELECT
		(SELECT count(*)::int FROM local_music_sources WHERE root_id=$1),
		(SELECT count(*)::int FROM local_music_sources WHERE root_id=$1 AND status='FAILED'),
		(SELECT count(*)::int FROM local_music_source_tracks mapping
		 JOIN local_music_sources source ON source.id=mapping.source_id WHERE source.root_id=$1),
		(SELECT count(DISTINCT mapping.source_id)::int FROM local_music_source_tracks mapping
		 JOIN local_music_sources source ON source.id=mapping.source_id
		 WHERE source.root_id=$1 AND mapping.cue_path IS NOT NULL)`, rootID).Scan(
		&counts.FileCount, &counts.FailedFileCount, &counts.TrackCount, &counts.CueFileCount,
	)
	if err != nil {
		return RootCounts{}, fmt.Errorf("count music source contents: %w", err)
	}
	return counts, nil
}

func (repository *Repository) latestRuns(ctx context.Context, rootIDs []string) (map[string]ScanRun, error) {
	result := make(map[string]ScanRun)
	if len(rootIDs) == 0 {
		return result, nil
	}
	rows, err := repository.database.Query(ctx, `SELECT DISTINCT ON(root_id) `+scanRunColumns+`
		FROM library_scan_runs WHERE root_id = ANY($1::uuid[])
		ORDER BY root_id,created_at DESC,id DESC`, rootIDs)
	if err != nil {
		return nil, fmt.Errorf("list latest music source scans: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest music source scan: %w", err)
		}
		result[run.RootID] = run
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest music source scans: %w", err)
	}
	return result, nil
}

type rowScanner interface{ Scan(...any) error }

func scanRoot(scanner rowScanner) (Root, error) {
	var root Root
	var includePatterns, excludePatterns []byte
	err := scanner.Scan(
		&root.ID, &root.Name, &root.Path, &root.NormalizedPath, &root.Mode,
		&root.TagWritebackEnabled, &root.Enabled, &root.ScanOnStartup,
		&root.ScanIntervalMinutes, &includePatterns, &excludePatterns, &root.Status,
		&root.LastScanAt, &root.LastError, &root.Version, &root.CreatedAt, &root.UpdatedAt,
	)
	if err != nil {
		return Root{}, err
	}
	if err := json.Unmarshal(includePatterns, &root.IncludePatterns); err != nil {
		return Root{}, fmt.Errorf("decode music source include patterns: %w", err)
	}
	if err := json.Unmarshal(excludePatterns, &root.ExcludePatterns); err != nil {
		return Root{}, fmt.Errorf("decode music source exclude patterns: %w", err)
	}
	return root, nil
}

func scanRun(scanner rowScanner) (ScanRun, error) {
	var run ScanRun
	err := scanner.Scan(
		&run.ID, &run.RootID, &run.RootVersion, &run.TriggeredBy, &run.Status,
		&run.DiscoveredFiles, &run.ProcessedFiles, &run.FailedFiles,
		&run.CancelRequested, &run.AttemptID, &run.LockedBy, &run.LockedUntil,
		&run.HeartbeatAt, &run.StartedAt, &run.CompletedAt, &run.LastError,
		&run.CreatedAt, &run.UpdatedAt,
	)
	return run, err
}

func encodePatterns(mutation RootMutation) (string, string, error) {
	includePatterns, err := json.Marshal(nonNilStrings(mutation.IncludePatterns))
	if err != nil {
		return "", "", fmt.Errorf("encode music source include patterns: %w", err)
	}
	excludePatterns, err := json.Marshal(nonNilStrings(mutation.ExcludePatterns))
	if err != nil {
		return "", "", fmt.Errorf("encode music source exclude patterns: %w", err)
	}
	return string(includePatterns), string(excludePatterns), nil
}

func writeAudit(
	ctx context.Context,
	executor interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	actorID, action, targetID, traceID string,
	details map[string]any,
) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode music source audit details: %w", err)
	}
	_, err = executor.Exec(ctx, `INSERT INTO audit_logs(
		actor_id,action,target_type,target_id,result,trace_id,details
	) VALUES($1,$2,'library_root',$3,'SUCCESS',$4,$5::jsonb)`, actorID, action, targetID, traceID, encoded)
	if err != nil {
		return fmt.Errorf("write music source audit: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

const rootColumns = `
	id,name,path,normalized_path,mode,tag_writeback_enabled,enabled,scan_on_startup,
	scan_interval_minutes,include_patterns,exclude_patterns,status,last_scan_at,last_error,
	version,created_at,updated_at`

const scanRunColumns = `
	id,root_id,root_version,triggered_by,status,discovered_files,processed_files,failed_files,
	cancel_requested,attempt_id,locked_by,locked_until,heartbeat_at,started_at,completed_at,
	last_error,created_at,updated_at`
