package adminmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/audiostatus"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) Dashboard(ctx context.Context) (DashboardCounts, error) {
	var result DashboardCounts
	result.Tracks = map[string]int{
		string(audiostatus.Processing): 0,
		string(audiostatus.Ready):      0,
		string(audiostatus.Error):      0,
		string(audiostatus.Archived):   0,
	}
	result.Sources = make(map[string]int)
	result.Jobs = make(map[string]int)
	trackStatusCountsSQL := `
		SELECT audio_status.value, count(*)::int
		FROM tracks t
		CROSS JOIN LATERAL (
			SELECT ` + audiostatus.Expression("t") + ` AS value
		) audio_status
		GROUP BY audio_status.value`
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE status = 'ACTIVE')::int,
		       count(*) FILTER (WHERE role = 'ADMIN' AND status = 'ACTIVE')::int
		FROM users
	`).Scan(&result.UsersTotal, &result.UsersActive, &result.UsersAdministrators); err != nil {
		return DashboardCounts{}, fmt.Errorf("query dashboard user counts: %w", err)
	}
	if err := repository.pool.QueryRow(ctx, `
		SELECT (SELECT count(*)::int FROM artists), (SELECT count(*)::int FROM albums)
	`).Scan(&result.Artists, &result.Albums); err != nil {
		return DashboardCounts{}, fmt.Errorf("query dashboard catalog counts: %w", err)
	}
	for _, grouped := range []struct {
		statement string
		target    map[string]int
		label     string
	}{
		{trackStatusCountsSQL, result.Tracks, "tracks"},
		{`SELECT status::text, count(*)::int FROM local_music_sources GROUP BY status`, result.Sources, "sources"},
		{`SELECT status::text, count(*)::int FROM media_jobs GROUP BY status`, result.Jobs, "jobs"},
	} {
		rows, err := repository.pool.Query(ctx, grouped.statement)
		if err != nil {
			return DashboardCounts{}, fmt.Errorf("query dashboard %s: %w", grouped.label, err)
		}
		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err != nil {
				rows.Close()
				return DashboardCounts{}, fmt.Errorf("scan dashboard %s: %w", grouped.label, err)
			}
			grouped.target[status] = count
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return DashboardCounts{}, fmt.Errorf("iterate dashboard %s: %w", grouped.label, err)
		}
	}
	activities, err := repository.queryRecentActivity(ctx)
	if err != nil {
		return DashboardCounts{}, err
	}
	result.RecentActivity = activities
	return result, nil
}

func (repository *Repository) queryRecentActivity(ctx context.Context) ([]AuditRecord, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT a.id, a.actor_id, u.username, p.display_name, a.action, a.target_type,
		       a.target_id, a.result::text, a.trace_id, a.details, a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_id
		LEFT JOIN user_profiles p ON p.user_id = u.id
		ORDER BY a.created_at DESC
		LIMIT 12
	`)
	if err != nil {
		return nil, fmt.Errorf("query dashboard activity: %w", err)
	}
	defer rows.Close()
	result := make([]AuditRecord, 0, 12)
	for rows.Next() {
		var record AuditRecord
		var details []byte
		if err := rows.Scan(
			&record.ID, &record.ActorID, &record.ActorUsername, &record.ActorDisplayName,
			&record.Action, &record.TargetType, &record.TargetID, &record.Result,
			&record.TraceID, &details, &record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan dashboard activity: %w", err)
		}
		if err := json.Unmarshal(details, &record.Details); err != nil {
			return nil, fmt.Errorf("decode dashboard activity details: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard activity: %w", err)
	}
	return result, nil
}

func (repository *Repository) ListUsers(
	ctx context.Context,
	query ListUsersQuery,
) ([]UserRecord, int, error) {
	arguments := make([]any, 0, 5)
	conditions := make([]string, 0, 3)
	if query.Search != "" {
		position := appendAdminArgument(&arguments, "%"+escapeLike(query.Search)+"%")
		conditions = append(conditions, fmt.Sprintf(
			`(u.username ILIKE $%d ESCAPE E'\\' OR p.display_name ILIKE $%d ESCAPE E'\\')`,
			position, position,
		))
	}
	if query.Role != "" {
		conditions = append(conditions, fmt.Sprintf("u.role = $%d", appendAdminArgument(&arguments, query.Role)))
	}
	if query.Status != "" {
		conditions = append(conditions, fmt.Sprintf("u.status = $%d", appendAdminArgument(&arguments, query.Status)))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	countArguments := append([]any(nil), arguments...)
	limitPosition := appendAdminArgument(&arguments, query.Limit)
	offsetPosition := appendAdminArgument(&arguments, query.Offset)
	statement := userProjectionSQL + where + fmt.Sprintf(
		" ORDER BY u.created_at DESC, u.id DESC LIMIT $%d OFFSET $%d", limitPosition, offsetPosition,
	)
	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query managed users: %w", err)
	}
	items, err := scanUsers(rows)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)::int FROM users u
		JOIN user_profiles p ON p.user_id = u.id
	`+where, countArguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count managed users: %w", err)
	}
	return items, total, nil
}

func (repository *Repository) FindUser(
	ctx context.Context,
	userID string,
	query SessionQuery,
) (UserRecord, []SessionRecord, int, error) {
	rows, err := repository.pool.Query(ctx, userProjectionSQL+" WHERE u.id = $1 LIMIT 1", userID)
	if err != nil {
		return UserRecord{}, nil, 0, fmt.Errorf("query managed user: %w", err)
	}
	users, scanErr := scanUsers(rows)
	rows.Close()
	if scanErr != nil {
		return UserRecord{}, nil, 0, scanErr
	}
	if len(users) == 0 {
		return UserRecord{}, nil, 0, apperror.NotFound("User was not found")
	}
	sessionRows, err := repository.pool.Query(ctx, `
		SELECT id, installation_id, device_name, platform, app_version,
		       created_at, last_seen_at, revoked_at
		FROM auth_sessions
		WHERE user_id = $1
		ORDER BY last_seen_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, userID, query.Limit, query.Offset)
	if err != nil {
		return UserRecord{}, nil, 0, fmt.Errorf("query managed user sessions: %w", err)
	}
	sessions := make([]SessionRecord, 0)
	for sessionRows.Next() {
		var session SessionRecord
		if err := sessionRows.Scan(
			&session.ID, &session.InstallationID, &session.DeviceName, &session.Platform,
			&session.AppVersion, &session.CreatedAt, &session.LastSeenAt, &session.RevokedAt,
		); err != nil {
			sessionRows.Close()
			return UserRecord{}, nil, 0, fmt.Errorf("scan managed user session: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := sessionRows.Err(); err != nil {
		sessionRows.Close()
		return UserRecord{}, nil, 0, fmt.Errorf("iterate managed user sessions: %w", err)
	}
	sessionRows.Close()
	var total int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*)::int FROM auth_sessions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return UserRecord{}, nil, 0, fmt.Errorf("count managed user sessions: %w", err)
	}
	return users[0], sessions, total, nil
}

func (repository *Repository) CreateUser(ctx context.Context, input CreateUserParams) (string, error) {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin managed user creation: %w", err)
	}
	defer transaction.Rollback(ctx)
	var userID string
	err = transaction.QueryRow(ctx, `
		INSERT INTO users (username, normalized_username, password_hash, role, status)
		VALUES ($1, $2, $3, $4, 'ACTIVE')
		RETURNING id
	`, input.Username, input.NormalizedUsername, input.PasswordHash, input.Role).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("insert managed user: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO user_profiles (user_id, display_name) VALUES ($1, $2)
	`, userID, input.DisplayName); err != nil {
		return "", fmt.Errorf("insert managed user profile: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit managed user creation: %w", err)
	}
	return userID, nil
}

func (repository *Repository) UpdateUser(ctx context.Context, input UpdateUserParams) error {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin managed user update: %w", err)
	}
	defer transaction.Rollback(ctx)
	var currentRole UserRole
	var currentStatus UserStatus
	var currentVersion int
	err = transaction.QueryRow(ctx, `
		SELECT role::text, status::text, version FROM users WHERE id = $1 FOR UPDATE
	`, input.UserID).Scan(&currentRole, &currentStatus, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("User was not found")
	}
	if err != nil {
		return fmt.Errorf("lock managed user: %w", err)
	}
	if currentVersion != input.ExpectedVersion {
		return userVersionConflict(input.ExpectedVersion, currentVersion)
	}
	removesAdministrator := currentRole == RoleAdmin && currentStatus == StatusActive &&
		((input.Role != nil && *input.Role == RoleUser) ||
			(input.Status != nil && (*input.Status == StatusSuspended || *input.Status == StatusDeleted)))
	if removesAdministrator {
		rows, err := transaction.Query(ctx, `
			SELECT id FROM users WHERE role = 'ADMIN' AND status = 'ACTIVE' FOR UPDATE
		`)
		if err != nil {
			return fmt.Errorf("lock active administrators: %w", err)
		}
		otherAdministrators := 0
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scan active administrator: %w", err)
			}
			if id != input.UserID {
				otherAdministrators++
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("iterate active administrators: %w", err)
		}
		if otherAdministrators < 1 {
			return apperror.Conflict(
				apperror.CodeInvalidStateTransition,
				"The last active administrator cannot be removed",
				nil,
			)
		}
	}
	set := []string{"version = version + 1", "updated_at = now()"}
	arguments := []any{input.UserID, input.ExpectedVersion}
	if input.Username != nil {
		set = append(set, fmt.Sprintf("username = $%d", appendAdminArgument(&arguments, *input.Username)))
		set = append(set, fmt.Sprintf("normalized_username = $%d", appendAdminArgument(&arguments, *input.NormalizedUsername)))
	}
	if input.Role != nil {
		set = append(set, fmt.Sprintf("role = $%d", appendAdminArgument(&arguments, *input.Role)))
	}
	if input.Status != nil {
		set = append(set, fmt.Sprintf("status = $%d", appendAdminArgument(&arguments, *input.Status)))
	}
	if input.Role != nil || input.Status != nil {
		set = append(set, "auth_version = auth_version + 1")
	}
	command, err := transaction.Exec(ctx, "UPDATE users SET "+strings.Join(set, ", ")+" WHERE id = $1 AND version = $2", arguments...)
	if err != nil {
		return fmt.Errorf("update managed user: %w", err)
	}
	if command.RowsAffected() != 1 {
		return repository.userVersionFailure(ctx, transaction, input.UserID, input.ExpectedVersion)
	}
	if input.DisplayName != nil || input.SetBio {
		profileSet := []string{"updated_at = now()"}
		profileArguments := []any{input.UserID}
		if input.DisplayName != nil {
			profileSet = append(profileSet, fmt.Sprintf("display_name = $%d", appendAdminArgument(&profileArguments, *input.DisplayName)))
		}
		if input.SetBio {
			profileSet = append(profileSet, fmt.Sprintf("bio = $%d", appendAdminArgument(&profileArguments, input.Bio)))
		}
		if _, err := transaction.Exec(ctx, "UPDATE user_profiles SET "+strings.Join(profileSet, ", ")+" WHERE user_id = $1", profileArguments...); err != nil {
			return fmt.Errorf("update managed user profile: %w", err)
		}
	}
	if (input.Status != nil && (*input.Status == StatusSuspended || *input.Status == StatusDeleted)) ||
		(input.Role != nil && *input.Role == RoleUser) {
		if err := revokeAllUserSessions(ctx, transaction, input.UserID); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit managed user update: %w", err)
	}
	return nil
}

func (repository *Repository) ResetPassword(
	ctx context.Context,
	userID string,
	expectedVersion int,
	passwordHash string,
) error {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin managed password reset: %w", err)
	}
	defer transaction.Rollback(ctx)
	command, err := transaction.Exec(ctx, `
		UPDATE users
		SET password_hash = $3, auth_version = auth_version + 1,
		    version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2
	`, userID, expectedVersion, passwordHash)
	if err != nil {
		return fmt.Errorf("reset managed user password: %w", err)
	}
	if command.RowsAffected() != 1 {
		return repository.userVersionFailure(ctx, transaction, userID, expectedVersion)
	}
	if err := revokeAllUserSessions(ctx, transaction, userID); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit managed password reset: %w", err)
	}
	return nil
}

func (repository *Repository) RevokeSession(ctx context.Context, userID, sessionID string) error {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin managed session revocation: %w", err)
	}
	defer transaction.Rollback(ctx)
	now := time.Now().UTC()
	command, err := transaction.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = $3 WHERE id = $1 AND user_id = $2
	`, sessionID, userID, now)
	if err != nil {
		return fmt.Errorf("revoke managed user session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.NotFound("Session was not found")
	}
	if _, err := transaction.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE session_id = $1`, sessionID, now); err != nil {
		return fmt.Errorf("revoke managed session refresh tokens: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit managed session revocation: %w", err)
	}
	return nil
}

func (repository *Repository) UpdateStatus(
	ctx context.Context,
	actorID, userID string,
	expectedVersion int,
	status UserStatus,
) error {
	if actorID == userID && status == StatusSuspended {
		return apperror.Validation("Administrators cannot suspend their own account")
	}
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin managed status update: %w", err)
	}
	defer transaction.Rollback(ctx)
	command, err := transaction.Exec(ctx, `
		UPDATE users
		SET status = $3, auth_version = auth_version + 1,
		    version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2
	`, userID, expectedVersion, status)
	if err != nil {
		return fmt.Errorf("update managed user status: %w", err)
	}
	if command.RowsAffected() != 1 {
		return repository.userVersionFailure(ctx, transaction, userID, expectedVersion)
	}
	if status == StatusSuspended {
		if err := revokeAllUserSessions(ctx, transaction, userID); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit managed status update: %w", err)
	}
	return nil
}

func (repository *Repository) WriteAudit(ctx context.Context, input AuditWrite) error {
	details, err := input.JSONDetails()
	if err != nil {
		return fmt.Errorf("encode managed user audit details: %w", err)
	}
	_, err = repository.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_id, action, target_type, target_id, result, trace_id, details)
		VALUES ($1, $2, 'user', $3, 'SUCCESS', $4, $5::jsonb)
	`, input.ActorID, input.Action, input.TargetID, input.TraceID, details)
	if err != nil {
		return fmt.Errorf("write managed user audit: %w", err)
	}
	return nil
}

func (repository *Repository) userVersionFailure(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
	expectedVersion int,
) error {
	var currentVersion int
	err := transaction.QueryRow(ctx, `SELECT version FROM users WHERE id = $1`, userID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("User was not found")
	}
	if err != nil {
		return fmt.Errorf("inspect managed user version: %w", err)
	}
	return userVersionConflict(expectedVersion, currentVersion)
}

func revokeAllUserSessions(ctx context.Context, transaction pgx.Tx, userID string) error {
	now := time.Now().UTC()
	if _, err := transaction.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = $2 WHERE user_id = $1
	`, userID, now); err != nil {
		return fmt.Errorf("revoke managed user sessions: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE refresh_tokens token
		SET revoked_at = $2
		FROM auth_sessions session
		WHERE token.session_id = session.id AND session.user_id = $1
	`, userID, now); err != nil {
		return fmt.Errorf("revoke managed user refresh tokens: %w", err)
	}
	return nil
}

func scanUsers(rows pgx.Rows) ([]UserRecord, error) {
	result := make([]UserRecord, 0)
	for rows.Next() {
		var record UserRecord
		if err := rows.Scan(
			&record.ID, &record.Username, &record.DisplayName, &record.Bio,
			&record.Role, &record.Status, &record.AuthVersion, &record.Version,
			&record.AvatarAssetID, &record.CreatedAt, &record.UserUpdatedAt, &record.ProfileUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan managed user: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed users: %w", err)
	}
	return result, nil
}

func userVersionConflict(expectedVersion, currentVersion int) error {
	return apperror.Conflict(apperror.CodeVersionConflict, "User version is stale", map[string]any{
		"expectedVersion": expectedVersion,
		"currentVersion":  currentVersion,
	})
}

func appendAdminArgument(arguments *[]any, value any) int {
	*arguments = append(*arguments, value)
	return len(*arguments)
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

const userProjectionSQL = `
	SELECT u.id, u.username, p.display_name, p.bio, u.role::text, u.status::text,
	       u.auth_version, u.version, p.avatar_asset_id,
	       u.created_at, u.updated_at, p.updated_at
	FROM users u
	JOIN user_profiles p ON p.user_id = u.id
`
