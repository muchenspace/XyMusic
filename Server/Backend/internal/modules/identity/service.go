package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
)

type AccessTokenManager interface {
	Issue(principal security.Principal) (token string, expiresAt time.Time, err error)
	Verify(raw string) (security.Principal, error)
}

type PasswordManager interface {
	Hash(password string) (string, error)
	Verify(password, encoded string) (bool, error)
}

type ArtworkURLSigner interface {
	PresignedGet(ctx context.Context, objectKey string, expires time.Duration) (string, error)
}

type Clock interface {
	Now() time.Time
}

type RefreshIdempotencyInput struct {
	ActorID     string
	Scope       string
	Key         string
	Payload     any
	RequestHash string
}

type RefreshOperation func(ctx context.Context) (AuthSessionDTO, error)

// RefreshIdempotency is injected so the identity module does not duplicate the
// shared, encrypted PostgreSQL idempotency implementation.
type RefreshIdempotency interface {
	ExecuteRefresh(
		ctx context.Context,
		input RefreshIdempotencyInput,
		operation RefreshOperation,
	) (session AuthSessionDTO, replayed bool, err error)
}

type ServiceDependencies struct {
	Repository   Store
	AccessTokens AccessTokenManager
	Idempotency  RefreshIdempotency
	ArtworkURLs  ArtworkURLSigner
	Passwords    PasswordManager
	Clock        Clock
	IDGenerator  func() string
	OpaqueToken  func() (string, error)
}

type Service struct {
	repository          Store
	accessTokens        AccessTokenManager
	idempotency         RefreshIdempotency
	artworkURLs         ArtworkURLSigner
	passwords           PasswordManager
	clock               Clock
	newID               func() string
	newOpaqueToken      func() (string, error)
	registrationEnabled bool
	refreshTokenTTL     time.Duration
	artworkURLTTL       time.Duration
	dummyPasswordHash   string
}

var _ AccessTokenManager = (*security.AccessTokenService)(nil)

func NewService(cfg config.Config, dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil {
		return nil, errors.New("identity repository is required")
	}
	if dependencies.AccessTokens == nil {
		return nil, errors.New("identity access-token manager is required")
	}
	if dependencies.Idempotency == nil {
		return nil, errors.New("identity refresh idempotency is required")
	}
	if dependencies.ArtworkURLs == nil {
		return nil, errors.New("identity artwork URL signer is required")
	}
	if dependencies.Passwords == nil {
		dependencies.Passwords = SecurityPasswordManager{}
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	if dependencies.IDGenerator == nil {
		dependencies.IDGenerator = uuid.NewString
	}
	if dependencies.OpaqueToken == nil {
		dependencies.OpaqueToken = security.CreateOpaqueToken
	}
	refreshTokenTTL := time.Duration(cfg.Security.RefreshTokenTTLSeconds) * time.Second
	if refreshTokenTTL <= 0 {
		return nil, errors.New("identity refresh-token TTL must be positive")
	}
	artworkURLTTL := time.Duration(cfg.Storage.SignedURLTTLSeconds) * time.Second
	if artworkURLTTL <= 0 {
		return nil, errors.New("identity artwork URL TTL must be positive")
	}
	dummyPasswordHash, err := dependencies.Passwords.Hash("not-a-real-user-password")
	if err != nil {
		return nil, fmt.Errorf("create identity timing-defense password hash: %w", err)
	}
	return &Service{
		repository:          dependencies.Repository,
		accessTokens:        dependencies.AccessTokens,
		idempotency:         dependencies.Idempotency,
		artworkURLs:         dependencies.ArtworkURLs,
		passwords:           dependencies.Passwords,
		clock:               dependencies.Clock,
		newID:               dependencies.IDGenerator,
		newOpaqueToken:      dependencies.OpaqueToken,
		registrationEnabled: cfg.Registration.Enabled,
		refreshTokenTTL:     refreshTokenTTL,
		artworkURLTTL:       artworkURLTTL,
		dummyPasswordHash:   dummyPasswordHash,
	}, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (RegistrationDTO, error) {
	if !s.registrationEnabled {
		return RegistrationDTO{}, apperror.Forbidden("当前服务器未开放用户注册，请联系管理员开启注册功能。")
	}
	username := trimSpace(input.Username)
	if err := validateRegistration(username, input.Password); err != nil {
		return RegistrationDTO{}, err
	}
	normalizedUsername := NormalizeUsername(username)
	exists, err := s.repository.UsernameExists(ctx, normalizedUsername)
	if err != nil {
		return RegistrationDTO{}, err
	}
	if exists {
		return RegistrationDTO{}, duplicateUsernameError()
	}
	passwordHash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return RegistrationDTO{}, fmt.Errorf("hash registration password: %w", err)
	}
	user, err := s.repository.CreateUser(ctx, CreateUserParams{
		Username:           username,
		NormalizedUsername: normalizedUsername,
		PasswordHash:       passwordHash,
	})
	if errors.Is(err, ErrDuplicateUsername) {
		return RegistrationDTO{}, duplicateUsernameError()
	}
	if err != nil {
		return RegistrationDTO{}, err
	}
	return RegistrationDTO{
		UserID:   user.ID,
		Username: user.Username,
		Status:   user.Status,
	}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (AuthSessionDTO, error) {
	// Transport-specific username/password bounds differ between the public
	// client and the web-admin login. Both routes validate their own schema;
	// the shared service only validates the device contract, matching legacy.
	if err := validateDevice(input.Device); err != nil {
		return AuthSessionDTO{}, err
	}
	normalizedUsername := NormalizeUsername(input.Username)
	user, err := s.repository.FindUserByNormalizedUsername(ctx, normalizedUsername)
	userFound := true
	if errors.Is(err, ErrNotFound) {
		userFound = false
		user = User{PasswordHash: s.dummyPasswordHash}
	} else if err != nil {
		return AuthSessionDTO{}, err
	}
	passwordMatches, err := s.passwords.Verify(input.Password, user.PasswordHash)
	if err != nil {
		return AuthSessionDTO{}, fmt.Errorf("verify login password: %w", err)
	}
	if !userFound || !passwordMatches {
		return AuthSessionDTO{}, apperror.Unauthorized(
			apperror.CodeInvalidCredentials,
			"Credentials are invalid",
		)
	}
	if err := requireActiveUser(user.Status); err != nil {
		return AuthSessionDTO{}, err
	}
	return s.createSession(ctx, user, input.Device)
}

func (s *Service) Refresh(
	ctx context.Context,
	refreshToken string,
	idempotencyKey string,
) (RefreshResult, error) {
	if err := validateRefreshInput(refreshToken, idempotencyKey); err != nil {
		return RefreshResult{}, err
	}
	tokenHash := security.HashSecret(refreshToken)
	initial, err := s.repository.FindRefreshRecord(ctx, tokenHash)
	if errors.Is(err, ErrNotFound) {
		return RefreshResult{}, sessionRevoked("Refresh token is invalid or revoked")
	}
	if err != nil {
		return RefreshResult{}, err
	}
	session, replayed, err := s.idempotency.ExecuteRefresh(ctx, RefreshIdempotencyInput{
		ActorID:     initial.User.ID,
		Scope:       "auth.refresh",
		Key:         idempotencyKey,
		Payload:     map[string]string{"refreshTokenHash": tokenHash},
		RequestHash: refreshRequestHash(tokenHash),
	}, func(operationContext context.Context) (AuthSessionDTO, error) {
		return s.rotateRefreshToken(operationContext, tokenHash)
	})
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Session: session, Replayed: replayed}, nil
}

func (s *Service) Authenticate(ctx context.Context, authorization string) (AuthenticatedActor, error) {
	rawToken, err := BearerToken(authorization)
	if err != nil {
		return AuthenticatedActor{}, err
	}
	principal, err := s.accessTokens.Verify(rawToken)
	if err != nil {
		if errors.Is(err, security.ErrExpiredAccessToken) {
			return AuthenticatedActor{}, apperror.Unauthorized(
				apperror.CodeAccessTokenExpired,
				"Access token has expired",
			)
		}
		return AuthenticatedActor{}, apperror.Unauthorized(
			apperror.CodeAuthenticationRequired,
			"Authentication is required",
		)
	}
	user, session, err := s.repository.FindAuthenticatedSession(
		ctx,
		principal.UserID,
		principal.SessionID,
	)
	if errors.Is(err, ErrNotFound) {
		return AuthenticatedActor{}, sessionRevoked("Session has been revoked")
	}
	if err != nil {
		return AuthenticatedActor{}, err
	}
	if session.RevokedAt != nil {
		return AuthenticatedActor{}, sessionRevoked("Session has been revoked")
	}
	if err := requireActiveUser(user.Status); err != nil {
		return AuthenticatedActor{}, err
	}
	if user.AuthVersion != principal.AuthVersion || string(user.Role) != principal.Role {
		return AuthenticatedActor{}, sessionRevoked("Session authorization is no longer current")
	}
	return AuthenticatedActor{
		UserID:      user.ID,
		SessionID:   session.ID,
		Role:        user.Role,
		AuthVersion: user.AuthVersion,
	}, nil
}

func (s *Service) Logout(ctx context.Context, actor AuthenticatedActor) error {
	return s.repository.RevokeSession(ctx, actor.UserID, actor.SessionID, s.clock.Now().UTC())
}

func (s *Service) LogoutAll(ctx context.Context, actor AuthenticatedActor) error {
	return s.repository.RevokeAllSessions(ctx, actor.UserID, s.clock.Now().UTC())
}

func (s *Service) GetAuthenticatedUser(
	ctx context.Context,
	actor AuthenticatedActor,
) (CurrentUserDTO, error) {
	return s.CurrentUser(ctx, actor.UserID)
}

func (s *Service) CurrentUser(ctx context.Context, userID string) (CurrentUserDTO, error) {
	record, err := s.repository.FindCurrentUser(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return CurrentUserDTO{}, apperror.NotFound("User no longer exists")
	}
	if err != nil {
		return CurrentUserDTO{}, err
	}
	return s.presentCurrentUser(ctx, record)
}

func (s *Service) createSession(
	ctx context.Context,
	user User,
	device DeviceInfoInput,
) (AuthSessionDTO, error) {
	rawRefreshToken, err := s.newOpaqueToken()
	if err != nil {
		return AuthSessionDTO{}, fmt.Errorf("create refresh token: %w", err)
	}
	now := s.clock.Now().UTC()
	refreshExpiresAt := now.Add(s.refreshTokenTTL)
	device.Name = trimSpace(device.Name)
	device.AppVersion = trimSpace(device.AppVersion)
	session, err := s.repository.CreateSession(ctx, CreateSessionParams{
		UserID:                user.ID,
		Device:                device,
		RefreshTokenHash:      security.HashSecret(rawRefreshToken),
		RefreshTokenFamilyID:  s.newID(),
		RefreshTokenExpiresAt: refreshExpiresAt,
	})
	if err != nil {
		return AuthSessionDTO{}, err
	}
	return s.authSessionResponse(ctx, user, session, rawRefreshToken, refreshExpiresAt)
}

func (s *Service) rotateRefreshToken(ctx context.Context, tokenHash string) (AuthSessionDTO, error) {
	record, err := s.repository.FindRefreshRecord(ctx, tokenHash)
	if errors.Is(err, ErrNotFound) {
		return AuthSessionDTO{}, sessionRevoked("Refresh token is invalid or revoked")
	}
	if err != nil {
		return AuthSessionDTO{}, err
	}
	now := s.clock.Now().UTC()
	if record.Token.UsedAt != nil || record.Token.RevokedAt != nil {
		if err := s.repository.RevokeRefreshFamily(
			ctx,
			record.Token.FamilyID,
			record.Session.ID,
			now,
		); err != nil {
			return AuthSessionDTO{}, err
		}
		return AuthSessionDTO{}, sessionRevoked("Refresh token reuse revoked the session")
	}
	if !record.Token.ExpiresAt.After(now) || record.Session.RevokedAt != nil {
		return AuthSessionDTO{}, sessionRevoked("Refresh token has expired or the session was revoked")
	}
	if err := requireActiveUser(record.User.Status); err != nil {
		return AuthSessionDTO{}, err
	}
	rawRefreshToken, err := s.newOpaqueToken()
	if err != nil {
		return AuthSessionDTO{}, fmt.Errorf("create rotated refresh token: %w", err)
	}
	refreshExpiresAt := now.Add(s.refreshTokenTTL)
	err = s.repository.RotateRefreshToken(ctx, RotateRefreshTokenParams{
		TokenID:       record.Token.ID,
		SessionID:     record.Session.ID,
		FamilyID:      record.Token.FamilyID,
		TokenHash:     security.HashSecret(rawRefreshToken),
		ParentTokenID: record.Token.ID,
		ExpiresAt:     refreshExpiresAt,
		ConsumedAt:    now,
	})
	if errors.Is(err, ErrRefreshTokenConsumed) {
		if revokeErr := s.repository.RevokeRefreshFamily(
			ctx,
			record.Token.FamilyID,
			record.Session.ID,
			now,
		); revokeErr != nil {
			return AuthSessionDTO{}, revokeErr
		}
		return AuthSessionDTO{}, sessionRevoked("Refresh token reuse revoked the session")
	}
	if err != nil {
		return AuthSessionDTO{}, err
	}
	return s.authSessionResponse(
		ctx,
		record.User,
		record.Session,
		rawRefreshToken,
		refreshExpiresAt,
	)
}

func (s *Service) authSessionResponse(
	ctx context.Context,
	user User,
	session AuthSession,
	rawRefreshToken string,
	refreshExpiresAt time.Time,
) (AuthSessionDTO, error) {
	accessToken, accessExpiresAt, err := s.accessTokens.Issue(security.Principal{
		UserID:      user.ID,
		SessionID:   session.ID,
		AuthVersion: user.AuthVersion,
		Role:        string(user.Role),
	})
	if err != nil {
		return AuthSessionDTO{}, fmt.Errorf("issue access token: %w", err)
	}
	currentUser, err := s.CurrentUser(ctx, user.ID)
	if err != nil {
		return AuthSessionDTO{}, err
	}
	return AuthSessionDTO{
		User: currentUser,
		Session: SessionDTO{
			ID:         session.ID,
			DeviceName: session.DeviceName,
			CreatedAt:  formatTimestamp(session.CreatedAt),
		},
		Tokens: TokensDTO{
			TokenType:             "Bearer",
			AccessToken:           accessToken,
			AccessTokenExpiresAt:  formatTimestamp(accessExpiresAt),
			RefreshToken:          rawRefreshToken,
			RefreshTokenExpiresAt: formatTimestamp(refreshExpiresAt),
		},
	}, nil
}

func (s *Service) presentCurrentUser(
	ctx context.Context,
	record CurrentUserRecord,
) (CurrentUserDTO, error) {
	var avatar *ArtworkDTO
	if record.Avatar != nil {
		url, err := s.artworkURLs.PresignedGet(ctx, record.Avatar.ObjectKey, s.artworkURLTTL)
		if err != nil {
			return CurrentUserDTO{}, fmt.Errorf("create avatar download URL: %w", err)
		}
		expiresAt := formatTimestamp(s.clock.Now().UTC().Add(s.artworkURLTTL))
		cacheVersion := strconv.FormatInt(record.Avatar.UpdatedAt.UnixMilli(), 10)
		if record.Avatar.ChecksumSHA256 != nil {
			cacheVersion = *record.Avatar.ChecksumSHA256
		}
		avatar = &ArtworkDTO{
			AssetID:   record.Avatar.ID,
			URL:       url,
			CacheKey:  record.Avatar.ID + ":" + cacheVersion,
			MimeType:  record.Avatar.MimeType,
			ExpiresAt: &expiresAt,
			Width:     record.Avatar.Width,
			Height:    record.Avatar.Height,
		}
	}
	updatedAt := record.User.UpdatedAt
	if record.ProfileUpdatedAt.After(updatedAt) {
		updatedAt = record.ProfileUpdatedAt
	}
	return CurrentUserDTO{
		ID:          record.User.ID,
		Username:    record.User.Username,
		DisplayName: record.DisplayName,
		Bio:         record.Bio,
		Avatar:      avatar,
		Role:        record.User.Role,
		Status:      record.User.Status,
		Version:     record.User.Version,
		CreatedAt:   formatTimestamp(record.User.CreatedAt),
		UpdatedAt:   formatTimestamp(updatedAt),
	}, nil
}

func refreshRequestHash(tokenHash string) string {
	digest := sha256.Sum256([]byte(`{"refreshTokenHash":"` + tokenHash + `"}`))
	return hex.EncodeToString(digest[:])
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

type SecurityPasswordManager struct{}

func (SecurityPasswordManager) Hash(password string) (string, error) {
	return security.HashPassword(password)
}

func (SecurityPasswordManager) Verify(password, encoded string) (bool, error) {
	return security.VerifyPassword(password, encoded)
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}
