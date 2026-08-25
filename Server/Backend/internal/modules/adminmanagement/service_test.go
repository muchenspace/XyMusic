package adminmanagement

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
)

func TestCreateUserNormalizesHashesAndPresents(t *testing.T) {
	createdAt := time.Date(2026, 7, 16, 2, 0, 0, 0, time.UTC)
	var created CreateUserParams
	store := &managementStoreStub{}
	store.createUser = func(_ context.Context, input CreateUserParams) (string, error) {
		created = input
		return "user-1", nil
	}
	store.findUser = func(context.Context, string, SessionQuery) (UserRecord, []SessionRecord, int, error) {
		return UserRecord{
			ID: "user-1", Username: created.Username, DisplayName: created.DisplayName,
			Role: created.Role, Status: StatusActive, Version: 1,
			CreatedAt: createdAt, UserUpdatedAt: createdAt, ProfileUpdatedAt: createdAt,
		}, []SessionRecord{}, 0, nil
	}
	service := newManagementServiceWithHasher(t, store, managementHasher{hash: "argon-hash"})
	result, err := service.CreateUser(context.Background(), CreateUserInput{
		Username: "Alice_1", Password: "secret1", DisplayName: " Alice ", Role: RoleUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.NormalizedUsername != "alice_1" || created.PasswordHash != "argon-hash" || created.DisplayName != "Alice" {
		t.Fatalf("created = %#v", created)
	}
	if result.ID != "user-1" || result.Sessions == nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestUpdateUserRejectsSelfDemotionBeforePersistence(t *testing.T) {
	called := false
	store := &managementStoreStub{updateUser: func(context.Context, UpdateUserParams) error {
		called = true
		return nil
	}}
	service := newManagementService(t, store)
	_, err := service.UpdateUser(context.Background(), "admin-1", "admin-1", UpdateUserInput{
		ExpectedVersion: 1, Role: OptionalRole{Set: true, Value: RoleUser},
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) || called {
		t.Fatalf("error/called = %v / %v", err, called)
	}
}

func TestUpdateUserNormalizesOptionalFields(t *testing.T) {
	now := time.Now().UTC()
	var updated UpdateUserParams
	store := &managementStoreStub{}
	store.updateUser = func(_ context.Context, input UpdateUserParams) error { updated = input; return nil }
	store.findUser = func(context.Context, string, SessionQuery) (UserRecord, []SessionRecord, int, error) {
		return UserRecord{ID: "user-1", Username: "Alice_2", DisplayName: "Alice", Role: RoleAdmin,
			Status: StatusActive, Version: 2, CreatedAt: now, UserUpdatedAt: now, ProfileUpdatedAt: now}, nil, 0, nil
	}
	service := newManagementService(t, store)
	emptyBio := "  "
	_, err := service.UpdateUser(context.Background(), "admin-1", "user-1", UpdateUserInput{
		ExpectedVersion: 1,
		Username:        OptionalString{Set: true, Value: "Alice_2"},
		Bio:             OptionalNullableString{Set: true, Value: &emptyBio},
		Role:            OptionalRole{Set: true, Value: RoleAdmin},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NormalizedUsername == nil || *updated.NormalizedUsername != "alice_2" || !updated.SetBio || updated.Bio != nil {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestUserPaginatesSessionsAndReturnsTotals(t *testing.T) {
	now := time.Now().UTC()
	var query SessionQuery
	store := &managementStoreStub{findUser: func(_ context.Context, _ string, input SessionQuery) (UserRecord, []SessionRecord, int, error) {
		query = input
		return UserRecord{ID: "user-1", Username: "user", DisplayName: "User", Role: RoleUser,
				Status: StatusActive, Version: 1, CreatedAt: now, UserUpdatedAt: now, ProfileUpdatedAt: now},
			[]SessionRecord{{ID: "session-1", CreatedAt: now, LastSeenAt: now}}, 41, nil
	}}
	service := newManagementService(t, store)
	result, err := service.User(context.Background(), "user-1", SessionPageInput{Page: 2, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if query.Limit != 20 || query.Offset != 20 || result.SessionPage != 2 || result.SessionPageSize != 20 ||
		result.SessionTotal != 41 || result.SessionTotalPages != 3 || len(result.Sessions) != 1 {
		t.Fatalf("query/result=%#v %#v", query, result)
	}
}

func TestCreateUserMapsWrappedUniqueViolation(t *testing.T) {
	store := &managementStoreStub{createUser: func(context.Context, CreateUserParams) (string, error) {
		return "", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"})
	}}
	service := newManagementService(t, store)
	_, err := service.CreateUser(context.Background(), CreateUserInput{
		Username: "Alice_1", Password: "secret1", DisplayName: "Alice", Role: RoleUser,
	})
	if !apperror.IsCode(err, apperror.CodeDuplicateUsername) {
		t.Fatalf("error = %v", err)
	}
}

func newManagementService(t *testing.T, store Store) *Service {
	t.Helper()
	return newManagementServiceWithHasher(t, store, managementHasher{hash: "hash"})
}

func newManagementServiceWithHasher(t *testing.T, store Store, hasher PasswordHasher) *Service {
	t.Helper()
	service, err := NewService(ServiceDependencies{
		Store: store, Artworks: managementArtworkStub{}, Passwords: hasher,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type managementHasher struct {
	hash string
	err  error
}

func (hasher managementHasher) Hash(string) (string, error) { return hasher.hash, hasher.err }

type managementArtworkStub struct{}

func (managementArtworkStub) Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error) {
	return map[string]catalog.ArtworkDTO{}, nil
}

type managementStoreStub struct {
	dashboard     func(context.Context) (DashboardCounts, error)
	listUsers     func(context.Context, ListUsersQuery) ([]UserRecord, int, error)
	findUser      func(context.Context, string, SessionQuery) (UserRecord, []SessionRecord, int, error)
	createUser    func(context.Context, CreateUserParams) (string, error)
	updateUser    func(context.Context, UpdateUserParams) error
	resetPassword func(context.Context, string, int, string) error
	revokeSession func(context.Context, string, string) error
	updateStatus  func(context.Context, string, string, int, UserStatus) error
}

func (stub *managementStoreStub) Dashboard(ctx context.Context) (DashboardCounts, error) {
	if stub.dashboard == nil {
		return DashboardCounts{}, errors.New("unexpected Dashboard call")
	}
	return stub.dashboard(ctx)
}
func (stub *managementStoreStub) ListUsers(ctx context.Context, query ListUsersQuery) ([]UserRecord, int, error) {
	if stub.listUsers == nil {
		return nil, 0, errors.New("unexpected ListUsers call")
	}
	return stub.listUsers(ctx, query)
}
func (stub *managementStoreStub) FindUser(ctx context.Context, id string, query SessionQuery) (UserRecord, []SessionRecord, int, error) {
	if stub.findUser == nil {
		return UserRecord{}, nil, 0, errors.New("unexpected FindUser call")
	}
	return stub.findUser(ctx, id, query)
}
func (stub *managementStoreStub) CreateUser(ctx context.Context, input CreateUserParams) (string, error) {
	if stub.createUser == nil {
		return "", errors.New("unexpected CreateUser call")
	}
	return stub.createUser(ctx, input)
}
func (stub *managementStoreStub) UpdateUser(ctx context.Context, input UpdateUserParams) error {
	if stub.updateUser == nil {
		return errors.New("unexpected UpdateUser call")
	}
	return stub.updateUser(ctx, input)
}
func (stub *managementStoreStub) ResetPassword(ctx context.Context, id string, version int, hash string) error {
	if stub.resetPassword == nil {
		return errors.New("unexpected ResetPassword call")
	}
	return stub.resetPassword(ctx, id, version, hash)
}
func (stub *managementStoreStub) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if stub.revokeSession == nil {
		return errors.New("unexpected RevokeSession call")
	}
	return stub.revokeSession(ctx, userID, sessionID)
}
func (stub *managementStoreStub) UpdateStatus(ctx context.Context, actorID, userID string, version int, status UserStatus) error {
	if stub.updateStatus == nil {
		return errors.New("unexpected UpdateStatus call")
	}
	return stub.updateStatus(ctx, actorID, userID, version, status)
}
