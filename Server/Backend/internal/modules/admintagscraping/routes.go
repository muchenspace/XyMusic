package admintagscraping

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"
	"unicode/utf16"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type ScrapingAPI interface {
	Search(context.Context, SearchInput) ([]Candidate, error)
	CandidateDetails(context.Context, Candidate) (CandidateDetailsDTO, error)
	SearchArtists(context.Context, ArtistSearchInput) ([]ArtistCandidate, error)
	Fingerprint(context.Context, string) ([]Candidate, error)
	Apply(context.Context, string, string, string, ApplyInput) (ApplyResult, error)
	ApplyArtistArtwork(context.Context, string, string, string, ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error)
	Artwork(context.Context, string) (DownloadedArtwork, error)
}

type BatchAPI interface {
	Create(context.Context, string, CreateBatchInput) (BatchJobDTO, error)
	Job(context.Context, string, *time.Time) (BatchJobDTO, error)
	Cancel(context.Context, string) (BatchJobDTO, error)
	Retry(context.Context, string) (BatchJobDTO, error)
}

type ArtistArtworkBatchAPI interface {
	Create(context.Context, string, CreateArtistArtworkBatchInput) (ArtistArtworkBatchCreateResult, error)
	Job(context.Context, string, *time.Time) (ArtistArtworkBatchJobDTO, error)
	Cancel(context.Context, string) (ArtistArtworkBatchJobDTO, error)
	Retry(context.Context, string) (ArtistArtworkBatchJobDTO, error)
}

type Routes struct {
	scraping             ScrapingAPI
	batches              BatchAPI
	artistArtworkBatches ArtistArtworkBatchAPI
	identity             adminauth.Identity
	idempotency          Idempotency
}

func NewRoutes(
	scraping ScrapingAPI,
	batches BatchAPI,
	artistArtworkBatches ArtistArtworkBatchAPI,
	identity adminauth.Identity,
	idempotency Idempotency,
) (*Routes, error) {
	if scraping == nil {
		return nil, errors.New("admin tag scraping service is required")
	}
	if batches == nil {
		return nil, errors.New("admin tag scraping batch service is required")
	}
	if artistArtworkBatches == nil {
		return nil, errors.New("artist artwork scraping batch service is required")
	}
	if identity == nil {
		return nil, errors.New("admin tag scraping identity service is required")
	}
	if idempotency == nil {
		return nil, errors.New("admin tag scraping idempotency service is required")
	}
	return &Routes{
		scraping: scraping, batches: batches, artistArtworkBatches: artistArtworkBatches,
		identity: identity, idempotency: idempotency,
	}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	group := router.Group("/api/v1/admin/tag-scraping")
	group.POST("/search", httpserver.Handle(routes.search))
	group.POST("/candidates/details", httpserver.Handle(routes.candidateDetails))
	group.POST("/artists/search", httpserver.Handle(routes.searchArtists))
	group.POST("/artists/batches", httpserver.Handle(routes.createArtistArtworkBatch))
	group.GET("/artists/batches/:id", httpserver.Handle(routes.getArtistArtworkBatch))
	group.POST("/artists/batches/:id/cancel", httpserver.Handle(routes.cancelArtistArtworkBatch))
	group.POST("/artists/batches/:id/retry", httpserver.Handle(routes.retryArtistArtworkBatch))
	group.POST("/artists/:id/apply", httpserver.Handle(routes.applyArtistArtwork))
	group.POST("/tracks/:id/fingerprint", httpserver.Handle(routes.fingerprint))
	group.POST("/tracks/:id/apply", httpserver.Handle(routes.apply))
	group.GET("/artwork", httpserver.Handle(routes.artwork))
	group.POST("/batches", httpserver.Handle(routes.createBatch))
	group.GET("/batches/:id", httpserver.Handle(routes.getBatch))
	group.POST("/batches/:id/cancel", httpserver.Handle(routes.cancelBatch))
	group.POST("/batches/:id/retry", httpserver.Handle(routes.retryBatch))
}

func (routes *Routes) search(c *gin.Context) error {
	var input SearchInput
	shape, err := decodeContractJSON(c, &input, "source")
	if err != nil {
		return err
	}
	if err := validateSearchInput(input, shape); err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	result, err := routes.scraping.Search(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) candidateDetails(c *gin.Context) error {
	var input CandidateDetailsInput
	shape, err := decodeContractJSON(c, &input, "candidate")
	if err != nil {
		return err
	}
	if err := validateCandidateDetailsInput(input, shape); err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	result, err := routes.scraping.CandidateDetails(c.Request.Context(), input.Candidate)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) fingerprint(c *gin.Context) error {
	trackID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	result, err := routes.scraping.Fingerprint(c.Request.Context(), trackID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) apply(c *gin.Context) error {
	trackID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input ApplyInput
	shape, err := decodeContractJSON(c, &input, "expectedVersion", "candidate", "fields", "reason")
	if err != nil {
		return err
	}
	if err := validateApplyInput(input, shape); err != nil {
		return err
	}
	return routes.mutate(c, "admin.tag-scraping.apply:"+trackID, input, http.StatusOK, func(actorID, traceID string) (any, error) {
		return routes.scraping.Apply(c.Request.Context(), actorID, traceID, trackID, input)
	})
}

func (routes *Routes) artwork(c *gin.Context) error {
	value, exists := lastQueryValue(c.Request.URL.Query(), "url")
	if !exists || !contractStringLength(value, 8, 2_000) {
		return contractError()
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	artwork, err := routes.scraping.Artwork(c.Request.Context(), value)
	if err != nil {
		return err
	}
	c.Header("Content-Length", strconv.Itoa(len(artwork.Bytes)))
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, artwork.ContentType, artwork.Bytes)
	return nil
}

func (routes *Routes) createBatch(c *gin.Context) error {
	var input CreateBatchInput
	shape, err := decodeContractJSON(c, &input, "items", "options")
	if err != nil {
		return err
	}
	if err := validateBatchInput(input, shape); err != nil {
		return err
	}
	return routes.mutate(c, "admin.tag-scraping.batch.create", input, http.StatusAccepted, func(actorID, _ string) (any, error) {
		return routes.batches.Create(c.Request.Context(), actorID, input)
	})
}

func (routes *Routes) getBatch(c *gin.Context) error {
	jobID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var updatedAfter *time.Time
	query := c.Request.URL.Query()
	if value, exists := lastQueryValue(query, "updatedAfter"); exists {
		if !contractStringLength(value, 1, 40) {
			return contractError()
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil {
			return contractError()
		}
		parsed = parsed.UTC()
		updatedAfter = &parsed
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	job, err := routes.batches.Job(c.Request.Context(), jobID, updatedAfter)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, job)
	return nil
}

func lastQueryValue(query map[string][]string, name string) (string, bool) {
	values, exists := query[name]
	if !exists || len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

func (routes *Routes) cancelBatch(c *gin.Context) error {
	jobID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	return routes.mutate(c, "admin.tag-scraping.batch.cancel:"+jobID, map[string]any{}, http.StatusAccepted, func(_, _ string) (any, error) {
		return routes.batches.Cancel(c.Request.Context(), jobID)
	})
}

func (routes *Routes) retryBatch(c *gin.Context) error {
	jobID, err := contractUUID(c.Param("id"))
	if err != nil {
		return err
	}
	return routes.mutate(c, "admin.tag-scraping.batch.retry:"+jobID, map[string]any{}, http.StatusAccepted, func(_, _ string) (any, error) {
		return routes.batches.Retry(c.Request.Context(), jobID)
	})
}

func (routes *Routes) mutate(
	c *gin.Context,
	scope string,
	payload any,
	status int,
	operation func(string, string) (any, error),
) error {
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	traceID := httpserver.TraceID(c)
	result, err := routes.idempotency.Execute(c.Request.Context(), IdempotencyInput{
		ActorID: actor.UserID,
		Scope:   scope,
		Key:     key,
		Payload: payload,
	}, func() (IdempotencyResponse, error) {
		body, operationErr := operation(actor.UserID, traceID)
		if operationErr != nil {
			return IdempotencyResponse{}, operationErr
		}
		encoded, encodeErr := json.Marshal(body)
		if encodeErr != nil {
			return IdempotencyResponse{}, encodeErr
		}
		return IdempotencyResponse{Status: status, Body: encoded}, nil
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.Data(result.Status, "application/json; charset=utf-8", result.Body)
	return nil
}

func decodeContractJSON(c *gin.Context, destination any, required ...string) (map[string]json.RawMessage, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, contractError()
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 2*1024*1024+1))
	if err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return nil, apperror.PayloadTooLarge("Request body exceeds the permitted size")
		}
		return nil, contractParseError()
	}
	if len(raw) > 2*1024*1024 {
		return nil, apperror.PayloadTooLarge("Request body exceeds the permitted size")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, contractError()
	}
	if !json.Valid(raw) {
		return nil, contractParseError()
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil || shape == nil {
		return nil, contractError()
	}
	for _, field := range required {
		value, exists := shape[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, contractError()
		}
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return nil, contractError()
	}
	return shape, nil
}

func validateSearchInput(input SearchInput, shape map[string]json.RawMessage) error {
	if input.Source != SourceSmart && !isSearchableSource(input.Source) {
		return contractError()
	}
	if hasExplicitNull(shape, "query", "title", "artist", "album", "sources") {
		return contractError()
	}
	for _, value := range []*string{input.Query, input.Title, input.Artist, input.Album} {
		if value != nil && !contractStringLength(*value, 0, 300) {
			return contractError()
		}
	}
	if _, provided := shape["sources"]; provided {
		if len(input.Sources) < 1 || len(input.Sources) > 5 || !uniqueSources(input.Sources) {
			return contractError()
		}
		for _, source := range input.Sources {
			if !isSearchableSource(source) {
				return contractError()
			}
		}
	}
	return nil
}

func validateCandidateDetailsInput(input CandidateDetailsInput, shape map[string]json.RawMessage) error {
	if err := requireNestedKeys(shape["candidate"],
		"id", "name", "artist", "artistId", "album", "albumId", "albumImg", "year", "track", "disc", "genre", "source"); err != nil {
		return err
	}
	var candidateShape map[string]json.RawMessage
	if json.Unmarshal(shape["candidate"], &candidateShape) != nil || hasExplicitNull(candidateShape, "titleScore", "artistScore", "albumScore", "score") {
		return contractError()
	}
	return validateCandidateContract(input.Candidate)
}

func validateApplyInput(input ApplyInput, shape map[string]json.RawMessage) error {
	if input.ExpectedVersion < 1 || !contractStringLength(input.Reason, 2, 500) {
		return contractError()
	}
	if hasExplicitNull(shape, "writeBack") {
		return contractError()
	}
	if err := requireNestedKeys(shape["candidate"],
		"id", "name", "artist", "artistId", "album", "albumId", "albumImg", "year", "track", "disc", "genre", "source"); err != nil {
		return err
	}
	if err := requireNestedKeys(shape["fields"], "title", "artist", "album", "year", "genre", "lyrics", "cover", "overwrite"); err != nil {
		return err
	}
	var candidateShape map[string]json.RawMessage
	if json.Unmarshal(shape["candidate"], &candidateShape) != nil || hasExplicitNull(candidateShape, "titleScore", "artistScore", "albumScore", "score") {
		return contractError()
	}
	return validateCandidateContract(input.Candidate)
}

func validateBatchInput(input CreateBatchInput, shape map[string]json.RawMessage) error {
	if len(input.Items) < 1 || len(input.Items) > 200 {
		return contractError()
	}
	seenTracks := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if !uuidPattern.MatchString(item.TrackID) || item.ExpectedVersion < 1 {
			return contractError()
		}
		if _, duplicate := seenTracks[item.TrackID]; duplicate {
			return contractError()
		}
		seenTracks[item.TrackID] = struct{}{}
	}
	if err := requireNestedKeys(shape["options"], "sources", "matchMode", "missingFields", "fields", "reason"); err != nil {
		return err
	}
	var optionShape map[string]json.RawMessage
	if err := json.Unmarshal(shape["options"], &optionShape); err != nil {
		return contractError()
	}
	if hasExplicitNull(optionShape, "writeBack") {
		return contractError()
	}
	if err := requireNestedKeys(optionShape["fields"], "title", "artist", "album", "year", "genre", "lyrics", "cover", "overwrite"); err != nil {
		return err
	}
	options := input.Options
	if len(options.Sources) < 1 || len(options.Sources) > 5 || !uniqueSources(options.Sources) ||
		(options.MatchMode != MatchStrict && options.MatchMode != MatchSimple) ||
		len(options.MissingFields) > 6 || !uniqueMissingFields(options.MissingFields) ||
		!contractStringLength(options.Reason, 2, 500) {
		return contractError()
	}
	for _, source := range options.Sources {
		if !isSearchableSource(source) {
			return contractError()
		}
	}
	for _, field := range options.MissingFields {
		if !isMissingField(field) {
			return contractError()
		}
	}
	return nil
}

func validateCandidateContract(candidate Candidate) error {
	limits := []struct {
		value string
		min   int
		max   int
	}{
		{candidate.ID, 1, 2_000}, {candidate.Name, 1, 300}, {candidate.Artist, 0, 500},
		{candidate.ArtistID, 0, 2_000}, {candidate.Album, 0, 500}, {candidate.AlbumID, 0, 2_000},
		{candidate.AlbumImg, 0, 2_000}, {candidate.Year, 0, 30}, {candidate.Track, 0, 30},
		{candidate.Disc, 0, 30}, {candidate.Genre, 0, 200},
	}
	for _, limit := range limits {
		if !contractStringLength(limit.value, limit.min, limit.max) {
			return contractError()
		}
	}
	if !isSearchableSource(candidate.Source) && candidate.Source != SourceAcoustID {
		return contractError()
	}
	for _, score := range []*float64{candidate.TitleScore, candidate.ArtistScore, candidate.AlbumScore, candidate.Score} {
		if score != nil && (math.IsNaN(*score) || math.IsInf(*score, 0)) {
			return contractError()
		}
	}
	return nil
}

func requireNestedKeys(raw json.RawMessage, fields ...string) error {
	var shape map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &shape) != nil || shape == nil {
		return contractError()
	}
	for _, field := range fields {
		value, exists := shape[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return contractError()
		}
	}
	return nil
}

func hasExplicitNull(shape map[string]json.RawMessage, fields ...string) bool {
	for _, field := range fields {
		if raw, exists := shape[field]; exists && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return true
		}
	}
	return false
}

func contractUUID(value string) (string, error) {
	if !uuidPattern.MatchString(value) {
		return "", contractError()
	}
	return value, nil
}

func contractStringLength(value string, minimum, maximum int) bool {
	length := len(utf16.Encode([]rune(value)))
	return length >= minimum && length <= maximum
}

func isSearchableSource(source Source) bool {
	for _, candidate := range searchableSources {
		if source == candidate {
			return true
		}
	}
	return false
}

func uniqueSources(sources []Source) bool {
	seen := make(map[Source]struct{}, len(sources))
	for _, source := range sources {
		if _, exists := seen[source]; exists {
			return false
		}
		seen[source] = struct{}{}
	}
	return true
}

func uniqueMissingFields(fields []MissingField) bool {
	seen := make(map[MissingField]struct{}, len(fields))
	for _, field := range fields {
		if _, exists := seen[field]; exists {
			return false
		}
		seen[field] = struct{}{}
	}
	return true
}

func isMissingField(field MissingField) bool {
	switch field {
	case MissingArtist, MissingAlbum, MissingYear, MissingGenre, MissingLyrics, MissingCover:
		return true
	default:
		return false
	}
}

func contractError() error {
	return apperror.Validation("请求参数不符合接口要求")
}

func contractParseError() error {
	return apperror.Validation("请求内容无法解析")
}

var (
	uuidPattern           = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)
