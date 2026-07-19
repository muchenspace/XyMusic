package adminmutation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type API interface {
	CreateArtist(context.Context, string, string, CreateArtistInput) (ArtistDTO, error)
	UpdateArtist(context.Context, string, string, string, UpdateArtistInput) (ArtistDTO, error)
	CreateAlbum(context.Context, string, string, CreateAlbumInput) (AlbumDTO, error)
	UpdateAlbum(context.Context, string, string, string, UpdateAlbumInput) (AlbumDTO, error)
	MergeAlbums(context.Context, string, string, MergeAlbumsInput) (MergeResultDTO, error)
	CreateTrack(context.Context, string, string, CreateTrackInput) (TrackDTO, error)
	UpdateTrack(context.Context, string, string, string, UpdateTrackInput) (TrackDTO, error)
	PublishTrack(context.Context, string, string, string, int) (TrackDTO, error)
	ArchiveTrack(context.Context, string, string, string, int) (TrackDTO, error)
	RestoreTrack(context.Context, string, string, string, int) (TrackDTO, error)
	RestoreTracksBatch(context.Context, string, string, BatchTrackMutationInput) (BatchRestoreDTO, error)
	DeleteTrackPermanently(context.Context, string, string, string, int) (DeleteTrackDTO, error)
	CreatePermanentDeleteBatch(context.Context, string, string, BatchTrackMutationInput) (PermanentDeleteBatchDTO, error)
	PermanentDeleteBatch(context.Context, string) (PermanentDeleteBatchDTO, error)
	UpsertLyrics(context.Context, string, string, string, LyricsInput) (LyricDTO, error)
	UpdateUserStatus(context.Context, string, string, string, UserStatusInput) (UserStatusDTO, error)
}

type Routes struct {
	service     API
	identity    adminauth.Identity
	idempotency Idempotency
}

func NewRoutes(service API, identity adminauth.Identity, idempotency Idempotency) (*Routes, error) {
	if service == nil {
		return nil, errors.New("admin mutation API is required")
	}
	if identity == nil {
		return nil, errors.New("admin mutation identity is required")
	}
	if idempotency == nil {
		return nil, errors.New("admin mutation idempotency is required")
	}
	return &Routes{service: service, identity: identity, idempotency: idempotency}, nil
}
func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.POST("/artists", httpserver.Handle(routes.createArtist))
	admin.PATCH("/artists/:id", httpserver.Handle(routes.updateArtist))
	admin.POST("/albums", httpserver.Handle(routes.createAlbum))
	admin.PATCH("/albums/:id", httpserver.Handle(routes.updateAlbum))
	admin.POST("/albums/merge", httpserver.Handle(routes.mergeAlbums))
	admin.POST("/tracks", httpserver.Handle(routes.createTrack))
	admin.POST("/tracks/batch/restore", httpserver.Handle(routes.restoreTracksBatch))
	admin.POST("/tracks/batch/delete-permanently", httpserver.Handle(routes.createPermanentDeleteBatch))
	admin.GET("/tracks/batch/delete-permanently/:jobId", httpserver.Handle(routes.permanentDeleteBatch))
	admin.PATCH("/tracks/:id", httpserver.Handle(routes.updateTrack))
	admin.POST("/tracks/:id/publish", httpserver.Handle(routes.publishTrack))
	admin.POST("/tracks/:id/archive", httpserver.Handle(routes.archiveTrack))
	admin.POST("/tracks/:id/restore", httpserver.Handle(routes.restoreTrack))
	admin.DELETE("/tracks/:id", httpserver.Handle(routes.deleteTrack))
	admin.PUT("/tracks/:id/lyrics", httpserver.Handle(routes.upsertLyrics))
	admin.PATCH("/users/:id/status", httpserver.Handle(routes.updateUserStatus))
}

func (routes *Routes) createArtist(c *gin.Context) error {
	var input CreateArtistInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if !routeText(input.Name, 1, 200) || (input.Description.Set && input.Description.Value != nil && routeLength(*input.Description.Value) > 5000) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.artist.create", artistCreatePayload(input), http.StatusCreated, func(actor, trace string) (any, error) {
		return routes.service.CreateArtist(c.Request.Context(), actor, trace, input)
	})
}
func (routes *Routes) updateArtist(c *gin.Context) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input UpdateArtistInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 || (!input.Name.Set && !input.Description.Set) || (input.Name.Set && !routeText(input.Name.Value, 1, 200)) || (input.Description.Set && input.Description.Value != nil && routeLength(*input.Description.Value) > 5000) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.artist.update:"+id, artistUpdatePayload(input), http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.UpdateArtist(c.Request.Context(), actor, trace, id, input)
	})
}
func (routes *Routes) createAlbum(c *gin.Context) error {
	var input CreateAlbumInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if !routeText(input.Title, 1, 300) || !validCreditsRoute(input.ArtistCredits) || !optionalDateRoute(input.ReleaseDate) || (input.Description.Set && input.Description.Value != nil && routeLength(*input.Description.Value) > 5000) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.album.create", albumCreatePayload(input), http.StatusCreated, func(actor, trace string) (any, error) {
		return routes.service.CreateAlbum(c.Request.Context(), actor, trace, input)
	})
}
func (routes *Routes) updateAlbum(c *gin.Context) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input UpdateAlbumInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 || (!input.Title.Set && !input.ArtistCredits.Set && !input.ReleaseDate.Set && !input.Description.Set) || (input.Title.Set && !routeText(input.Title.Value, 1, 300)) || (input.ArtistCredits.Set && !validCreditsRoute(input.ArtistCredits.Values)) || !optionalDateRoute(input.ReleaseDate) || (input.Description.Set && input.Description.Value != nil && routeLength(*input.Description.Value) > 5000) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.album.update:"+id, albumUpdatePayload(input), http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.UpdateAlbum(c.Request.Context(), actor, trace, id, input)
	})
}
func (routes *Routes) mergeAlbums(c *gin.Context) error {
	var input MergeAlbumsInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if !validMergeRoute(input) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.album.merge:"+input.Target.AlbumID, mergePayload(input), http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.MergeAlbums(c.Request.Context(), actor, trace, input)
	})
}
func (routes *Routes) createTrack(c *gin.Context) error {
	var input CreateTrackInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if !input.DiscNumber.Set {
		input.DiscNumber = OptionalInt{Set: true, Value: 1}
	}
	if !routeText(input.Title, 1, 300) || !validCreditsRoute(input.ArtistCredits) || (input.AlbumID.Set && input.AlbumID.Value != nil && !isMutationUUID(*input.AlbumID.Value)) || (input.TrackNumber.Set && input.TrackNumber.Value != nil && *input.TrackNumber.Value < 1) || input.DiscNumber.Value < 1 {
		return mutationContractError()
	}
	return routes.execute(c, "admin.track.create", trackCreatePayload(input), http.StatusCreated, func(actor, trace string) (any, error) {
		return routes.service.CreateTrack(c.Request.Context(), actor, trace, input)
	})
}
func (routes *Routes) updateTrack(c *gin.Context) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input UpdateTrackInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 || (!input.Title.Set && !input.AlbumID.Set && !input.ArtistCredits.Set && !input.TrackNumber.Set && !input.DiscNumber.Set) || (input.Title.Set && !routeText(input.Title.Value, 1, 300)) || (input.AlbumID.Set && input.AlbumID.Value != nil && !isMutationUUID(*input.AlbumID.Value)) || (input.ArtistCredits.Set && !validCreditsRoute(input.ArtistCredits.Values)) || (input.TrackNumber.Set && input.TrackNumber.Value != nil && *input.TrackNumber.Value < 1) || (input.DiscNumber.Set && input.DiscNumber.Value < 1) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.track.update:"+id, trackUpdatePayload(input), http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.UpdateTrack(c.Request.Context(), actor, trace, id, input)
	})
}
func (routes *Routes) publishTrack(c *gin.Context) error {
	return routes.versionMutation(c, "admin.track.publish:", routes.service.PublishTrack)
}
func (routes *Routes) archiveTrack(c *gin.Context) error {
	return routes.versionMutation(c, "admin.track.archive:", routes.service.ArchiveTrack)
}
func (routes *Routes) restoreTrack(c *gin.Context) error {
	return routes.versionMutation(c, "admin.track.restore:", routes.service.RestoreTrack)
}
func (routes *Routes) restoreTracksBatch(c *gin.Context) error {
	input, err := decodeBatchTrackMutation(c)
	if err != nil {
		return err
	}
	return routes.execute(c, "admin.track.restore.batch", input, http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.RestoreTracksBatch(c.Request.Context(), actor, trace, input)
	})
}
func (routes *Routes) createPermanentDeleteBatch(c *gin.Context) error {
	input, err := decodeBatchTrackMutation(c)
	if err != nil {
		return err
	}
	return routes.execute(c, "admin.track.delete-permanently.batch", input, http.StatusAccepted, func(actor, trace string) (any, error) {
		return routes.service.CreatePermanentDeleteBatch(c.Request.Context(), actor, trace, input)
	})
}
func (routes *Routes) permanentDeleteBatch(c *gin.Context) error {
	jobID, err := mutationUUID(c.Param("jobId"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.PermanentDeleteBatch(c.Request.Context(), jobID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}
func (routes *Routes) deleteTrack(c *gin.Context) error { return routes.versionMutationDelete(c) }
func (routes *Routes) versionMutation(c *gin.Context, prefix string, operation func(context.Context, string, string, string, int) (TrackDTO, error)) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	input, err := decodeVersion(c)
	if err != nil {
		return err
	}
	return routes.execute(c, prefix+id, map[string]any{"expectedVersion": input.ExpectedVersion}, http.StatusOK, func(actor, trace string) (any, error) {
		return operation(c.Request.Context(), actor, trace, id, input.ExpectedVersion)
	})
}
func (routes *Routes) versionMutationDelete(c *gin.Context) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	input, err := decodeVersion(c)
	if err != nil {
		return err
	}
	return routes.execute(c, "admin.track.delete-permanently:"+id, map[string]any{"expectedVersion": input.ExpectedVersion}, http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.DeleteTrackPermanently(c.Request.Context(), actor, trace, id, input.ExpectedVersion)
	})
}
func (routes *Routes) upsertLyrics(c *gin.Context) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input LyricsInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 || !routeText(input.Language, 2, 35) || (input.Format != "LRC" && input.Format != "PLAIN") || !input.Content.Set || routeLength(input.Content.Value) > 1000000 || !input.IsDefault.Set {
		return mutationContractError()
	}
	return routes.execute(c, "admin.track.lyrics:"+id+":"+input.Language, lyricsPayload(input), http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.UpsertLyrics(c.Request.Context(), actor, trace, id, input)
	})
}
func (routes *Routes) updateUserStatus(c *gin.Context) error {
	id, err := mutationUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input UserStatusInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 || (input.Status != UserActive && input.Status != UserSuspended) || !routeText(input.Reason, 1, 500) {
		return mutationContractError()
	}
	return routes.execute(c, "admin.user.status:"+id, map[string]any{"expectedVersion": input.ExpectedVersion, "status": input.Status, "reason": input.Reason}, http.StatusOK, func(actor, trace string) (any, error) {
		return routes.service.UpdateUserStatus(c.Request.Context(), actor, trace, id, input)
	})
}

func (routes *Routes) execute(c *gin.Context, scope string, payload any, status int, operation func(string, string) (any, error)) error {
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !mutationIdempotencyPattern.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	trace := httpserver.TraceID(c)
	result, err := routes.idempotency.Execute(c.Request.Context(), IdempotencyInput{ActorID: actor.UserID, Scope: scope, Key: key, Payload: payload}, func() (IdempotencyResponse, error) {
		body, err := operation(actor.UserID, trace)
		if err != nil {
			return IdempotencyResponse{}, err
		}
		encoded, err := json.Marshal(body)
		return IdempotencyResponse{Status: status, Body: encoded}, err
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.Data(result.Status, "application/json; charset=utf-8", result.Body)
	return nil
}
func decodeVersion(c *gin.Context) (VersionInput, error) {
	var input VersionInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return input, err
	}
	if input.ExpectedVersion < 1 {
		return input, mutationContractError()
	}
	return input, nil
}
func decodeBatchTrackMutation(c *gin.Context) (BatchTrackMutationInput, error) {
	var input BatchTrackMutationInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return input, err
	}
	if len(input.Items) < 1 || len(input.Items) > 200 {
		return input, mutationContractError()
	}
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if !isMutationUUID(item.TrackID) || item.ExpectedVersion < 1 {
			return input, mutationContractError()
		}
		canonicalID := strings.ToLower(item.TrackID)
		if _, duplicate := seen[canonicalID]; duplicate {
			return input, mutationContractError()
		}
		seen[canonicalID] = struct{}{}
	}
	return input, nil
}
func validCreditsRoute(credits []CreditInput) bool {
	if len(credits) < 1 || len(credits) > 100 {
		return false
	}
	for _, credit := range credits {
		if !isMutationUUID(credit.ArtistID) || !validCreditRole(credit.Role) || !credit.SortOrderSet || credit.SortOrder < 0 {
			return false
		}
	}
	return true
}
func validMergeRoute(input MergeAlbumsInput) bool {
	if !isMutationUUID(input.Target.AlbumID) || input.Target.ExpectedVersion < 1 || len(input.Sources) < 1 || len(input.Sources) > 100 || !isMutationUUID(input.FieldSources.Title) || !isMutationUUID(input.FieldSources.ArtistCredits) || !input.FieldSources.Cover.Set || !input.FieldSources.ReleaseDate.Set || !input.FieldSources.Description.Set {
		return false
	}
	for _, source := range input.Sources {
		if !isMutationUUID(source.AlbumID) || source.ExpectedVersion < 1 {
			return false
		}
	}
	for _, source := range []*string{input.FieldSources.Cover.Value, input.FieldSources.ReleaseDate.Value, input.FieldSources.Description.Value} {
		if source != nil && !isMutationUUID(*source) {
			return false
		}
	}
	return true
}
func optionalDateRoute(value OptionalNullableString) bool {
	if !value.Set || value.Value == nil {
		return true
	}
	parsed, err := time.Parse("2006-01-02", *value.Value)
	return err == nil && parsed.Format("2006-01-02") == *value.Value
}
func mutationUUID(value string) (string, error) {
	if !isMutationUUID(value) {
		return "", mutationContractError()
	}
	return value, nil
}
func isMutationUUID(value string) bool { return mutationUUIDPattern.MatchString(value) }
func routeLength(value string) int     { return len(utf16.Encode([]rune(value))) }
func routeText(value string, min, max int) bool {
	length := routeLength(value)
	return length >= min && length <= max
}
func mutationContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

func artistCreatePayload(i CreateArtistInput) map[string]any {
	p := map[string]any{"name": i.Name}
	if i.Description.Set {
		p["description"] = i.Description.Value
	}
	return p
}
func artistUpdatePayload(i UpdateArtistInput) map[string]any {
	p := map[string]any{"expectedVersion": i.ExpectedVersion}
	if i.Name.Set {
		p["name"] = i.Name.Value
	}
	if i.Description.Set {
		p["description"] = i.Description.Value
	}
	return p
}
func creditPayloads(values []CreditInput) []map[string]any {
	p := make([]map[string]any, 0, len(values))
	for _, c := range values {
		p = append(p, map[string]any{"artistId": c.ArtistID, "role": c.Role, "sortOrder": c.SortOrder})
	}
	return p
}
func albumCreatePayload(i CreateAlbumInput) map[string]any {
	p := map[string]any{"title": i.Title, "artistCredits": creditPayloads(i.ArtistCredits)}
	if i.ReleaseDate.Set {
		p["releaseDate"] = i.ReleaseDate.Value
	}
	if i.Description.Set {
		p["description"] = i.Description.Value
	}
	return p
}
func albumUpdatePayload(i UpdateAlbumInput) map[string]any {
	p := map[string]any{"expectedVersion": i.ExpectedVersion}
	if i.Title.Set {
		p["title"] = i.Title.Value
	}
	if i.ArtistCredits.Set {
		p["artistCredits"] = creditPayloads(i.ArtistCredits.Values)
	}
	if i.ReleaseDate.Set {
		p["releaseDate"] = i.ReleaseDate.Value
	}
	if i.Description.Set {
		p["description"] = i.Description.Value
	}
	return p
}
func mergePayload(i MergeAlbumsInput) map[string]any {
	return map[string]any{"target": map[string]any{"albumId": i.Target.AlbumID, "expectedVersion": i.Target.ExpectedVersion}, "sources": i.Sources, "fieldSources": map[string]any{"title": i.FieldSources.Title, "cover": i.FieldSources.Cover.Value, "artistCredits": i.FieldSources.ArtistCredits, "releaseDate": i.FieldSources.ReleaseDate.Value, "description": i.FieldSources.Description.Value}}
}
func trackCreatePayload(i CreateTrackInput) map[string]any {
	p := map[string]any{"title": i.Title, "artistCredits": creditPayloads(i.ArtistCredits), "discNumber": i.DiscNumber.Value}
	if i.AlbumID.Set {
		p["albumId"] = i.AlbumID.Value
	}
	if i.TrackNumber.Set {
		p["trackNumber"] = i.TrackNumber.Value
	}
	return p
}
func trackUpdatePayload(i UpdateTrackInput) map[string]any {
	p := map[string]any{"expectedVersion": i.ExpectedVersion}
	if i.Title.Set {
		p["title"] = i.Title.Value
	}
	if i.AlbumID.Set {
		p["albumId"] = i.AlbumID.Value
	}
	if i.ArtistCredits.Set {
		p["artistCredits"] = creditPayloads(i.ArtistCredits.Values)
	}
	if i.TrackNumber.Set {
		p["trackNumber"] = i.TrackNumber.Value
	}
	if i.DiscNumber.Set {
		p["discNumber"] = i.DiscNumber.Value
	}
	return p
}
func lyricsPayload(i LyricsInput) map[string]any {
	return map[string]any{"expectedVersion": i.ExpectedVersion, "language": i.Language, "format": i.Format, "content": i.Content.Value, "isDefault": i.IsDefault.Value}
}

var mutationUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var mutationIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
