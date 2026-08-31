package adminmanagement

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type PasswordHasher interface {
	Hash(string) (string, error)
}

type ServiceDependencies struct {
	Store     Store
	Artworks  ArtworkPresenter
	Passwords PasswordHasher
}

type Service struct {
	store     Store
	artworks  ArtworkPresenter
	passwords PasswordHasher
	cursors   *pagination.CursorCodec
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	return NewServiceWithOptions(dependencies, nil)
}

func NewServiceWithOptions(dependencies ServiceDependencies, cursors *pagination.CursorCodec) (*Service, error) {
	if dependencies.Store == nil {
		return nil, errors.New("admin management store is required")
	}
	if dependencies.Artworks == nil {
		return nil, errors.New("admin management artwork presenter is required")
	}
	if dependencies.Passwords == nil {
		return nil, errors.New("admin management password hasher is required")
	}
	return &Service{
		store: dependencies.Store, artworks: dependencies.Artworks, passwords: dependencies.Passwords,
		cursors: cursors,
	}, nil
}

func (service *Service) Dashboard(ctx context.Context) (DashboardDTO, error) {
	counts, err := service.store.Dashboard(ctx)
	if err != nil {
		return DashboardDTO{}, err
	}
	result := DashboardDTO{
		Sources: nonNilCounts(counts.Sources),
		Jobs:    nonNilCounts(counts.Jobs),
	}
	result.Users.Total = counts.UsersTotal
	result.Users.Active = counts.UsersActive
	result.Users.Administrators = counts.UsersAdministrators
	result.Catalog.Artists = counts.Artists
	result.Catalog.Albums = counts.Albums
	result.Catalog.Tracks = nonNilCounts(counts.Tracks)
	return result, nil
}

func (service *Service) ListUsers(ctx context.Context, input ListUsersInput) (UserPageDTO, error) {
	query := strings.TrimSpace(input.Query)
	input.Query = query
	if javascriptStringLength(query) > 100 || !validRoleFilter(input.Role) || !validStatusFilter(input.Status) {
		return UserPageDTO{}, apperror.Validation("User filters are invalid")
	}
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, 25)
		if err != nil {
			return UserPageDTO{}, err
		}
		if service.cursors == nil {
			return UserPageDTO{}, errors.New("admin management cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return UserPageDTO{}, apperror.Validation("cursor is required for deep user pages")
		}
		scope := userCursorScope(input)
		after, err := decodeUserCursor(service.cursors, scope, input.Cursor)
		if err != nil {
			return UserPageDTO{}, err
		}
		records, total, err := service.store.ListUsers(ctx, ListUsersQuery{
			Search: query, Role: input.Role, Status: input.Status,
			Limit: pagination.CursorLimit(page.Page, page.PageSize), After: after, CursorMode: true,
			TotalHint: userCursorTotalHint(after),
		})
		if err != nil {
			return UserPageDTO{}, err
		}
		hasNext := len(records) > page.PageSize
		if len(records) > page.PageSize {
			records = records[:page.PageSize]
		}
		artworks, err := service.presentArtworks(ctx, records)
		if err != nil {
			return UserPageDTO{}, err
		}
		items := make([]UserDTO, 0, len(records))
		for _, record := range records {
			items = append(items, presentUser(record, artworks))
		}
		result := UserPageDTO{
			Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
			TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
		}
		if hasNext && len(records) > 0 {
			next, err := encodeUserCursor(service.cursors, scope, records[len(records)-1], total)
			if err != nil {
				return UserPageDTO{}, err
			}
			result.NextCursor = &next
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return UserPageDTO{}, err
	}
	records, total, err := service.store.ListUsers(ctx, ListUsersQuery{
		Search: query, Role: input.Role, Status: input.Status, Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return UserPageDTO{}, err
	}
	artworks, err := service.presentArtworks(ctx, records)
	if err != nil {
		return UserPageDTO{}, err
	}
	items := make([]UserDTO, 0, len(records))
	for _, record := range records {
		items = append(items, presentUser(record, artworks))
	}
	return UserPageDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func (service *Service) User(ctx context.Context, userID string, input SessionPageInput) (UserDetailDTO, error) {
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, 25)
		if err != nil {
			return UserDetailDTO{}, err
		}
		if service.cursors == nil {
			return UserDetailDTO{}, errors.New("admin management cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return UserDetailDTO{}, apperror.Validation("cursor is required for deep session pages")
		}
		scope := sessionCursorScope(userID)
		after, err := decodeSessionCursor(service.cursors, scope, input.Cursor)
		if err != nil {
			return UserDetailDTO{}, err
		}
		record, sessions, total, err := service.store.FindUser(ctx, userID, SessionQuery{
			Limit: pagination.CursorLimit(page.Page, page.PageSize), After: after, CursorMode: true,
			TotalHint: sessionCursorTotalHint(after),
		})
		if err != nil {
			return UserDetailDTO{}, err
		}
		hasNext := len(sessions) > page.PageSize
		if len(sessions) > page.PageSize {
			sessions = sessions[:page.PageSize]
		}
		artworks, err := service.presentArtworks(ctx, []UserRecord{record})
		if err != nil {
			return UserDetailDTO{}, err
		}
		result := UserDetailDTO{
			UserDTO: presentUser(record, artworks), Sessions: make([]SessionDTO, 0, len(sessions)),
			SessionPage: page.Page, SessionPageSize: page.PageSize, SessionTotal: total,
			SessionTotalPages: pagination.BoundedTotalPages(total, page.PageSize),
		}
		if hasNext && len(sessions) > 0 {
			next, err := encodeSessionCursor(service.cursors, scope, sessions[len(sessions)-1], total)
			if err != nil {
				return UserDetailDTO{}, err
			}
			result.NextSessionCursor = &next
		}
		for _, session := range sessions {
			result.Sessions = append(result.Sessions, presentSession(session))
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return UserDetailDTO{}, err
	}
	record, sessions, total, err := service.store.FindUser(ctx, userID, SessionQuery{
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return UserDetailDTO{}, err
	}
	artworks, err := service.presentArtworks(ctx, []UserRecord{record})
	if err != nil {
		return UserDetailDTO{}, err
	}
	result := UserDetailDTO{
		UserDTO: presentUser(record, artworks), Sessions: make([]SessionDTO, 0, len(sessions)),
		SessionPage: page.Page, SessionPageSize: page.PageSize, SessionTotal: total,
		SessionTotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}
	for _, session := range sessions {
		result.Sessions = append(result.Sessions, presentSession(session))
	}
	return result, nil
}

func presentSession(session SessionRecord) SessionDTO {
	var revokedAt *string
	if session.RevokedAt != nil {
		value := formatTimestamp(*session.RevokedAt)
		revokedAt = &value
	}
	return SessionDTO{
		ID: session.ID, InstallationID: session.InstallationID, DeviceName: session.DeviceName,
		Platform: session.Platform, AppVersion: session.AppVersion, Active: session.RevokedAt == nil,
		CreatedAt: formatTimestamp(session.CreatedAt), LastSeenAt: formatTimestamp(session.LastSeenAt),
		RevokedAt: revokedAt,
	}
}

func (service *Service) CreateUser(ctx context.Context, input CreateUserInput) (UserDetailDTO, error) {
	username := strings.TrimSpace(input.Username)
	if !usernamePattern.MatchString(username) {
		return UserDetailDTO{}, apperror.Validation("username is invalid")
	}
	if err := validatePassword(input.Password); err != nil {
		return UserDetailDTO{}, err
	}
	displayName, err := requiredText(input.DisplayName, 100, "displayName")
	if err != nil {
		return UserDetailDTO{}, err
	}
	if !validRole(input.Role) {
		return UserDetailDTO{}, apperror.Validation("role is invalid")
	}
	passwordHash, err := service.passwords.Hash(input.Password)
	if err != nil {
		return UserDetailDTO{}, fmt.Errorf("hash managed user password: %w", err)
	}
	userID, err := service.store.CreateUser(ctx, CreateUserParams{
		Username: username, NormalizedUsername: normalizeUsername(username),
		PasswordHash: passwordHash, DisplayName: displayName, Role: input.Role,
	})
	if isUniqueViolation(err) {
		return UserDetailDTO{}, duplicateUsernameError()
	}
	if err != nil {
		return UserDetailDTO{}, err
	}
	return service.User(ctx, userID, SessionPageInput{})
}

func (service *Service) UpdateUser(
	ctx context.Context,
	actorID, userID string,
	input UpdateUserInput,
) (UserDetailDTO, error) {
	if err := validateUpdateInput(input); err != nil {
		return UserDetailDTO{}, err
	}
	if actorID == userID && ((input.Role.Set && input.Role.Value == RoleUser) ||
		(input.Status.Set && (input.Status.Value == StatusSuspended || input.Status.Value == StatusDeleted))) {
		return UserDetailDTO{}, apperror.Validation("Administrators cannot remove their own administrative access")
	}
	params := UpdateUserParams{UserID: userID, ExpectedVersion: input.ExpectedVersion}
	if input.Username.Set {
		username := strings.TrimSpace(input.Username.Value)
		if !usernamePattern.MatchString(username) {
			return UserDetailDTO{}, apperror.Validation("username is invalid")
		}
		normalized := normalizeUsername(username)
		params.Username = &username
		params.NormalizedUsername = &normalized
	}
	if input.DisplayName.Set {
		displayName, err := requiredText(input.DisplayName.Value, 100, "displayName")
		if err != nil {
			return UserDetailDTO{}, err
		}
		params.DisplayName = &displayName
	}
	if input.Bio.Set {
		bio, err := nullableText(input.Bio.Value, 500, "bio")
		if err != nil {
			return UserDetailDTO{}, err
		}
		params.SetBio = true
		params.Bio = bio
	}
	if input.Role.Set {
		role := input.Role.Value
		params.Role = &role
	}
	if input.Status.Set {
		status := input.Status.Value
		params.Status = &status
	}
	if err := service.store.UpdateUser(ctx, params); isUniqueViolation(err) {
		return UserDetailDTO{}, duplicateUsernameError()
	} else if err != nil {
		return UserDetailDTO{}, err
	}
	return service.User(ctx, userID, SessionPageInput{})
}

func (service *Service) ResetPassword(
	ctx context.Context,
	userID string,
	input PasswordInput,
) (UpdatedDTO, error) {
	if input.ExpectedVersion < 1 {
		return UpdatedDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	if err := validatePassword(input.Password); err != nil {
		return UpdatedDTO{}, err
	}
	passwordHash, err := service.passwords.Hash(input.Password)
	if err != nil {
		return UpdatedDTO{}, fmt.Errorf("hash managed password reset: %w", err)
	}
	if err := service.store.ResetPassword(ctx, userID, input.ExpectedVersion, passwordHash); err != nil {
		return UpdatedDTO{}, err
	}
	return UpdatedDTO{Updated: true}, nil
}

func (service *Service) RevokeSession(
	ctx context.Context,
	userID, sessionID string,
) (RevokedDTO, error) {
	if err := service.store.RevokeSession(ctx, userID, sessionID); err != nil {
		return RevokedDTO{}, err
	}
	return RevokedDTO{Revoked: true}, nil
}

func (service *Service) presentArtworks(ctx context.Context, users []UserRecord) (map[string]catalog.ArtworkDTO, error) {
	assetIDs := make([]string, 0, len(users))
	for _, user := range users {
		if user.AvatarAssetID != nil {
			assetIDs = append(assetIDs, *user.AvatarAssetID)
		}
	}
	return service.artworks.Artworks(ctx, assetIDs)
}

func presentUser(record UserRecord, artworks map[string]catalog.ArtworkDTO) UserDTO {
	var avatar *catalog.ArtworkDTO
	if record.AvatarAssetID != nil {
		if value, exists := artworks[*record.AvatarAssetID]; exists {
			copy := value
			avatar = &copy
		}
	}
	updatedAt := record.UserUpdatedAt
	if record.ProfileUpdatedAt.After(updatedAt) {
		updatedAt = record.ProfileUpdatedAt
	}
	return UserDTO{
		ID: record.ID, Username: record.Username, DisplayName: record.DisplayName, Bio: record.Bio,
		Role: record.Role, Status: record.Status, Version: record.Version, Avatar: avatar,
		CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(updatedAt),
	}
}

func validateUpdateInput(input UpdateUserInput) error {
	if input.ExpectedVersion < 1 {
		return apperror.Validation("expectedVersion is invalid")
	}
	if !input.Username.Set && !input.DisplayName.Set && !input.Bio.Set && !input.Role.Set && !input.Status.Set {
		return apperror.Validation("At least one user field must change")
	}
	if input.Role.Set && !validRole(input.Role.Value) {
		return apperror.Validation("role is invalid")
	}
	if input.Status.Set && !validStatus(input.Status.Value) {
		return apperror.Validation("status is invalid")
	}
	return nil
}

func validatePassword(value string) error {
	length := javascriptStringLength(value)
	if length < 6 || length > 128 {
		return apperror.Validation("password must contain 6 to 128 characters")
	}
	return nil
}

func requiredText(value string, maximum int, field string) (string, error) {
	result := strings.TrimSpace(value)
	if result == "" || javascriptStringLength(result) > maximum {
		return "", apperror.Validation(field + " is invalid")
	}
	return result, nil
}

func nullableText(value *string, maximum int, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	result := strings.TrimSpace(*value)
	if javascriptStringLength(result) > maximum {
		return nil, apperror.Validation(field + " is invalid")
	}
	if result == "" {
		return nil, nil
	}
	return &result, nil
}

func validRole(value UserRole) bool { return value == RoleUser || value == RoleAdmin }

func validRoleFilter(value UserRole) bool { return value == "" || validRole(value) }

func validStatus(value UserStatus) bool {
	return value == StatusActive || value == StatusSuspended || value == StatusDeleted
}

func validStatusFilter(value UserStatus) bool { return value == "" || validStatus(value) }

func normalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
}

func javascriptStringLength(value string) int { return len(utf16.Encode([]rune(value))) }

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func duplicateUsernameError() error {
	return apperror.Conflict(apperror.CodeDuplicateUsername, "Username is already registered", nil)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func nonNilCounts(input map[string]int) map[string]int {
	if input == nil {
		return map[string]int{}
	}
	return input
}

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)
)
