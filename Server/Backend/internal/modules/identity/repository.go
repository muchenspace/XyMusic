package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound             = errors.New("identity record not found")
	ErrDuplicateUsername    = errors.New("username already exists")
	ErrRefreshTokenConsumed = errors.New("refresh token was already consumed")
)

// Store is the persistence boundary used by Service. Repository is its pgx
// implementation; tests and alternate persistence adapters can implement this
// interface without coupling business logic to PostgreSQL primitives.
type Store interface {
	UsernameExists(ctx context.Context, normalizedUsername string) (bool, error)
	CreateUser(ctx context.Context, input CreateUserParams) (User, error)
	FindUserByNormalizedUsername(ctx context.Context, normalizedUsername string) (User, error)
	CreateSession(ctx context.Context, input CreateSessionParams) (AuthSession, error)
	FindRefreshRecord(ctx context.Context, tokenHash string) (RefreshRecord, error)
	RotateRefreshToken(ctx context.Context, input RotateRefreshTokenParams) error
	RevokeRefreshFamily(ctx context.Context, familyID, sessionID string, revokedAt time.Time) error
	FindAuthenticatedSession(ctx context.Context, userID, sessionID string) (User, AuthSession, error)
	RevokeSession(ctx context.Context, userID, sessionID string, revokedAt time.Time) error
	RevokeAllSessions(ctx context.Context, userID string, revokedAt time.Time) error
	FindCurrentUser(ctx context.Context, userID string) (CurrentUserRecord, error)
}

type CreateUserParams struct {
	Username           string
	NormalizedUsername string
	PasswordHash       string
}

type CreateSessionParams struct {
	UserID                string
	Device                DeviceInfoInput
	RefreshTokenHash      string
	RefreshTokenFamilyID  string
	RefreshTokenExpiresAt time.Time
}

type RotateRefreshTokenParams struct {
	TokenID       string
	SessionID     string
	FamilyID      string
	TokenHash     string
	ParentTokenID string
	ExpiresAt     time.Time
	ConsumedAt    time.Time
}

// Repository persists identity state in the existing users, user_profiles,
// auth_sessions and refresh_tokens tables.
type Repository struct {
	pool *pgxpool.Pool
}

var _ Store = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) UsernameExists(ctx context.Context, normalizedUsername string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE normalized_username = $1)`,
		normalizedUsername,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check username availability: %w", err)
	}
	return exists, nil
}

func (r *Repository) CreateUser(ctx context.Context, input CreateUserParams) (User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, fmt.Errorf("begin user creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var user User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, normalized_username, password_hash, status)
		VALUES ($1, $2, $3, 'ACTIVE')
		RETURNING id, username, normalized_username, password_hash, role, status,
		          auth_version, version, created_at, updated_at
	`, input.Username, input.NormalizedUsername, input.PasswordHash).Scan(userScanDestinations(&user)...)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrDuplicateUsername
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO user_profiles (user_id, display_name) VALUES ($1, $2)`,
		user.ID, input.Username,
	); err != nil {
		return User{}, fmt.Errorf("insert user profile: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrDuplicateUsername
		}
		return User{}, fmt.Errorf("commit user creation: %w", err)
	}
	return user, nil
}

func (r *Repository) FindUserByNormalizedUsername(ctx context.Context, normalizedUsername string) (User, error) {
	var user User
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, normalized_username, password_hash, role, status,
		       auth_version, version, created_at, updated_at
		FROM users
		WHERE normalized_username = $1
		LIMIT 1
	`, normalizedUsername).Scan(userScanDestinations(&user)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by normalized username: %w", err)
	}
	return user, nil
}

func (r *Repository) CreateSession(ctx context.Context, input CreateSessionParams) (AuthSession, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AuthSession{}, fmt.Errorf("begin session creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var session AuthSession
	err = tx.QueryRow(ctx, `
		INSERT INTO auth_sessions (
			user_id, installation_id, device_name, platform, app_version
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, installation_id, device_name, platform, app_version,
		          created_at, last_seen_at, revoked_at
	`, input.UserID, input.Device.InstallationID, input.Device.Name,
		input.Device.Platform, input.Device.AppVersion,
	).Scan(sessionScanDestinations(&session)...)
	if err != nil {
		return AuthSession{}, fmt.Errorf("insert auth session: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO refresh_tokens (session_id, token_hash, family_id, expires_at)
		VALUES ($1, $2, $3, $4)
	`, session.ID, input.RefreshTokenHash, input.RefreshTokenFamilyID, input.RefreshTokenExpiresAt); err != nil {
		return AuthSession{}, fmt.Errorf("insert initial refresh token: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthSession{}, fmt.Errorf("commit session creation: %w", err)
	}
	return session, nil
}

func (r *Repository) FindRefreshRecord(ctx context.Context, tokenHash string) (RefreshRecord, error) {
	var record RefreshRecord
	destinations := refreshTokenScanDestinations(&record.Token)
	destinations = append(destinations, sessionScanDestinations(&record.Session)...)
	destinations = append(destinations, userScanDestinations(&record.User)...)
	err := r.pool.QueryRow(ctx, `
		SELECT
			rt.id, rt.session_id, rt.token_hash, rt.family_id, rt.parent_token_id,
			rt.created_at, rt.expires_at, rt.used_at, rt.revoked_at,
			s.id, s.user_id, s.installation_id, s.device_name, s.platform,
			s.app_version, s.created_at, s.last_seen_at, s.revoked_at,
			u.id, u.username, u.normalized_username, u.password_hash, u.role,
			u.status, u.auth_version, u.version, u.created_at, u.updated_at
		FROM refresh_tokens rt
		JOIN auth_sessions s ON s.id = rt.session_id
		JOIN users u ON u.id = s.user_id
		WHERE rt.token_hash = $1
		LIMIT 1
	`, tokenHash).Scan(destinations...)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshRecord{}, ErrNotFound
	}
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("find refresh token: %w", err)
	}
	return record, nil
}

func (r *Repository) RotateRefreshToken(ctx context.Context, input RotateRefreshTokenParams) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin refresh token rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var consumedID string
	err = tx.QueryRow(ctx, `
		UPDATE refresh_tokens
		SET used_at = $2
		WHERE id = $1
		  AND used_at IS NULL
		  AND revoked_at IS NULL
		  AND expires_at > $2
		RETURNING id
	`, input.TokenID, input.ConsumedAt).Scan(&consumedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRefreshTokenConsumed
	}
	if err != nil {
		return fmt.Errorf("consume refresh token: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO refresh_tokens (
			session_id, token_hash, family_id, parent_token_id, expires_at
		) VALUES ($1, $2, $3, $4, $5)
	`, input.SessionID, input.TokenHash, input.FamilyID, input.ParentTokenID, input.ExpiresAt); err != nil {
		return fmt.Errorf("insert rotated refresh token: %w", err)
	}
	if _, err = tx.Exec(ctx,
		`UPDATE auth_sessions SET last_seen_at = $2 WHERE id = $1`,
		input.SessionID, input.ConsumedAt,
	); err != nil {
		return fmt.Errorf("update session last seen time: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit refresh token rotation: %w", err)
	}
	return nil
}

func (r *Repository) RevokeRefreshFamily(
	ctx context.Context,
	familyID string,
	sessionID string,
	revokedAt time.Time,
) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin refresh family revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID, revokedAt); err != nil {
		return fmt.Errorf("revoke refresh token family: %w", err)
	}
	if _, err = tx.Exec(ctx,
		`UPDATE auth_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`,
		sessionID, revokedAt,
	); err != nil {
		return fmt.Errorf("revoke reused-token session: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit refresh family revocation: %w", err)
	}
	return nil
}

func (r *Repository) FindAuthenticatedSession(
	ctx context.Context,
	userID string,
	sessionID string,
) (User, AuthSession, error) {
	var user User
	var session AuthSession
	destinations := userScanDestinations(&user)
	destinations = append(destinations, sessionScanDestinations(&session)...)
	err := r.pool.QueryRow(ctx, `
		SELECT
			u.id, u.username, u.normalized_username, u.password_hash, u.role,
			u.status, u.auth_version, u.version, u.created_at, u.updated_at,
			s.id, s.user_id, s.installation_id, s.device_name, s.platform,
			s.app_version, s.created_at, s.last_seen_at, s.revoked_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = $1 AND u.id = $2
		LIMIT 1
	`, sessionID, userID).Scan(destinations...)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, AuthSession{}, ErrNotFound
	}
	if err != nil {
		return User{}, AuthSession{}, fmt.Errorf("find authenticated session: %w", err)
	}
	return user, session, nil
}

func (r *Repository) RevokeSession(
	ctx context.Context,
	userID string,
	sessionID string,
	revokedAt time.Time,
) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin session revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = $3
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, sessionID, userID, revokedAt); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE session_id = $1 AND revoked_at IS NULL
	`, sessionID, revokedAt); err != nil {
		return fmt.Errorf("revoke session refresh tokens: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit session revocation: %w", err)
	}
	return nil
}

func (r *Repository) RevokeAllSessions(ctx context.Context, userID string, revokedAt time.Time) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin all-session revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE revoked_at IS NULL
		  AND session_id IN (SELECT id FROM auth_sessions WHERE user_id = $1)
	`, userID, revokedAt); err != nil {
		return fmt.Errorf("revoke all refresh tokens: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID, revokedAt); err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE users
		SET auth_version = auth_version + 1, updated_at = $2
		WHERE id = $1
	`, userID, revokedAt); err != nil {
		return fmt.Errorf("advance user auth version: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit all-session revocation: %w", err)
	}
	return nil
}

func (r *Repository) FindCurrentUser(ctx context.Context, userID string) (CurrentUserRecord, error) {
	var record CurrentUserRecord
	var avatarID *string
	var avatarMimeType *string
	var avatarChecksum *string
	var avatarWidth *int
	var avatarHeight *int
	var avatarUpdatedAt *time.Time
	destinations := userScanDestinations(&record.User)
	destinations = append(destinations,
		&record.DisplayName,
		&record.Bio,
		&record.ProfileUpdatedAt,
		&avatarID,
		&avatarMimeType,
		&avatarChecksum,
		&avatarWidth,
		&avatarHeight,
		&avatarUpdatedAt,
	)
	err := r.pool.QueryRow(ctx, `
		SELECT
			u.id, u.username, u.normalized_username, u.password_hash, u.role,
			u.status, u.auth_version, u.version, u.created_at, u.updated_at,
			p.display_name, p.bio, p.updated_at,
			a.id, a.mime_type, a.checksum_sha256,
			a.width, a.height, a.updated_at
		FROM users u
		JOIN user_profiles p ON p.user_id = u.id
		LEFT JOIN media_assets a
		  ON a.id = p.avatar_asset_id AND a.status = 'READY'
		WHERE u.id = $1
		LIMIT 1
	`, userID).Scan(destinations...)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentUserRecord{}, ErrNotFound
	}
	if err != nil {
		return CurrentUserRecord{}, fmt.Errorf("find current user: %w", err)
	}
	if avatarID != nil && avatarMimeType != nil && avatarUpdatedAt != nil {
		record.Avatar = &AvatarAsset{
			ID:             *avatarID,
			MimeType:       *avatarMimeType,
			ChecksumSHA256: avatarChecksum,
			Width:          avatarWidth,
			Height:         avatarHeight,
			UpdatedAt:      *avatarUpdatedAt,
		}
	}
	return record, nil
}

func userScanDestinations(user *User) []any {
	return []any{
		&user.ID,
		&user.Username,
		&user.NormalizedUsername,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&user.AuthVersion,
		&user.Version,
		&user.CreatedAt,
		&user.UpdatedAt,
	}
}

func sessionScanDestinations(session *AuthSession) []any {
	return []any{
		&session.ID,
		&session.UserID,
		&session.InstallationID,
		&session.DeviceName,
		&session.Platform,
		&session.AppVersion,
		&session.CreatedAt,
		&session.LastSeenAt,
		&session.RevokedAt,
	}
}

func refreshTokenScanDestinations(token *RefreshToken) []any {
	return []any{
		&token.ID,
		&token.SessionID,
		&token.TokenHash,
		&token.FamilyID,
		&token.ParentTokenID,
		&token.CreatedAt,
		&token.ExpiresAt,
		&token.UsedAt,
		&token.RevokedAt,
	}
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
