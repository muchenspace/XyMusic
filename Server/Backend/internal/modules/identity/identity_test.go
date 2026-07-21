package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
)

func TestNormalizeUsernameUsesLegacyNFKCCaseFold(t *testing.T) {
	t.Parallel()
	if actual := NormalizeUsername("  ＡLiＣe  "); actual != "alice" {
		t.Fatalf("NormalizeUsername() = %q, want alice", actual)
	}
}

func TestBearerToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   string
		valid  bool
	}{
		{name: "standard", header: "Bearer abc.def", want: "abc.def", valid: true},
		{name: "case insensitive", header: "bEaReR\tsecret", want: "secret", valid: true},
		{name: "missing token", header: "Bearer ", valid: false},
		{name: "wrong scheme", header: "Basic secret", valid: false},
		{name: "leading whitespace", header: " Bearer secret", valid: false},
		{name: "missing header", header: "", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := BearerToken(test.header)
			if test.valid {
				if err != nil || actual != test.want {
					t.Fatalf("BearerToken(%q) = %q, %v; want %q", test.header, actual, err, test.want)
				}
				return
			}
			if !apperror.IsCode(err, apperror.CodeAuthenticationRequired) {
				t.Fatalf("BearerToken(%q) error = %v, want AUTHENTICATION_REQUIRED", test.header, err)
			}
		})
	}
}

func TestInputValidation(t *testing.T) {
	t.Parallel()
	validDevice := DeviceInfoInput{
		InstallationID: "550e8400-e29b-41d4-a716-446655440000",
		Name:           "Windows PC",
		Platform:       DevicePlatformWindows,
		AppVersion:     "1.2.3",
	}
	tests := []struct {
		name string
		err  error
	}{
		{name: "registration username", err: validateRegistration("two words", "password1")},
		{name: "registration password", err: validateRegistration("alice_1", "short")},
		{name: "login username", err: validateLogin(LoginInput{Username: "a", Password: "password1", Device: validDevice})},
		{name: "login empty password", err: validateLogin(LoginInput{Username: "alice", Device: validDevice})},
		{name: "device uuid", err: validateDevice(DeviceInfoInput{Name: "PC", Platform: DevicePlatformWindows, AppVersion: "1", InstallationID: "not-a-uuid"})},
		{name: "device platform", err: validateDevice(DeviceInfoInput{Name: "PC", Platform: "LINUX", AppVersion: "1", InstallationID: validDevice.InstallationID})},
		{name: "device trimmed name", err: validateDevice(DeviceInfoInput{Name: "   ", Platform: DevicePlatformWindows, AppVersion: "1", InstallationID: validDevice.InstallationID})},
		{name: "refresh token", err: validateRefreshInput("short", "valid-key")},
		{name: "idempotency key", err: validateRefreshInput(strings.Repeat("r", 64), "bad key")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !apperror.IsCode(test.err, apperror.CodeValidationError) {
				t.Fatalf("error = %v, want VALIDATION_ERROR", test.err)
			}
		})
	}
	if err := validateLogin(LoginInput{Username: "alice", Password: "password1", Device: validDevice}); err != nil {
		t.Fatalf("valid login rejected: %v", err)
	}
	if err := validateRefreshInput(strings.Repeat("r", 64), "request-123"); err != nil {
		t.Fatalf("valid refresh rejected: %v", err)
	}
}

func TestAuthSessionDTOJSONCompatibility(t *testing.T) {
	t.Parallel()
	dto := AuthSessionDTO{
		User: CurrentUserDTO{
			ID:          "user-1",
			Username:    "alice",
			DisplayName: "Alice",
			Bio:         nil,
			Avatar:      nil,
			Role:        RoleUser,
			Status:      UserStatusActive,
			Version:     1,
			CreatedAt:   "2026-01-02T03:04:05.000Z",
			UpdatedAt:   "2026-01-02T03:04:05.000Z",
		},
		Session: SessionDTO{ID: "session-1", DeviceName: "PC", CreatedAt: "2026-01-02T03:04:05.000Z"},
		Tokens: TokensDTO{
			TokenType:             "Bearer",
			AccessToken:           "access",
			AccessTokenExpiresAt:  "2026-01-02T03:19:05.000Z",
			RefreshToken:          "refresh",
			RefreshTokenExpiresAt: "2026-02-01T03:04:05.000Z",
		},
	}
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	assertKeys(t, payload, "user", "session", "tokens")
	assertKeys(t, payload["user"].(map[string]any),
		"id", "username", "displayName", "bio", "avatar", "role", "status", "version", "createdAt", "updatedAt")
	assertKeys(t, payload["session"].(map[string]any), "id", "deviceName", "createdAt")
	assertKeys(t, payload["tokens"].(map[string]any),
		"tokenType", "accessToken", "accessTokenExpiresAt", "refreshToken", "refreshTokenExpiresAt")
	if payload["user"].(map[string]any)["bio"] != nil || payload["user"].(map[string]any)["avatar"] != nil {
		t.Fatal("bio and avatar must be explicit JSON null values")
	}
}

func TestRegisterNormalizesAndCreatesProfileBackedUser(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	store.usernameExists = func(_ context.Context, normalized string) (bool, error) {
		if normalized != "alice_1" {
			t.Fatalf("normalized username = %q", normalized)
		}
		return false, nil
	}
	store.createUser = func(_ context.Context, input CreateUserParams) (User, error) {
		if input.Username != "Alice_1" || input.NormalizedUsername != "alice_1" || input.PasswordHash != "hashed:password1" {
			t.Fatalf("unexpected CreateUser input: %#v", input)
		}
		return User{ID: "user-1", Username: input.Username, Status: UserStatusActive}, nil
	}
	service := newTestService(t, store)
	result, err := service.Register(context.Background(), RegisterInput{Username: " Alice_1 ", Password: "password1"})
	if err != nil {
		t.Fatal(err)
	}
	if result != (RegistrationDTO{UserID: "user-1", Username: "Alice_1", Status: UserStatusActive}) {
		t.Fatalf("Register() = %#v", result)
	}
}

func TestRegisterExplainsWhenSelfServiceRegistrationIsDisabled(t *testing.T) {
	t.Parallel()
	service, err := NewService(config.Config{
		Registration: config.Registration{Enabled: false},
		Security:     config.Security{RefreshTokenTTLSeconds: 3600},
		Storage:      config.Storage{SignedURLTTLSeconds: 300},
	}, ServiceDependencies{
		Repository:   &storeStub{},
		AccessTokens: tokenStub{},
		Idempotency:  &idempotencyStub{},
		ArtworkURLs:  artworkSignerStub{},
		Passwords:    fakePasswords{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Register(context.Background(), RegisterInput{Username: "alice_1", Password: "password1"})
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeForbidden {
		t.Fatalf("disabled registration error = %#v", err)
	}
	if applicationError.Detail != "当前服务器未开放用户注册，请联系管理员开启注册功能。" {
		t.Fatalf("disabled registration detail = %q", applicationError.Detail)
	}
}

func TestLoginCreatesCompatibleSessionResponse(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 123_000_000, time.UTC)
	store := &storeStub{}
	user := User{
		ID: "user-1", Username: "Alice", NormalizedUsername: "alice", PasswordHash: "hashed:password1",
		Role: RoleUser, Status: UserStatusActive, AuthVersion: 3, Version: 2,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
	}
	store.findUserByNormalizedUsername = func(_ context.Context, normalized string) (User, error) {
		if normalized != "alice" {
			t.Fatalf("normalized username = %q", normalized)
		}
		return user, nil
	}
	store.createSession = func(_ context.Context, input CreateSessionParams) (AuthSession, error) {
		if input.Device.Name != "Desktop" || input.Device.AppVersion != "2.0" {
			t.Fatalf("device was not trimmed: %#v", input.Device)
		}
		if input.RefreshTokenHash != security.HashSecret(strings.Repeat("t", 64)) {
			t.Fatalf("refresh token hash = %q", input.RefreshTokenHash)
		}
		if input.RefreshTokenFamilyID != "family-1" || !input.RefreshTokenExpiresAt.Equal(now.Add(time.Hour)) {
			t.Fatalf("unexpected refresh metadata: %#v", input)
		}
		return AuthSession{ID: "session-1", UserID: user.ID, DeviceName: input.Device.Name, CreatedAt: now}, nil
	}
	store.findCurrentUser = func(_ context.Context, userID string) (CurrentUserRecord, error) {
		return CurrentUserRecord{User: user, DisplayName: "Alice", ProfileUpdatedAt: now}, nil
	}
	service := newTestServiceWith(t, store, testDependencies{
		clock:       fixedClock{value: now},
		idGenerator: func() string { return "family-1" },
		opaqueToken: func() (string, error) { return strings.Repeat("t", 64), nil },
	})
	result, err := service.Login(context.Background(), LoginInput{
		Username: "  ＡLICE ", Password: "password1",
		Device: DeviceInfoInput{
			InstallationID: "550e8400-e29b-41d4-a716-446655440000",
			Name:           " Desktop ", Platform: DevicePlatformWindows, AppVersion: " 2.0 ",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID != user.ID || result.Session.ID != "session-1" || result.Tokens.TokenType != "Bearer" {
		t.Fatalf("Login() = %#v", result)
	}
	if result.Tokens.AccessToken != "access-token" || result.Tokens.RefreshToken != strings.Repeat("t", 64) {
		t.Fatalf("unexpected token DTO: %#v", result.Tokens)
	}
	if result.Tokens.RefreshTokenExpiresAt != "2026-07-16T09:00:00.123Z" {
		t.Fatalf("refresh expiry = %q", result.Tokens.RefreshTokenExpiresAt)
	}
}

func TestLoginUnknownUserUsesDummyHashAndReturnsGenericCredentialsError(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	store.findUserByNormalizedUsername = func(context.Context, string) (User, error) {
		return User{}, ErrNotFound
	}
	service := newTestService(t, store)
	_, err := service.Login(context.Background(), LoginInput{
		Username: "alice", Password: "wrong-password",
		Device: DeviceInfoInput{
			InstallationID: "550e8400-e29b-41d4-a716-446655440000",
			Name:           "PC",
			Platform:       DevicePlatformWindows,
			AppVersion:     "1.0",
		},
	})
	if !apperror.IsCode(err, apperror.CodeInvalidCredentials) {
		t.Fatalf("Login() error = %v, want INVALID_CREDENTIALS", err)
	}
}

func TestRefreshTokenReuseRevokesFamilyAndSession(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	usedAt := now.Add(-time.Minute)
	token := strings.Repeat("r", 64)
	record := RefreshRecord{
		Token:   RefreshToken{ID: "token-1", FamilyID: "family-1", UsedAt: &usedAt, ExpiresAt: now.Add(time.Hour)},
		Session: AuthSession{ID: "session-1", UserID: "user-1"},
		User:    User{ID: "user-1", Status: UserStatusActive},
	}
	store := &storeStub{}
	store.findRefreshRecord = func(_ context.Context, hash string) (RefreshRecord, error) {
		if hash != security.HashSecret(token) {
			t.Fatalf("token hash = %q", hash)
		}
		return record, nil
	}
	revoked := false
	store.revokeRefreshFamily = func(_ context.Context, familyID, sessionID string, revokedAt time.Time) error {
		revoked = true
		if familyID != "family-1" || sessionID != "session-1" || !revokedAt.Equal(now) {
			t.Fatalf("unexpected family revocation: %s %s %s", familyID, sessionID, revokedAt)
		}
		return nil
	}
	idempotency := &idempotencyStub{}
	service := newTestServiceWith(t, store, testDependencies{clock: fixedClock{value: now}, idempotency: idempotency})
	_, err := service.Refresh(context.Background(), token, "request-123")
	if !apperror.IsCode(err, apperror.CodeSessionRevoked) {
		t.Fatalf("Refresh() error = %v, want SESSION_REVOKED", err)
	}
	if !revoked {
		t.Fatal("refresh token family was not revoked")
	}
	if idempotency.input.ActorID != "user-1" || idempotency.input.Scope != "auth.refresh" || idempotency.input.Key != "request-123" {
		t.Fatalf("unexpected idempotency input: %#v", idempotency.input)
	}
	payload, ok := idempotency.input.Payload.(map[string]string)
	if !ok || payload["refreshTokenHash"] != security.HashSecret(token) {
		t.Fatalf("unexpected idempotency payload: %#v", idempotency.input.Payload)
	}
	if len(idempotency.input.RequestHash) != 64 {
		t.Fatalf("request hash = %q", idempotency.input.RequestHash)
	}
}

func TestRefreshRotatesTokenAndReturnsNewSessionCredentials(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	oldToken := strings.Repeat("r", 64)
	newToken := strings.Repeat("n", 64)
	user := User{
		ID: "user-1", Username: "alice", Role: RoleUser, Status: UserStatusActive,
		AuthVersion: 2, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}
	session := AuthSession{ID: "session-1", UserID: user.ID, DeviceName: "PC", CreatedAt: now.Add(-time.Minute)}
	record := RefreshRecord{
		Token:   RefreshToken{ID: "token-1", SessionID: session.ID, FamilyID: "family-1", ExpiresAt: now.Add(time.Hour)},
		Session: session,
		User:    user,
	}
	store := &storeStub{}
	store.findRefreshRecord = func(_ context.Context, _ string) (RefreshRecord, error) { return record, nil }
	rotated := false
	store.rotateRefreshToken = func(_ context.Context, input RotateRefreshTokenParams) error {
		rotated = true
		if input.TokenID != "token-1" || input.SessionID != "session-1" || input.FamilyID != "family-1" || input.ParentTokenID != "token-1" {
			t.Fatalf("unexpected rotation linkage: %#v", input)
		}
		if input.TokenHash != security.HashSecret(newToken) || !input.ExpiresAt.Equal(now.Add(time.Hour)) || !input.ConsumedAt.Equal(now) {
			t.Fatalf("unexpected rotation token data: %#v", input)
		}
		return nil
	}
	store.findCurrentUser = func(_ context.Context, _ string) (CurrentUserRecord, error) {
		return CurrentUserRecord{User: user, DisplayName: "Alice", ProfileUpdatedAt: now}, nil
	}
	service := newTestServiceWith(t, store, testDependencies{
		clock:       fixedClock{value: now},
		opaqueToken: func() (string, error) { return newToken, nil },
	})
	result, err := service.Refresh(context.Background(), oldToken, "request-123")
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("refresh token was not rotated")
	}
	if result.Replayed || result.Session.Tokens.RefreshToken != newToken || result.Session.Session.ID != session.ID {
		t.Fatalf("Refresh() = %#v", result)
	}
}

func TestAdminRefreshIdempotencyReplaysAndRejectsChangedTokenPayload(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	originalToken := strings.Repeat("r", 64)
	differentToken := strings.Repeat("d", 64)
	rotatedToken := strings.Repeat("n", 64)
	user := User{
		ID: "admin-1", Username: "administrator", Role: RoleAdmin, Status: UserStatusActive,
		AuthVersion: 3, Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}
	session := AuthSession{ID: "session-1", UserID: user.ID, DeviceName: "Admin browser", CreatedAt: now.Add(-time.Minute)}
	recordFor := func(id string) RefreshRecord {
		return RefreshRecord{
			Token:   RefreshToken{ID: id, SessionID: session.ID, FamilyID: "family-1", ExpiresAt: now.Add(time.Hour)},
			Session: session,
			User:    user,
		}
	}
	store := &storeStub{}
	store.findRefreshRecord = func(_ context.Context, hash string) (RefreshRecord, error) {
		switch hash {
		case security.HashSecret(originalToken):
			return recordFor("token-original"), nil
		case security.HashSecret(differentToken):
			return recordFor("token-different"), nil
		default:
			return RefreshRecord{}, ErrNotFound
		}
	}
	rotations := 0
	store.rotateRefreshToken = func(_ context.Context, input RotateRefreshTokenParams) error {
		rotations++
		if input.TokenID != "token-original" || input.TokenHash != security.HashSecret(rotatedToken) {
			t.Fatalf("rotation=%+v", input)
		}
		return nil
	}
	store.findCurrentUser = func(_ context.Context, userID string) (CurrentUserRecord, error) {
		if userID != user.ID {
			t.Fatalf("user id=%q", userID)
		}
		return CurrentUserRecord{User: user, DisplayName: "Administrator", ProfileUpdatedAt: now}, nil
	}
	idempotency := newStrictRefreshIdempotencyStub()
	service := newTestServiceWith(t, store, testDependencies{
		clock:       fixedClock{value: now},
		idempotency: idempotency,
		opaqueToken: func() (string, error) { return rotatedToken, nil },
	})

	first, err := service.Refresh(context.Background(), originalToken, "admin-refresh-key")
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Session.User.Role != RoleAdmin || first.Session.Tokens.RefreshToken != rotatedToken {
		t.Fatalf("first=%+v", first)
	}
	replay, err := service.Refresh(context.Background(), originalToken, "admin-refresh-key")
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Session.Tokens.RefreshToken != rotatedToken || rotations != 1 {
		t.Fatalf("replay/rotations=%+v/%d", replay, rotations)
	}
	if _, err := service.Refresh(context.Background(), differentToken, "admin-refresh-key"); !apperror.IsCode(err, apperror.CodeIdempotencyKeyReused) {
		t.Fatalf("conflict error=%v", err)
	}
	if rotations != 1 || len(idempotency.inputs) != 3 {
		t.Fatalf("rotations/idempotency calls=%d/%d", rotations, len(idempotency.inputs))
	}
	for index, input := range idempotency.inputs {
		if input.ActorID != user.ID || input.Scope != "auth.refresh" || input.Key != "admin-refresh-key" {
			t.Fatalf("input[%d]=%+v", index, input)
		}
		payload, ok := input.Payload.(map[string]string)
		if !ok || len(payload) != 1 || payload["refreshTokenHash"] == "" {
			t.Fatalf("payload[%d]=%#v", index, input.Payload)
		}
	}
	if idempotency.inputs[0].RequestHash != idempotency.inputs[1].RequestHash || idempotency.inputs[0].RequestHash == idempotency.inputs[2].RequestHash {
		t.Fatalf("request hashes=%q/%q/%q", idempotency.inputs[0].RequestHash, idempotency.inputs[1].RequestHash, idempotency.inputs[2].RequestHash)
	}
}

func TestRefreshCASFailureRevokesFamily(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	store := &storeStub{}
	store.findRefreshRecord = func(_ context.Context, _ string) (RefreshRecord, error) {
		return RefreshRecord{
			Token:   RefreshToken{ID: "token-1", FamilyID: "family-1", ExpiresAt: now.Add(time.Hour)},
			Session: AuthSession{ID: "session-1"},
			User:    User{ID: "user-1", Role: RoleUser, Status: UserStatusActive, AuthVersion: 1},
		}, nil
	}
	store.rotateRefreshToken = func(context.Context, RotateRefreshTokenParams) error {
		return ErrRefreshTokenConsumed
	}
	revoked := false
	store.revokeRefreshFamily = func(_ context.Context, _, _ string, _ time.Time) error {
		revoked = true
		return nil
	}
	service := newTestServiceWith(t, store, testDependencies{clock: fixedClock{value: now}})
	_, err := service.Refresh(context.Background(), strings.Repeat("r", 64), "request-123")
	if !apperror.IsCode(err, apperror.CodeSessionRevoked) || !revoked {
		t.Fatalf("Refresh() error = %v, revoked = %v", err, revoked)
	}
}

func TestAuthenticateChecksBackedSessionAndAuthorizationVersion(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	store.findAuthenticatedSession = func(_ context.Context, userID, sessionID string) (User, AuthSession, error) {
		return User{ID: userID, Role: RoleAdmin, Status: UserStatusActive, AuthVersion: 4},
			AuthSession{ID: sessionID, UserID: userID}, nil
	}
	service := newTestServiceWith(t, store, testDependencies{tokens: tokenStub{
		principal: security.Principal{UserID: "user-1", SessionID: "session-1", AuthVersion: 4, Role: "ADMIN"},
	}})
	actor, err := service.Authenticate(context.Background(), "Bearer signed-token")
	if err != nil {
		t.Fatal(err)
	}
	if actor != (AuthenticatedActor{UserID: "user-1", SessionID: "session-1", Role: RoleAdmin, AuthVersion: 4}) {
		t.Fatalf("Authenticate() = %#v", actor)
	}
}

func TestAuthenticateMapsExpiredAndStaleTokens(t *testing.T) {
	t.Parallel()
	t.Run("expired access token", func(t *testing.T) {
		service := newTestServiceWith(t, &storeStub{}, testDependencies{tokens: tokenStub{verifyErr: security.ErrExpiredAccessToken}})
		_, err := service.Authenticate(context.Background(), "Bearer expired")
		if !apperror.IsCode(err, apperror.CodeAccessTokenExpired) {
			t.Fatalf("Authenticate() error = %v, want ACCESS_TOKEN_EXPIRED", err)
		}
	})
	t.Run("authorization version changed", func(t *testing.T) {
		store := &storeStub{}
		store.findAuthenticatedSession = func(context.Context, string, string) (User, AuthSession, error) {
			return User{ID: "user-1", Role: RoleUser, Status: UserStatusActive, AuthVersion: 2},
				AuthSession{ID: "session-1", UserID: "user-1"}, nil
		}
		service := newTestServiceWith(t, store, testDependencies{tokens: tokenStub{principal: security.Principal{
			UserID: "user-1", SessionID: "session-1", Role: "USER", AuthVersion: 1,
		}}})
		_, err := service.Authenticate(context.Background(), "Bearer stale")
		if !apperror.IsCode(err, apperror.CodeSessionRevoked) {
			t.Fatalf("Authenticate() error = %v, want SESSION_REVOKED", err)
		}
	})
}

func TestCurrentUserPresentsAvatarAndLaterUpdateTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	checksum := strings.Repeat("a", 64)
	width := 256
	store := &storeStub{}
	store.findCurrentUser = func(_ context.Context, _ string) (CurrentUserRecord, error) {
		return CurrentUserRecord{
			User: User{
				ID: "user-1", Username: "alice", Role: RoleUser, Status: UserStatusActive,
				Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
			},
			DisplayName: "Alice", ProfileUpdatedAt: now,
			Avatar: &AvatarAsset{
				ID: "asset-1", MimeType: "image/webp",
				ChecksumSHA256: &checksum, Width: &width, UpdatedAt: now.Add(-time.Hour),
			},
		}, nil
	}
	service := newTestServiceWith(t, store, testDependencies{clock: fixedClock{value: now}})
	result, err := service.CurrentUser(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Avatar == nil || result.Avatar.URL != "https://objects.example/artwork/asset-1" {
		t.Fatalf("avatar = %#v", result.Avatar)
	}
	if result.Avatar.CacheKey != "asset-1:"+checksum {
		t.Fatalf("avatar metadata = %#v", result.Avatar)
	}
	if result.Avatar.ExpiresAt != nil {
		t.Fatalf("stable avatar must not expire: %#v", result.Avatar)
	}
	if result.UpdatedAt != "2026-07-16T08:00:00.000Z" {
		t.Fatalf("updatedAt = %q", result.UpdatedAt)
	}
}

func TestLogoutAndLogoutAllUseActorScope(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	store := &storeStub{}
	logoutCalled := false
	store.revokeSession = func(_ context.Context, userID, sessionID string, at time.Time) error {
		logoutCalled = userID == "user-1" && sessionID == "session-1" && at.Equal(now)
		return nil
	}
	logoutAllCalled := false
	store.revokeAllSessions = func(_ context.Context, userID string, at time.Time) error {
		logoutAllCalled = userID == "user-1" && at.Equal(now)
		return nil
	}
	service := newTestServiceWith(t, store, testDependencies{clock: fixedClock{value: now}})
	actor := AuthenticatedActor{UserID: "user-1", SessionID: "session-1"}
	if err := service.Logout(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
	if err := service.LogoutAll(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
	if !logoutCalled || !logoutAllCalled {
		t.Fatalf("logout called = %v, logout-all called = %v", logoutCalled, logoutAllCalled)
	}
}

type storeStub struct {
	usernameExists               func(context.Context, string) (bool, error)
	createUser                   func(context.Context, CreateUserParams) (User, error)
	findUserByNormalizedUsername func(context.Context, string) (User, error)
	createSession                func(context.Context, CreateSessionParams) (AuthSession, error)
	findRefreshRecord            func(context.Context, string) (RefreshRecord, error)
	rotateRefreshToken           func(context.Context, RotateRefreshTokenParams) error
	revokeRefreshFamily          func(context.Context, string, string, time.Time) error
	findAuthenticatedSession     func(context.Context, string, string) (User, AuthSession, error)
	revokeSession                func(context.Context, string, string, time.Time) error
	revokeAllSessions            func(context.Context, string, time.Time) error
	findCurrentUser              func(context.Context, string) (CurrentUserRecord, error)
}

func (s *storeStub) UsernameExists(ctx context.Context, value string) (bool, error) {
	if s.usernameExists == nil {
		return false, errors.New("unexpected UsernameExists call")
	}
	return s.usernameExists(ctx, value)
}

func (s *storeStub) CreateUser(ctx context.Context, input CreateUserParams) (User, error) {
	if s.createUser == nil {
		return User{}, errors.New("unexpected CreateUser call")
	}
	return s.createUser(ctx, input)
}

func (s *storeStub) FindUserByNormalizedUsername(ctx context.Context, value string) (User, error) {
	if s.findUserByNormalizedUsername == nil {
		return User{}, errors.New("unexpected FindUserByNormalizedUsername call")
	}
	return s.findUserByNormalizedUsername(ctx, value)
}

func (s *storeStub) CreateSession(ctx context.Context, input CreateSessionParams) (AuthSession, error) {
	if s.createSession == nil {
		return AuthSession{}, errors.New("unexpected CreateSession call")
	}
	return s.createSession(ctx, input)
}

func (s *storeStub) FindRefreshRecord(ctx context.Context, hash string) (RefreshRecord, error) {
	if s.findRefreshRecord == nil {
		return RefreshRecord{}, errors.New("unexpected FindRefreshRecord call")
	}
	return s.findRefreshRecord(ctx, hash)
}

func (s *storeStub) RotateRefreshToken(ctx context.Context, input RotateRefreshTokenParams) error {
	if s.rotateRefreshToken == nil {
		return errors.New("unexpected RotateRefreshToken call")
	}
	return s.rotateRefreshToken(ctx, input)
}

func (s *storeStub) RevokeRefreshFamily(ctx context.Context, familyID, sessionID string, at time.Time) error {
	if s.revokeRefreshFamily == nil {
		return errors.New("unexpected RevokeRefreshFamily call")
	}
	return s.revokeRefreshFamily(ctx, familyID, sessionID, at)
}

func (s *storeStub) FindAuthenticatedSession(ctx context.Context, userID, sessionID string) (User, AuthSession, error) {
	if s.findAuthenticatedSession == nil {
		return User{}, AuthSession{}, errors.New("unexpected FindAuthenticatedSession call")
	}
	return s.findAuthenticatedSession(ctx, userID, sessionID)
}

func (s *storeStub) RevokeSession(ctx context.Context, userID, sessionID string, at time.Time) error {
	if s.revokeSession == nil {
		return errors.New("unexpected RevokeSession call")
	}
	return s.revokeSession(ctx, userID, sessionID, at)
}

func (s *storeStub) RevokeAllSessions(ctx context.Context, userID string, at time.Time) error {
	if s.revokeAllSessions == nil {
		return errors.New("unexpected RevokeAllSessions call")
	}
	return s.revokeAllSessions(ctx, userID, at)
}

func (s *storeStub) FindCurrentUser(ctx context.Context, userID string) (CurrentUserRecord, error) {
	if s.findCurrentUser == nil {
		return CurrentUserRecord{}, errors.New("unexpected FindCurrentUser call")
	}
	return s.findCurrentUser(ctx, userID)
}

type fakePasswords struct{}

func (fakePasswords) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func (fakePasswords) Verify(password, encoded string) (bool, error) {
	return encoded == "hashed:"+password, nil
}

type tokenStub struct {
	principal security.Principal
	verifyErr error
}

func (t tokenStub) Issue(principal security.Principal) (string, time.Time, error) {
	return "access-token", time.Date(2026, time.July, 16, 8, 15, 0, 123_000_000, time.UTC), nil
}

func (t tokenStub) Verify(string) (security.Principal, error) {
	return t.principal, t.verifyErr
}

type idempotencyStub struct {
	input    RefreshIdempotencyInput
	replayed bool
}

type strictRefreshIdempotencyRecord struct {
	requestHash string
	session     AuthSessionDTO
}

type strictRefreshIdempotencyStub struct {
	inputs  []RefreshIdempotencyInput
	records map[string]strictRefreshIdempotencyRecord
}

func newStrictRefreshIdempotencyStub() *strictRefreshIdempotencyStub {
	return &strictRefreshIdempotencyStub{records: make(map[string]strictRefreshIdempotencyRecord)}
}

func (stub *strictRefreshIdempotencyStub) ExecuteRefresh(
	ctx context.Context,
	input RefreshIdempotencyInput,
	operation RefreshOperation,
) (AuthSessionDTO, bool, error) {
	stub.inputs = append(stub.inputs, input)
	key := input.ActorID + "\x00" + input.Scope + "\x00" + input.Key
	if record, exists := stub.records[key]; exists {
		if record.requestHash != input.RequestHash {
			return AuthSessionDTO{}, false, apperror.Conflict(
				apperror.CodeIdempotencyKeyReused,
				"idempotency key payload changed",
				nil,
			)
		}
		return record.session, true, nil
	}
	session, err := operation(ctx)
	if err != nil {
		return AuthSessionDTO{}, false, err
	}
	stub.records[key] = strictRefreshIdempotencyRecord{requestHash: input.RequestHash, session: session}
	return session, false, nil
}

func (s *idempotencyStub) ExecuteRefresh(
	ctx context.Context,
	input RefreshIdempotencyInput,
	operation RefreshOperation,
) (AuthSessionDTO, bool, error) {
	s.input = input
	result, err := operation(ctx)
	return result, s.replayed, err
}

type artworkSignerStub struct{}

func (artworkSignerStub) PresentArtwork(
	assetID string,
	checksumSHA256 *string,
	updatedAt time.Time,
) (string, string, error) {
	version := updatedAt.UTC().Format("20060102150405")
	if checksumSHA256 != nil {
		version = *checksumSHA256
	}
	return "https://objects.example/artwork/" + assetID, assetID + ":" + version, nil
}

type fixedClock struct {
	value time.Time
}

func (c fixedClock) Now() time.Time {
	return c.value
}

type testDependencies struct {
	clock       Clock
	tokens      AccessTokenManager
	idempotency RefreshIdempotency
	idGenerator func() string
	opaqueToken func() (string, error)
}

func newTestService(t *testing.T, store Store) *Service {
	t.Helper()
	return newTestServiceWith(t, store, testDependencies{})
}

func newTestServiceWith(t *testing.T, store Store, overrides testDependencies) *Service {
	t.Helper()
	if overrides.clock == nil {
		overrides.clock = fixedClock{value: time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)}
	}
	if overrides.tokens == nil {
		overrides.tokens = tokenStub{}
	}
	if overrides.idempotency == nil {
		overrides.idempotency = &idempotencyStub{}
	}
	if overrides.idGenerator == nil {
		overrides.idGenerator = func() string { return "generated-id" }
	}
	if overrides.opaqueToken == nil {
		overrides.opaqueToken = func() (string, error) { return strings.Repeat("o", 64), nil }
	}
	service, err := NewService(config.Config{
		Registration: config.Registration{Enabled: true},
		Security:     config.Security{RefreshTokenTTLSeconds: 3600},
		Storage:      config.Storage{SignedURLTTLSeconds: 300},
	}, ServiceDependencies{
		Repository:   store,
		AccessTokens: overrides.tokens,
		Idempotency:  overrides.idempotency,
		ArtworkURLs:  artworkSignerStub{},
		Passwords:    fakePasswords{},
		Clock:        overrides.clock,
		IDGenerator:  overrides.idGenerator,
		OpaqueToken:  overrides.opaqueToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertKeys(t *testing.T, value map[string]any, expected ...string) {
	t.Helper()
	if len(value) != len(expected) {
		t.Fatalf("keys = %#v, want %#v", value, expected)
	}
	for _, key := range expected {
		if _, exists := value[key]; !exists {
			t.Fatalf("missing key %q in %#v", key, value)
		}
	}
}
