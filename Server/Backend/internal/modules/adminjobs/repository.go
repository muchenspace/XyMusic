package adminjobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

type Repository struct {
	database repositoryDatabase
}

type repositoryDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{database: pool}
}

func (repository *Repository) ListJobs(ctx context.Context, input ListQuery) ([]JobRecord, int, error) {
	where, arguments := jobWhere(input)
	countWhere := where
	countArguments := append([]any(nil), arguments...)
	orderColumn := map[SortField]string{
		SortCreatedAt: "created_at",
		SortUpdatedAt: "updated_at",
		SortStatus:    "status",
		SortType:      "type",
		SortTitle:     "title",
	}[input.Sort]
	if orderColumn == "" {
		return nil, 0, fmt.Errorf("unsupported administrator job sort %q", input.Sort)
	}
	direction := "DESC"
	if input.Order == SortAscending {
		direction = "ASC"
	} else if input.Order != SortDescending {
		return nil, 0, fmt.Errorf("unsupported administrator job order %q", input.Order)
	}
	if input.CursorMode && input.After != nil {
		operator := "<"
		if input.Order == SortAscending {
			operator = ">"
		}
		valuePosition := len(arguments) + 1
		idPosition := len(arguments) + 2
		arguments = append(arguments, input.After.Value, input.After.ID)
		value := fmt.Sprintf("$%d", valuePosition)
		if input.Sort == SortCreatedAt || input.Sort == SortUpdatedAt {
			value += "::timestamptz"
		}
		seek := fmt.Sprintf("(%s %s %s OR (%s = %s AND id %s $%d))", orderColumn, operator, value, orderColumn, value, operator, idPosition)
		if where == "" {
			where = " WHERE " + seek
		} else {
			where += " AND " + seek
		}
	}
	limitPosition := len(arguments) + 1
	arguments = append(arguments, input.Limit)
	statement := `WITH admin_jobs AS (` + jobUnionSQL + `)
		SELECT ` + jobColumns + ` FROM admin_jobs` + where + `
		ORDER BY ` + orderColumn + ` ` + direction + `, id ` + direction + `
		LIMIT $` + fmt.Sprint(limitPosition)
	if !input.CursorMode {
		offsetPosition := len(arguments) + 1
		arguments = append(arguments, input.Offset)
		statement += ` OFFSET $` + fmt.Sprint(offsetPosition)
	}
	rows, err := repository.database.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("list administrator jobs: %w", err)
	}
	defer rows.Close()
	result := make([]JobRecord, 0)
	for rows.Next() {
		record, err := scanJob(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan administrator job: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate administrator jobs: %w", err)
	}
	var total int
	if input.TotalHint != nil {
		if *input.TotalHint < 0 {
			return nil, 0, fmt.Errorf("pagination total hint is invalid")
		}
		total = *input.TotalHint
	} else if err := repository.database.QueryRow(ctx, `WITH admin_jobs AS (`+jobUnionSQL+`)
		SELECT count(*)::int FROM admin_jobs`+countWhere, countArguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count administrator jobs: %w", err)
	}
	return result, total, nil
}

func (repository *Repository) FindJob(ctx context.Context, jobID string) (JobRecord, error) {
	record, err := scanJob(repository.database.QueryRow(ctx, `WITH admin_jobs AS (`+jobUnionSQL+`)
		SELECT `+jobColumns+` FROM admin_jobs WHERE id = $1 LIMIT 1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return JobRecord{}, apperror.NotFound("Background job was not found")
	}
	if err != nil {
		return JobRecord{}, fmt.Errorf("find administrator job: %w", err)
	}
	return record, nil
}

func (repository *Repository) FindMetadataVersion(ctx context.Context, jobID string) (int, bool, error) {
	var version int
	err := repository.database.QueryRow(ctx,
		"SELECT version FROM metadata_writeback_jobs WHERE id = $1", jobID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find metadata writeback job: %w", err)
	}
	return version, true, nil
}

func (repository *Repository) RetryMediaOrScan(ctx context.Context, jobID string) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin administrator job retry: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if err := retryScanJob(ctx, transaction, jobID); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit administrator job retry: %w", err)
	}
	return nil
}

func (repository *Repository) CancelMediaOrScan(ctx context.Context, jobID string) error {
	transaction, err := repository.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin administrator job cancellation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if err := cancelScanJob(ctx, transaction, jobID); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit administrator job cancellation: %w", err)
	}
	return nil
}

func (repository *Repository) EventState(ctx context.Context) (EventRecord, error) {
	var result EventRecord
	err := repository.database.QueryRow(ctx, `
		SELECT max(source.updated_at), coalesce(sum(source.active), 0)::int
		FROM (
			SELECT max(updated_at) AS updated_at,
				count(*) FILTER (WHERE status IN ('PENDING', 'RUNNING'))::int AS active
			FROM library_scan_runs
			UNION ALL
			SELECT max(updated_at) AS updated_at,
				count(*) FILTER (WHERE status IN ('PENDING', 'PROCESSING'))::int AS active
			FROM metadata_writeback_jobs
		) source`,
	).Scan(&result.UpdatedAt, &result.Active)
	if err != nil {
		return EventRecord{}, fmt.Errorf("read administrator job event state: %w", err)
	}
	return result, nil
}

func retryScanJob(ctx context.Context, transaction pgx.Tx, jobID string) error {
	var rootID, previousStatus string
	var runRootVersion int
	err := transaction.QueryRow(ctx, `
		SELECT root_id::text, status::text, root_version
		FROM library_scan_runs WHERE id = $1 FOR UPDATE`, jobID,
	).Scan(&rootID, &previousStatus, &runRootVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Background job was not found")
	}
	if err != nil {
		return fmt.Errorf("lock library scan for retry: %w", err)
	}
	if previousStatus != "FAILED" && previousStatus != "CANCELLED" {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Only failed or cancelled scans can be retried",
			nil,
		)
	}

	var enabled bool
	var rootVersion int
	err = transaction.QueryRow(ctx, `
		SELECT enabled, version FROM library_roots WHERE id = $1 FOR UPDATE`, rootID,
	).Scan(&enabled, &rootVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Music source was not found")
	}
	if err != nil {
		return fmt.Errorf("lock library root for scan retry: %w", err)
	}
	if !enabled {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Disabled music sources cannot be scanned",
			nil,
		)
	}
	var active bool
	if err := transaction.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM library_scan_runs
			WHERE root_id = $1 AND id <> $2 AND status IN ('PENDING', 'RUNNING')
		)`, rootID, jobID).Scan(&active); err != nil {
		return fmt.Errorf("check active library scan: %w", err)
	}
	if active {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"This music source already has an active scan",
			nil,
		)
	}

	now := time.Now().UTC()
	_, err = transaction.Exec(ctx, `
		UPDATE library_scan_runs SET
			status = 'PENDING', discovered_files = 0, processed_files = 0,
			failed_files = 0, cancel_requested = false, root_version = $1,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			heartbeat_at = NULL, started_at = NULL, completed_at = NULL,
			last_error = NULL, updated_at = $2
		WHERE id = $3 AND root_version = $4`,
		rootVersion, now, jobID, runRootVersion)
	if isUniqueViolation(err) {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"This music source already has an active scan",
			nil,
		)
	}
	if err != nil {
		return fmt.Errorf("retry library scan: %w", err)
	}
	if err := reconcileScanRootState(ctx, transaction, rootID, rootVersion, "PENDING", nil, now); err != nil {
		return err
	}

	return nil
}

func cancelScanJob(ctx context.Context, transaction pgx.Tx, jobID string) error {
	var previousStatus, rootID string
	var rootVersion int
	err := transaction.QueryRow(ctx, `
		SELECT status::text, root_id, root_version FROM library_scan_runs WHERE id = $1 FOR UPDATE`, jobID,
	).Scan(&previousStatus, &rootID, &rootVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Background job was not found")
	}
	if err != nil {
		return fmt.Errorf("lock library scan for cancellation: %w", err)
	}
	if previousStatus != "PENDING" && previousStatus != "RUNNING" {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Only queued or running scans can be cancelled",
			nil,
		)
	}
	now := time.Now().UTC()
	status := previousStatus
	if previousStatus == "PENDING" {
		status = "CANCELLED"
	}
	_, err = transaction.Exec(ctx, `
		UPDATE library_scan_runs SET
			status = $1, cancel_requested = true,
			locked_by = CASE WHEN $2 = 'CANCELLED' THEN NULL ELSE locked_by END,
			locked_until = CASE WHEN $2 = 'CANCELLED' THEN NULL ELSE locked_until END,
			heartbeat_at = CASE WHEN $2 = 'CANCELLED' THEN NULL ELSE heartbeat_at END,
			completed_at = CASE WHEN $2 = 'CANCELLED' THEN $3 ELSE completed_at END,
			updated_at = $3
		WHERE id = $4`, status, status, now, jobID)
	if err != nil {
		return fmt.Errorf("cancel library scan: %w", err)
	}
	if status == "CANCELLED" {
		if err := reconcileScanRootState(ctx, transaction, rootID, rootVersion, "CANCELLED", nil, now); err != nil {
			return err
		}
	}
	return nil
}

type scanRootStateExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func reconcileScanRootState(
	ctx context.Context,
	transaction scanRootStateExecutor,
	rootID string,
	rootVersion int,
	scanStatus string,
	lastError *string,
	now time.Time,
) error {
	_, err := transaction.Exec(ctx, `UPDATE library_roots AS root SET
		status = CASE
			WHEN NOT root.enabled THEN 'DISABLED'::library_root_status
			WHEN EXISTS (
				SELECT 1 FROM library_scan_runs active
				WHERE active.root_id=root.id AND active.status IN ('PENDING','RUNNING')
			) THEN 'SCANNING'::library_root_status
			WHEN root.version <> $2 THEN 'UNKNOWN'::library_root_status
			WHEN $3::text IN ('PENDING','RUNNING') THEN 'SCANNING'::library_root_status
			WHEN $3::text = 'COMPLETED' THEN 'READY'::library_root_status
			WHEN $3::text = 'FAILED' THEN 'ERROR'::library_root_status
			WHEN root.last_error IS NOT NULL THEN 'ERROR'::library_root_status
			WHEN root.last_scan_at IS NOT NULL THEN 'READY'::library_root_status
			ELSE 'UNKNOWN'::library_root_status
		END,
		last_scan_at = CASE
			WHEN root.version=$2 AND $3::text IN ('COMPLETED','FAILED') THEN $4
			WHEN $3::text IN ('COMPLETED','FAILED') THEN coalesce(root.last_scan_at,$4)
			ELSE root.last_scan_at
		END,
		last_error = CASE
			WHEN root.version<>$2 THEN NULL
			WHEN $3::text IN ('PENDING','RUNNING','COMPLETED','CANCELLED') THEN NULL
			WHEN $3::text = 'FAILED' THEN $5
			ELSE root.last_error
		END,
		updated_at=GREATEST(root.updated_at,$4)
		WHERE root.id=$1`, rootID, rootVersion, scanStatus, now, lastError)
	if err != nil {
		return fmt.Errorf("reconcile library scan root state: %w", err)
	}
	return nil
}

func jobWhere(input ListQuery) (string, []any) {
	conditions := make([]string, 0, 3)
	arguments := make([]any, 0, 3)
	if input.Status != "" {
		arguments = append(arguments, input.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(arguments)))
	}
	if input.Type != "" {
		arguments = append(arguments, input.Type)
		conditions = append(conditions, fmt.Sprintf("type = $%d", len(arguments)))
	}
	if input.Search != "" {
		arguments = append(arguments, "%"+escapeLike(input.Search)+"%")
		position := len(arguments)
		conditions = append(conditions, fmt.Sprintf(
			"(title ILIKE $%d ESCAPE '\\' OR id::text ILIKE $%d ESCAPE '\\' OR error_message ILIKE $%d ESCAPE '\\')",
			position, position, position,
		))
	}
	if len(conditions) == 0 {
		return "", arguments
	}
	return " WHERE " + strings.Join(conditions, " AND "), arguments
}

func escapeLike(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, character := range value {
		if character == '\\' || character == '%' || character == '_' {
			result.WriteByte('\\')
		}
		result.WriteRune(character)
	}
	return result.String()
}

type rowScanner interface {
	Scan(...any) error
}

func scanJob(scanner rowScanner) (JobRecord, error) {
	var record JobRecord
	err := scanner.Scan(
		&record.Source, &record.ID, &record.Type, &record.Status, &record.Title,
		&record.Progress, &record.Processed, &record.Total, &record.Attempts,
		&record.MaxAttempts, &record.Version, &record.CreatedAt, &record.UpdatedAt,
		&record.StartedAt, &record.CompletedAt, &record.ErrorCode, &record.ErrorMessage,
		&record.TrackID, &record.SourceID, &record.SourceAssetID, &record.CancelRequested,
		&record.NextAttemptAt, &record.LockedUntil, &record.HeartbeatAt,
	)
	return record, err
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

const jobColumns = `
	source, id, type, status, title, progress, processed, total, attempts,
	max_attempts, version, created_at, updated_at, started_at, completed_at,
	error_code, error_message, track_id, source_id, source_asset_id,
	cancel_requested, next_attempt_at, locked_until, heartbeat_at`

const jobUnionSQL = `
	SELECT
		'SCAN'::text AS source,
		run.id,
		'SOURCE_SCAN'::text AS type,
		CASE run.status
			WHEN 'PENDING' THEN 'QUEUED'
			WHEN 'RUNNING' THEN 'RUNNING'
			WHEN 'COMPLETED' THEN 'SUCCEEDED'
			WHEN 'CANCELLED' THEN 'CANCELED'
			ELSE 'FAILED'
		END::text AS status,
		root.name AS title,
		CASE
			WHEN run.status = 'COMPLETED' THEN 100
			WHEN run.discovered_files > 0 THEN LEAST(
				100, ROUND((run.processed_files::numeric / run.discovered_files) * 100)::int
			)
			ELSE 0
		END::int AS progress,
		run.processed_files AS processed,
		run.discovered_files AS total,
		CASE WHEN run.started_at IS NULL THEN 0 ELSE 1 END::int AS attempts,
		1::int AS max_attempts,
		NULL::int AS version,
		run.created_at,
		run.updated_at,
		run.started_at,
		run.completed_at,
		CASE WHEN run.last_error IS NULL THEN NULL ELSE 'LIBRARY_SCAN_FAILED' END::text AS error_code,
		run.last_error AS error_message,
		NULL::uuid AS track_id,
		run.root_id AS source_id,
		NULL::uuid AS source_asset_id,
		run.cancel_requested,
		NULL::timestamptz AS next_attempt_at,
		run.locked_until,
		run.heartbeat_at
	FROM library_scan_runs run
	JOIN library_roots root ON root.id = run.root_id

	UNION ALL

	SELECT
		'TAG'::text AS source,
		job.id,
		'TAG_WRITE'::text AS type,
		CASE job.status
			WHEN 'PENDING' THEN 'QUEUED'
			WHEN 'PROCESSING' THEN 'RUNNING'
			WHEN 'READY' THEN 'SUCCEEDED'
			WHEN 'CANCELLED' THEN 'CANCELED'
			ELSE 'FAILED'
		END::text AS status,
		track.title,
		CASE WHEN job.status = 'READY' THEN 100 ELSE 0 END::int AS progress,
		CASE WHEN job.status = 'READY' THEN 1 ELSE 0 END::int AS processed,
		1::int AS total,
		job.attempts,
		job.max_attempts,
		job.version,
		job.created_at,
		job.updated_at,
		job.started_at,
		job.completed_at,
		job.last_error_code AS error_code,
		job.last_error AS error_message,
		job.track_id,
		job.source_id,
		NULL::uuid AS source_asset_id,
		job.cancel_requested,
		job.next_attempt_at,
		job.locked_until,
		NULL::timestamptz AS heartbeat_at
	FROM metadata_writeback_jobs job
	JOIN tracks track ON track.id = job.track_id`
