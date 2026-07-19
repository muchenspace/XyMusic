package adminmutation

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
)

type Service struct {
	store                   Store
	artworks                ArtworkPresenter
	defaultLibraryDirectory string
}

func NewService(store Store, artworks ArtworkPresenter, defaultLibraryDirectory string) (*Service, error) {
	if store == nil {
		return nil, errors.New("admin mutation store is required")
	}
	if artworks == nil {
		return nil, errors.New("admin mutation artwork presenter is required")
	}
	if strings.TrimSpace(defaultLibraryDirectory) == "" {
		return nil, errors.New("admin mutation library directory is required")
	}
	return &Service{store: store, artworks: artworks, defaultLibraryDirectory: defaultLibraryDirectory}, nil
}

func (service *Service) CreateArtist(ctx context.Context, actorID, traceID string, input CreateArtistInput) (ArtistDTO, error) {
	name, err := requireText(input.Name, 200, "name")
	if err != nil {
		return ArtistDTO{}, err
	}
	var description *string
	if input.Description.Set {
		description, err = nullableText(input.Description.Value, 5_000, "description")
		if err != nil {
			return ArtistDTO{}, err
		}
	}
	id, err := service.store.CreateArtist(ctx, CreateArtistParams{
		Name: name, NormalizedName: normalize(name), SetDescription: input.Description.Set, Description: description,
	})
	if err != nil {
		return ArtistDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.artist.create", "artist", id, traceID, nil); err != nil {
		return ArtistDTO{}, err
	}
	return service.artist(ctx, id)
}

func (service *Service) UpdateArtist(ctx context.Context, actorID, traceID, id string, input UpdateArtistInput) (ArtistDTO, error) {
	if input.ExpectedVersion < 1 || (!input.Name.Set && !input.Description.Set) {
		return ArtistDTO{}, apperror.Validation("At least one field must change")
	}
	params := UpdateArtistParams{ID: id, ExpectedVersion: input.ExpectedVersion, SetDescription: input.Description.Set}
	if input.Name.Set {
		name, err := requireText(input.Name.Value, 200, "name")
		if err != nil {
			return ArtistDTO{}, err
		}
		normalized := normalize(name)
		params.Name, params.NormalizedName = &name, &normalized
	}
	if input.Description.Set {
		description, err := nullableText(input.Description.Value, 5_000, "description")
		if err != nil {
			return ArtistDTO{}, err
		}
		params.Description = description
	}
	if err := service.store.UpdateArtist(ctx, params); err != nil {
		return ArtistDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.artist.update", "artist", id, traceID, nil); err != nil {
		return ArtistDTO{}, err
	}
	return service.artist(ctx, id)
}

func (service *Service) CreateAlbum(ctx context.Context, actorID, traceID string, input CreateAlbumInput) (AlbumDTO, error) {
	title, err := requireText(input.Title, 300, "title")
	if err != nil {
		return AlbumDTO{}, err
	}
	credits, err := service.validateCredits(ctx, input.ArtistCredits)
	if err != nil {
		return AlbumDTO{}, err
	}
	releaseDate, err := optionalDate(input.ReleaseDate)
	if err != nil {
		return AlbumDTO{}, err
	}
	description, err := optionalText(input.Description, 5_000, "description")
	if err != nil {
		return AlbumDTO{}, err
	}
	id, err := service.store.CreateAlbum(ctx, CreateAlbumParams{
		Title: title, NormalizedTitle: normalize(title), Credits: credits,
		SetReleaseDate: input.ReleaseDate.Set, ReleaseDate: releaseDate,
		SetDescription: input.Description.Set, Description: description,
	})
	if err != nil {
		return AlbumDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.album.create", "album", id, traceID, nil); err != nil {
		return AlbumDTO{}, err
	}
	return service.album(ctx, id)
}

func (service *Service) UpdateAlbum(ctx context.Context, actorID, traceID, id string, input UpdateAlbumInput) (AlbumDTO, error) {
	if input.ExpectedVersion < 1 || (!input.Title.Set && !input.ArtistCredits.Set && !input.ReleaseDate.Set && !input.Description.Set) {
		return AlbumDTO{}, apperror.Validation("At least one field must change")
	}
	params := UpdateAlbumParams{ID: id, ExpectedVersion: input.ExpectedVersion,
		SetCredits: input.ArtistCredits.Set, SetReleaseDate: input.ReleaseDate.Set, SetDescription: input.Description.Set}
	var err error
	if input.Title.Set {
		title, err := requireText(input.Title.Value, 300, "title")
		if err != nil {
			return AlbumDTO{}, err
		}
		normalized := normalize(title)
		params.Title, params.NormalizedTitle = &title, &normalized
	}
	if input.ArtistCredits.Set {
		params.Credits, err = service.validateCredits(ctx, input.ArtistCredits.Values)
		if err != nil {
			return AlbumDTO{}, err
		}
	}
	params.ReleaseDate, err = optionalDate(input.ReleaseDate)
	if err != nil {
		return AlbumDTO{}, err
	}
	params.Description, err = optionalText(input.Description, 5_000, "description")
	if err != nil {
		return AlbumDTO{}, err
	}
	if err := service.store.UpdateAlbum(ctx, params); err != nil {
		return AlbumDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.album.update", "album", id, traceID, nil); err != nil {
		return AlbumDTO{}, err
	}
	return service.album(ctx, id)
}

func (service *Service) MergeAlbums(ctx context.Context, actorID, traceID string, input MergeAlbumsInput) (MergeResultDTO, error) {
	if len(input.Sources) < 1 || len(input.Sources) > 100 {
		return MergeResultDTO{}, apperror.Validation("sources must contain 1 to 100 albums")
	}
	participants := map[string]struct{}{input.Target.AlbumID: {}}
	if input.Target.ExpectedVersion < 1 {
		return MergeResultDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	for _, source := range input.Sources {
		if source.ExpectedVersion < 1 || source.AlbumID == input.Target.AlbumID {
			return MergeResultDTO{}, apperror.Validation("Album merge sources must be unique and different from the target")
		}
		if _, duplicate := participants[source.AlbumID]; duplicate {
			return MergeResultDTO{}, apperror.Validation("Album merge sources must be unique and different from the target")
		}
		participants[source.AlbumID] = struct{}{}
	}
	fieldSources := []*string{&input.FieldSources.Title, &input.FieldSources.ArtistCredits, input.FieldSources.Cover.Value, input.FieldSources.ReleaseDate.Value, input.FieldSources.Description.Value}
	for _, source := range fieldSources {
		if source != nil {
			if _, exists := participants[*source]; !exists {
				return MergeResultDTO{}, apperror.Validation("Every merge field source must be one of the participating albums")
			}
		}
	}
	return service.store.MergeAlbums(ctx, actorID, traceID, input)
}

func (service *Service) CreateTrack(ctx context.Context, actorID, traceID string, input CreateTrackInput) (TrackDTO, error) {
	title, err := requireText(input.Title, 300, "title")
	if err != nil {
		return TrackDTO{}, err
	}
	credits, err := service.validateCredits(ctx, input.ArtistCredits)
	if err != nil {
		return TrackDTO{}, err
	}
	if input.AlbumID.Value != nil {
		if err := service.requireAlbum(ctx, *input.AlbumID.Value); err != nil {
			return TrackDTO{}, err
		}
	}
	if input.TrackNumber.Value != nil && *input.TrackNumber.Value < 1 {
		return TrackDTO{}, apperror.Validation("trackNumber must be positive")
	}
	if input.DiscNumber.Value < 1 {
		return TrackDTO{}, apperror.Validation("discNumber must be positive")
	}
	id, err := service.store.CreateTrack(ctx, CreateTrackParams{
		Title: title, NormalizedTitle: normalize(title), AlbumID: input.AlbumID.Value,
		Credits: credits, TrackNumber: input.TrackNumber.Value, DiscNumber: input.DiscNumber.Value,
	})
	if err != nil {
		return TrackDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.track.create", "track", id, traceID, nil); err != nil {
		return TrackDTO{}, err
	}
	return service.track(ctx, id)
}

func (service *Service) UpdateTrack(ctx context.Context, actorID, traceID, id string, input UpdateTrackInput) (TrackDTO, error) {
	if input.ExpectedVersion < 1 || (!input.Title.Set && !input.AlbumID.Set && !input.ArtistCredits.Set && !input.TrackNumber.Set && !input.DiscNumber.Set) {
		return TrackDTO{}, apperror.Validation("At least one field must change")
	}
	params := UpdateTrackParams{ID: id, ExpectedVersion: input.ExpectedVersion,
		SetAlbum: input.AlbumID.Set, AlbumID: input.AlbumID.Value,
		SetCredits: input.ArtistCredits.Set, SetTrackNumber: input.TrackNumber.Set, TrackNumber: input.TrackNumber.Value}
	if input.Title.Set {
		title, err := requireText(input.Title.Value, 300, "title")
		if err != nil {
			return TrackDTO{}, err
		}
		normalized := normalize(title)
		params.Title, params.NormalizedTitle = &title, &normalized
	}
	if input.AlbumID.Set && input.AlbumID.Value != nil {
		if err := service.requireAlbum(ctx, *input.AlbumID.Value); err != nil {
			return TrackDTO{}, err
		}
	}
	if input.ArtistCredits.Set {
		credits, err := service.validateCredits(ctx, input.ArtistCredits.Values)
		if err != nil {
			return TrackDTO{}, err
		}
		params.Credits = credits
	}
	if input.TrackNumber.Set && input.TrackNumber.Value != nil && *input.TrackNumber.Value < 1 {
		return TrackDTO{}, apperror.Validation("trackNumber must be positive")
	}
	if input.DiscNumber.Set {
		if input.DiscNumber.Value < 1 {
			return TrackDTO{}, apperror.Validation("discNumber must be positive")
		}
		params.DiscNumber = &input.DiscNumber.Value
	}
	if err := service.store.UpdateTrack(ctx, params); err != nil {
		return TrackDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.track.update", "track", id, traceID, nil); err != nil {
		return TrackDTO{}, err
	}
	return service.track(ctx, id)
}

func (service *Service) PublishTrack(ctx context.Context, actorID, traceID, id string, expectedVersion int) (TrackDTO, error) {
	if expectedVersion < 1 {
		return TrackDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	if err := service.store.PublishTrack(ctx, id, expectedVersion); err != nil {
		return TrackDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.track.publish", "track", id, traceID, nil); err != nil {
		return TrackDTO{}, err
	}
	return service.track(ctx, id)
}

func (service *Service) ArchiveTrack(ctx context.Context, actorID, traceID, id string, expectedVersion int) (TrackDTO, error) {
	if expectedVersion < 1 {
		return TrackDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	if err := service.store.ArchiveTrack(ctx, id, expectedVersion); err != nil {
		return TrackDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.track.archive", "track", id, traceID, nil); err != nil {
		return TrackDTO{}, err
	}
	return service.track(ctx, id)
}

func (service *Service) RestoreTrack(ctx context.Context, actorID, traceID, id string, expectedVersion int) (TrackDTO, error) {
	if expectedVersion < 1 {
		return TrackDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	if err := service.store.RestoreTrack(ctx, id, expectedVersion); err != nil {
		return TrackDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.track.restore", "track", id, traceID, nil); err != nil {
		return TrackDTO{}, err
	}
	return service.track(ctx, id)
}

func (service *Service) RestoreTracksBatch(
	ctx context.Context,
	actorID string,
	traceID string,
	input BatchTrackMutationInput,
) (BatchRestoreDTO, error) {
	if err := validateBatchTrackItems(input.Items); err != nil {
		return BatchRestoreDTO{}, err
	}
	records, err := service.store.RestoreTracksBatch(ctx, actorID, traceID, input.Items)
	if err != nil {
		return BatchRestoreDTO{}, err
	}
	items := make([]BatchRestoreItemDTO, 0, len(records))
	for _, record := range records {
		items = append(items, BatchRestoreItemDTO{
			TrackID: record.TrackID, Status: record.Status, Version: record.Version,
		})
	}
	return BatchRestoreDTO{Restored: len(items), Items: items}, nil
}

func (service *Service) CreatePermanentDeleteBatch(
	ctx context.Context,
	actorID string,
	traceID string,
	input BatchTrackMutationInput,
) (PermanentDeleteBatchDTO, error) {
	if err := validateBatchTrackItems(input.Items); err != nil {
		return PermanentDeleteBatchDTO{}, err
	}
	job, items, err := service.store.CreatePermanentDeleteBatch(ctx, actorID, traceID, input.Items)
	if err != nil {
		return PermanentDeleteBatchDTO{}, err
	}
	return presentPermanentDeleteBatch(job, items), nil
}

func (service *Service) PermanentDeleteBatch(ctx context.Context, jobID string) (PermanentDeleteBatchDTO, error) {
	job, items, err := service.store.FindPermanentDeleteBatch(ctx, jobID)
	if err != nil {
		return PermanentDeleteBatchDTO{}, err
	}
	return presentPermanentDeleteBatch(job, items), nil
}

func (service *Service) DeleteTrackPermanently(ctx context.Context, actorID, traceID, id string, expectedVersion int) (DeleteTrackDTO, error) {
	if expectedVersion < 1 {
		return DeleteTrackDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	result, err := service.store.DeleteTrackPermanently(ctx, id, expectedVersion, service.defaultLibraryDirectory)
	if err != nil {
		return DeleteTrackDTO{}, err
	}
	details := map[string]any{"deletedFiles": result.DeletedFiles, "quarantinedFiles": result.QuarantinedFiles, "scheduledObjects": result.ScheduledObjects}
	if err := service.audit(ctx, actorID, "admin.track.delete_permanently", "track", id, traceID, details); err != nil {
		return DeleteTrackDTO{}, err
	}
	return DeleteTrackDTO{Deleted: true, DeletedFiles: result.DeletedFiles, QuarantinedFiles: result.QuarantinedFiles, ScheduledObjects: result.ScheduledObjects}, nil
}

func validateBatchTrackItems(items []BatchTrackItemInput) error {
	if len(items) < 1 || len(items) > 200 {
		return apperror.Validation("A track batch must contain 1 to 200 items")
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		parsedID, err := uuid.Parse(item.TrackID)
		if err != nil || !strings.EqualFold(parsedID.String(), item.TrackID) || item.ExpectedVersion < 1 {
			return apperror.Validation("A track batch item is invalid")
		}
		canonicalID := parsedID.String()
		if _, duplicate := seen[canonicalID]; duplicate {
			return apperror.Validation("Batch track IDs must be unique")
		}
		seen[canonicalID] = struct{}{}
	}
	return nil
}

func presentPermanentDeleteBatch(
	job PermanentDeleteBatchRecord,
	items []PermanentDeleteBatchItemRecord,
) PermanentDeleteBatchDTO {
	presented := make([]PermanentDeleteBatchItemDTO, 0, len(items))
	for _, item := range items {
		presented = append(presented, PermanentDeleteBatchItemDTO{
			ID: item.ID, TrackID: item.TrackID, ExpectedVersion: item.ExpectedVersion,
			Position: item.Position, Status: item.Status, Attempts: item.Attempts,
			DeletedFiles: item.DeletedFiles, QuarantinedFiles: item.QuarantinedFiles,
			ScheduledObjects: item.ScheduledObjects, ErrorCode: item.ErrorCode, Message: item.Message,
			StartedAt: optionalBatchTimestamp(item.StartedAt), CompletedAt: optionalBatchTimestamp(item.CompletedAt),
			CreatedAt: formatTimestamp(item.CreatedAt), UpdatedAt: formatTimestamp(item.UpdatedAt),
		})
	}
	return PermanentDeleteBatchDTO{
		ID: job.ID, Status: job.Status, Total: job.Total, Processed: job.Processed,
		Succeeded: job.Succeeded, Failed: job.Failed,
		CreatedAt: formatTimestamp(job.CreatedAt), UpdatedAt: formatTimestamp(job.UpdatedAt),
		StartedAt: optionalBatchTimestamp(job.StartedAt), CompletedAt: optionalBatchTimestamp(job.CompletedAt),
		Items: presented,
	}
}

func optionalBatchTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTimestamp(*value)
	return &formatted
}

func (service *Service) UpsertLyrics(ctx context.Context, actorID, traceID, trackID string, input LyricsInput) (LyricDTO, error) {
	if input.ExpectedVersion < 1 || (input.Format != "LRC" && input.Format != "PLAIN") {
		return LyricDTO{}, apperror.Validation("Lyrics request is invalid")
	}
	if javascriptLength(input.Content.Value) > 1_000_000 {
		return LyricDTO{}, apperror.Validation("content is too long")
	}
	language, err := requireText(input.Language, 35, "language")
	if err != nil || javascriptLength(language) < 2 {
		return LyricDTO{}, apperror.Validation("language is invalid")
	}
	input.Language = strings.ToLower(language)
	stored, err := service.store.UpsertLyrics(ctx, trackID, input)
	if err != nil {
		return LyricDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.track.lyrics.upsert", "track", trackID, traceID, nil); err != nil {
		return LyricDTO{}, err
	}
	return LyricDTO{ID: stored.ID, TrackID: trackID, Language: stored.Language, Format: stored.Format, Content: stored.Content,
		IsDefault: stored.IsDefault, TrackVersion: stored.TrackVersion, UpdatedAt: formatTimestamp(stored.UpdatedAt)}, nil
}

func (service *Service) UpdateUserStatus(ctx context.Context, actorID, traceID, userID string, input UserStatusInput) (UserStatusDTO, error) {
	if input.ExpectedVersion < 1 || (input.Status != UserActive && input.Status != UserSuspended) {
		return UserStatusDTO{}, apperror.Validation("User status request is invalid")
	}
	reason, err := requireText(input.Reason, 500, "reason")
	if err != nil {
		return UserStatusDTO{}, err
	}
	if actorID == userID && input.Status == UserSuspended {
		return UserStatusDTO{}, apperror.Validation("Administrators cannot suspend their own account")
	}
	if err := service.store.UpdateUserStatus(ctx, actorID, userID, input.ExpectedVersion, input.Status); err != nil {
		return UserStatusDTO{}, err
	}
	if err := service.audit(ctx, actorID, "admin.user.status.update", "user", userID, traceID, map[string]any{"status": input.Status, "reason": reason}); err != nil {
		return UserStatusDTO{}, err
	}
	record, err := service.store.FindUser(ctx, userID)
	if err != nil {
		return UserStatusDTO{}, err
	}
	updatedAt := record.UserUpdatedAt
	if record.ProfileUpdatedAt.After(updatedAt) {
		updatedAt = record.ProfileUpdatedAt
	}
	return UserStatusDTO{ID: record.ID, Username: record.Username, DisplayName: record.DisplayName, Role: record.Role,
		Status: record.Status, Version: record.Version, CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(updatedAt)}, nil
}

func (service *Service) validateCredits(ctx context.Context, input []CreditInput) ([]CreditInput, error) {
	if len(input) < 1 || len(input) > 100 {
		return nil, apperror.Validation("artistCredits must contain 1 to 100 items")
	}
	keys := make(map[string]struct{}, len(input))
	ids := make([]string, 0, len(input))
	seenIDs := make(map[string]struct{})
	for _, credit := range input {
		if credit.ArtistID == "" || !validCreditRole(credit.Role) || credit.SortOrder < 0 {
			return nil, apperror.Validation("artistCredits contains an invalid value")
		}
		key := credit.ArtistID + ":" + string(credit.Role)
		if _, exists := keys[key]; exists {
			return nil, apperror.Validation("artistCredits contains duplicates")
		}
		keys[key] = struct{}{}
		if _, exists := seenIDs[credit.ArtistID]; !exists {
			seenIDs[credit.ArtistID] = struct{}{}
			ids = append(ids, credit.ArtistID)
		}
	}
	exists, err := service.store.ArtistsExist(ctx, ids)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperror.Validation("artistCredits references an unknown artist")
	}
	return input, nil
}

func (service *Service) requireAlbum(ctx context.Context, id string) error {
	exists, err := service.store.AlbumExists(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return apperror.Validation("albumId references an unknown album")
	}
	return nil
}

func (service *Service) artist(ctx context.Context, id string) (ArtistDTO, error) {
	record, err := service.store.FindArtist(ctx, id)
	if err != nil {
		return ArtistDTO{}, err
	}
	artworks, err := service.artworks.Artworks(ctx, optionalID(record.ArtworkAssetID))
	if err != nil {
		return ArtistDTO{}, err
	}
	return ArtistDTO{ID: record.ID, Name: record.Name, Description: record.Description, Artwork: artwork(artworks, record.ArtworkAssetID), Version: record.Version, CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(record.UpdatedAt)}, nil
}
func (service *Service) album(ctx context.Context, id string) (AlbumDTO, error) {
	record, err := service.store.FindAlbum(ctx, id)
	if err != nil {
		return AlbumDTO{}, err
	}
	artworks, err := service.artworks.Artworks(ctx, optionalID(record.CoverAssetID))
	if err != nil {
		return AlbumDTO{}, err
	}
	return AlbumDTO{ID: record.ID, Title: record.Title, ArtistCredits: creditsDTO(record.Credits), ReleaseDate: record.ReleaseDate, Description: record.Description, Cover: artwork(artworks, record.CoverAssetID), Version: record.Version, CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(record.UpdatedAt)}, nil
}
func (service *Service) track(ctx context.Context, id string) (TrackDTO, error) {
	record, err := service.store.FindTrack(ctx, id)
	if err != nil {
		return TrackDTO{}, err
	}
	artworks, err := service.artworks.Artworks(ctx, optionalID(record.AlbumCoverAssetID))
	if err != nil {
		return TrackDTO{}, err
	}
	var album *AlbumReferenceDTO
	if record.AlbumID != nil && record.AlbumTitle != nil {
		album = &AlbumReferenceDTO{ID: *record.AlbumID, Title: *record.AlbumTitle}
	}
	var duration *int64
	if record.DurationMS > 0 {
		v := record.DurationMS
		duration = &v
	}
	disc := 1
	if record.DiscNumber != nil {
		disc = *record.DiscNumber
	}
	return TrackDTO{ID: record.ID, Title: record.Title, Album: album, ArtistCredits: creditsDTO(record.Credits), Artwork: artwork(artworks, record.AlbumCoverAssetID), DurationMS: duration, TrackNumber: record.TrackNumber, DiscNumber: disc, Status: record.Status, ActiveMediaJobID: record.ActiveMediaJobID, Version: record.Version, CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(record.UpdatedAt)}, nil
}

func (service *Service) audit(ctx context.Context, actorID, action, targetType, targetID, traceID string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	return service.store.WriteAudit(ctx, actorID, action, targetType, targetID, traceID, details)
}
func creditsDTO(records []CreditRecord) []CreditDTO {
	result := make([]CreditDTO, 0, len(records))
	for _, r := range records {
		result = append(result, CreditDTO{Artist: catalog.ArtistReferenceDTO{ID: r.ArtistID, Name: r.ArtistName}, Role: r.Role, SortOrder: r.SortOrder})
	}
	return result
}
func optionalID(id *string) []string {
	if id == nil {
		return []string{}
	}
	return []string{*id}
}
func artwork(items map[string]catalog.ArtworkDTO, id *string) *catalog.ArtworkDTO {
	if id == nil {
		return nil
	}
	v, ok := items[*id]
	if !ok {
		return nil
	}
	return &v
}
func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}
func requireText(value string, maximum int, field string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" || javascriptLength(v) > maximum {
		return "", apperror.Validation(field + " is invalid")
	}
	return v, nil
}
func nullableText(value *string, maximum int, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*value)
	if javascriptLength(v) > maximum {
		return nil, apperror.Validation(field + " is too long")
	}
	if v == "" {
		return nil, nil
	}
	return &v, nil
}
func optionalText(input OptionalNullableString, maximum int, field string) (*string, error) {
	if !input.Set {
		return nil, nil
	}
	return nullableText(input.Value, maximum, field)
}
func optionalDate(input OptionalNullableString) (*string, error) {
	if !input.Set || input.Value == nil {
		return input.Value, nil
	}
	parsed, err := time.Parse("2006-01-02", *input.Value)
	if err != nil || parsed.Format("2006-01-02") != *input.Value {
		return nil, apperror.Validation("releaseDate is invalid")
	}
	return input.Value, nil
}
func validCreditRole(role CreditRole) bool {
	switch role {
	case CreditPrimary, CreditFeatured, CreditComposer, CreditLyricist, CreditProducer:
		return true
	}
	return false
}
func javascriptLength(value string) int { return len(utf16.Encode([]rune(value))) }
func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
