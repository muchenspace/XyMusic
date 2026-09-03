package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/lyrics"
)

var defaultSmartSources = []Source{SourceQMusic, SourceNetease, SourceKugou}

const unknownArtistPlaceholder = "Unknown Artist"

type ServiceDependencies struct {
	Store                   Store
	Music                   MusicPlatform
	Artwork                 ArtworkApplier
	DefaultLibraryDirectory string
}

type Service struct {
	store                   Store
	music                   MusicPlatform
	artwork                 ArtworkApplier
	defaultLibraryDirectory string
}

var (
	_ ScrapingAPI    = (*Service)(nil)
	_ BatchProcessor = (*Service)(nil)
)

type batchMetadataStore interface {
	MetadataBatch(context.Context, []string) (map[string]TrackMetadata, error)
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Store == nil {
		return nil, errors.New("admin tag scraping store is required")
	}
	if dependencies.Music == nil {
		return nil, errors.New("admin tag scraping music platform is required")
	}
	if dependencies.Artwork == nil {
		return nil, errors.New("admin tag scraping artwork applier is required")
	}
	if strings.TrimSpace(dependencies.DefaultLibraryDirectory) == "" {
		return nil, errors.New("admin tag scraping library directory is required")
	}
	return &Service{
		store: dependencies.Store, music: dependencies.Music,
		artwork: dependencies.Artwork, defaultLibraryDirectory: dependencies.DefaultLibraryDirectory,
	}, nil
}

func (service *Service) Search(ctx context.Context, input SearchInput) ([]Candidate, error) {
	searchText := ""
	if input.Query != nil {
		searchText = cleanScrapedText(*input.Query)
	}
	if searchText == "" && input.Title != nil {
		searchText = cleanScrapedText(*input.Title)
	}
	if searchText == "" {
		return nil, apperror.Validation("Search text must not be empty")
	}
	if input.Source != SourceSmart && !isSearchableSource(input.Source) {
		return nil, apperror.Validation("The music platform source is invalid")
	}
	sources := []Source{input.Source}
	if input.Source == SourceSmart {
		sources = validSmartSources(input.Sources)
		if input.Verbatim {
			verbatimSources := make([]Source, 0, len(sources))
			for _, s := range sources {
				if isVerbatimSource(s) {
					verbatimSources = append(verbatimSources, s)
				}
			}
			if len(verbatimSources) == 0 {
				return nil, unsupportedVerbatimSourceError()
			}
			sources = verbatimSources
		}
	} else if input.Verbatim && !isVerbatimSource(input.Source) {
		return nil, unsupportedVerbatimSourceError()
	}

	var candidates []Candidate
	if input.Source != SourceSmart {
		result, err := service.music.Search(ctx, sources[0], searchText)
		if err != nil {
			return nil, err
		}
		candidates = result
	} else {
		type searchResult struct {
			items []Candidate
		}
		results := make(chan searchResult, len(sources))
		var wait sync.WaitGroup
		for _, source := range sources {
			source := source
			wait.Add(1)
			go func() {
				defer wait.Done()
				items, err := service.music.Search(ctx, source, searchText)
				if err == nil {
					results <- searchResult{items: items}
				}
			}()
		}
		wait.Wait()
		close(results)
		for result := range results {
			candidates = append(candidates, result.items...)
		}
	}

	scored := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		titleScore, artistScore, albumScore, score := scoreCandidate(input, candidate)
		candidate.TitleScore = floatPointer(titleScore)
		candidate.ArtistScore = floatPointer(artistScore)
		candidate.AlbumScore = floatPointer(albumScore)
		candidate.Score = floatPointer(score)
		if input.Source != SourceSmart || titleScore > 0 {
			scored = append(scored, candidate)
		}
	}
	sort.SliceStable(scored, func(left, right int) bool {
		return valueOrZero(scored[left].Score) > valueOrZero(scored[right].Score)
	})
	limit := 10
	if input.Source == SourceSmart {
		limit = 15
	}
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

func (service *Service) CandidateDetails(ctx context.Context, candidate Candidate, verbatim bool) (CandidateDetailsDTO, error) {
	if err := validateCandidate(candidate); err != nil {
		return CandidateDetailsDTO{}, err
	}
	if err := validateVerbatimCandidate(candidate, verbatim); err != nil {
		return CandidateDetailsDTO{}, err
	}
	result, err := service.lyrics(ctx, candidate, verbatim, false)
	if err != nil {
		return CandidateDetailsDTO{}, err
	}
	metadata, err := metadataLyrics(result)
	if err != nil {
		return CandidateDetailsDTO{}, err
	}
	return CandidateDetailsDTO{Candidate: candidate, Lyrics: metadata}, nil
}

func (service *Service) Artwork(ctx context.Context, url string) (DownloadedArtwork, error) {
	return service.music.DownloadArtwork(ctx, url)
}

func (service *Service) TrackMetadata(ctx context.Context, trackID string) (TrackMetadata, error) {
	return service.store.Metadata(ctx, trackID)
}

func (service *Service) TrackMetadataBatch(
	ctx context.Context,
	trackIDs []string,
) (map[string]TrackMetadata, error) {
	validIDs := true
	for _, trackID := range trackIDs {
		if _, err := uuid.Parse(trackID); err != nil {
			validIDs = false
			break
		}
	}
	if validIDs {
		if batchStore, ok := service.store.(batchMetadataStore); ok {
			return batchStore.MetadataBatch(ctx, trackIDs)
		}
	}
	result := make(map[string]TrackMetadata, len(trackIDs))
	for _, trackID := range trackIDs {
		metadata, err := service.TrackMetadata(ctx, trackID)
		if err != nil {
			return nil, err
		}
		result[trackID] = metadata
	}
	return result, nil
}

func (service *Service) Apply(
	ctx context.Context,
	actorID string,
	trackID string,
	input ApplyInput,
) (ApplyResult, error) {
	return service.apply(ctx, actorID, trackID, input, nil)
}

// applyWithMetadata is used by the batch worker when the claim transaction
// already loaded the metadata snapshot. UpdateMetadata still fences the
// expected version before any projection or writeback mutation is committed.
func (service *Service) applyWithMetadata(
	ctx context.Context,
	actorID string,
	trackID string,
	input ApplyInput,
	currentMetadata TrackMetadata,
) (ApplyResult, error) {
	return service.apply(ctx, actorID, trackID, input, &currentMetadata)
}

func (service *Service) apply(
	ctx context.Context,
	actorID string,
	trackID string,
	input ApplyInput,
	claimedMetadata *TrackMetadata,
) (ApplyResult, error) {
	if err := validateCandidate(input.Candidate); err != nil {
		return ApplyResult{}, err
	}
	if err := validateVerbatimCandidate(input.Candidate, input.Verbatim); err != nil {
		return ApplyResult{}, err
	}
	reason := normalizeText(input.Reason)
	if (reason != "" && javascriptLength(reason) > 500) || input.ExpectedVersion < 1 {
		return ApplyResult{}, apperror.Validation("A valid expectedVersion is required")
	}
	if err := checkApplyCancellation(ctx, input); err != nil {
		return ApplyResult{}, err
	}
	var current TrackMetadata
	var err error
	if claimedMetadata != nil {
		current = *claimedMetadata
	} else {
		current, err = service.store.Metadata(ctx, trackID)
		if err != nil {
			return ApplyResult{}, err
		}
	}
	if trackIsArchived(current.TrackStatus) {
		return ApplyResult{}, archivedTrackError(trackID)
	}
	if err := checkApplyCancellation(ctx, input); err != nil {
		return ApplyResult{}, err
	}
	if current.Version != input.ExpectedVersion {
		return ApplyResult{}, apperror.Conflict(apperror.CodeVersionConflict, "Track metadata version is stale", map[string]any{
			"expectedVersion": input.ExpectedVersion,
			"currentVersion":  current.Version,
		})
	}
	if input.WriteBack && (current.Source == nil || !current.Source.CanWriteBack) {
		message := "The current track does not have a writable local source"
		if current.Source != nil && current.Source.WritebackBlockReason != nil {
			message = *current.Source.WritebackBlockReason
		}
		return ApplyResult{}, apperror.Forbidden(message)
	}
	patch := MetadataPatch{}
	appliedFields := make([]string, 0, 12)
	warnings := make([]string, 0)
	set := func(field string, value any, currentValue any, empty bool) {
		if emptyCandidateValue(value) || (!input.Fields.Overwrite && !empty) || sameMetadataValue(value, currentValue) {
			return
		}
		patch[field] = value
		appliedFields = append(appliedFields, field)
	}
	candidate := input.Candidate
	if input.Fields.Title {
		set("title", candidate.Name, current.Effective.Title, current.Effective.Title == "")
	}
	if input.Fields.Artist {
		names := splitArtists(candidate.Artist)
		credits := make([]MetadataCredit, 0, len(names))
		for _, name := range names {
			credits = append(credits, MetadataCredit{Name: name, Role: "PRIMARY"})
		}
		set("credits", credits, current.Effective.Credits, len(current.Effective.Credits) == 0)
		set("albumArtists", names, current.Effective.AlbumArtists, len(current.Effective.AlbumArtists) == 0)
	}
	if input.Fields.Album {
		set("album", candidate.Album, nullableStringValue(current.Effective.Album), current.Effective.Album == nil || *current.Effective.Album == "")
	}
	if input.Fields.Year {
		date := scrapedReleaseDate(candidate.Year)
		set("releaseDate", date, nullableStringValue(current.Effective.ReleaseDate), current.Effective.ReleaseDate == nil)
	}
	if candidate.Track != "" {
		number, total := numberPair(candidate.Track)
		set("trackNumber", number, current.Effective.TrackNumber, current.Effective.TrackNumber == nil)
		if total != nil {
			set("trackTotal", total, current.Effective.TrackTotal, current.Effective.TrackTotal == nil)
		}
	}
	if candidate.Disc != "" {
		number, total := numberPair(candidate.Disc)
		set("discNumber", number, current.Effective.DiscNumber, current.Effective.DiscNumber == nil)
		if total != nil {
			set("discTotal", total, current.Effective.DiscTotal, current.Effective.DiscTotal == nil)
		}
	}
	if input.Fields.Genre {
		genres := []string(nil)
		if candidate.Genre != "" {
			genres = []string{candidate.Genre}
		}
		set("genres", genres, current.Effective.Genres, len(current.Effective.Genres) == 0)
	}
	if input.Fields.Lyrics && (input.Fields.Overwrite || current.Effective.Lyrics == nil) {
		lyricResult, lyricErr := service.lyrics(ctx, candidate, input.Verbatim, input.retryTransientOptional)
		var metadata *MetadataLyrics
		if lyricErr == nil {
			metadata, lyricErr = metadataLyrics(lyricResult)
		}
		switch {
		case lyricErr != nil:
			if input.retryTransientOptional && transientScrapingDependencyError(lyricErr) {
				return ApplyResult{}, lyricErr
			}
			warnings = append(warnings, "Lyrics retrieval failed: "+messageOf(lyricErr))
		case metadata == nil:
			warnings = append(warnings, "No lyrics were returned")
		default:
			if !sameMetadataValue(*metadata, current.Effective.Lyrics) {
				patch["lyrics"] = *metadata
				appliedFields = append(appliedFields, "lyrics")
			}
		}
	}

	// Download artwork before changing metadata. A transient platform failure
	// can then be retried by the batch queue without replaying a stale version.
	var artwork DownloadedArtwork
	coverReady := false
	if input.Fields.Cover && candidate.AlbumImg != "" {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		var artworkErr error
		artwork, artworkErr = service.music.DownloadArtwork(ctx, candidate.AlbumImg)
		if artworkErr != nil {
			if err := checkApplyCancellation(ctx, input); err != nil {
				return ApplyResult{}, err
			}
			if input.retryTransientOptional && transientScrapingDependencyError(artworkErr) {
				return ApplyResult{}, artworkErr
			}
			warnings = append(warnings, "Cover application failed: "+messageOf(artworkErr))
		} else {
			coverReady = true
		}
	}

	metadata := current
	if len(patch) > 0 {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		metadata, err = service.store.UpdateMetadata(ctx, actorID, trackID, input.ExpectedVersion, patch, reason)
		if err != nil {
			if apperror.IsCode(err, apperror.CodeResourceConflict) && strings.Contains(strings.ToLower(err.Error()), "does not change") {
				appliedFields = appliedFields[:0]
				warnings = append(warnings, "Existing metadata already has the selected values")
			} else {
				return ApplyResult{}, err
			}
		}
	}

	coverApplied := false
	if coverReady {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		albumID, lookupErr := service.store.TrackAlbumID(ctx, trackID)
		if lookupErr != nil {
			warnings = append(warnings, "Cover application failed: "+messageOf(lookupErr))
		} else if albumID == nil {
			warnings = append(warnings, "The track has no album; cover artwork was skipped")
		} else {
			artworkErr := service.artwork.ApplyAlbumArtwork(ctx, actorID, *albumID, artwork)
			if artworkErr != nil {
				warnings = append(warnings, "Cover application failed: "+messageOf(artworkErr))
			} else {
				coverApplied = true
			}
		}
	}
	if coverApplied {
		metadata.Effective.HasArtwork = true
	}
	if len(appliedFields) == 0 && !coverApplied && len(warnings) == 0 {
		warnings = append(warnings, "Existing metadata already has the selected values")
	}

	var writebackJob *WritebackJob
	if input.WriteBack {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		job, enqueueErr := service.store.EnqueueWriteback(ctx, actorID, trackID, metadata.Version, reason)
		if enqueueErr != nil {
			return ApplyResult{}, enqueueErr
		}
		writebackJob = &job
	}
	return ApplyResult{
		Metadata: metadata, AppliedFields: uniqueStrings(appliedFields), CoverApplied: coverApplied,
		Warnings: warnings, WritebackJob: writebackJob,
	}, nil
}

func checkApplyCancellation(ctx context.Context, input ApplyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.cancellationCheck != nil {
		return input.cancellationCheck(ctx)
	}
	return nil
}

func (service *Service) lyrics(
	ctx context.Context,
	candidate Candidate,
	verbatim bool,
	propagateTransient bool,
) (LyricResult, error) {
	result, err := service.music.Lyric(ctx, candidate.Source, candidate, verbatim)
	if err == nil && strings.TrimSpace(result.Content) != "" && (!verbatim || result.Timing == lyrics.TimingWord) {
		return result, nil
	}
	if candidate.Source == SourceQMusic && err != nil && !verbatim {
		return LyricResult{}, err
	}
	if propagateTransient && err != nil && transientScrapingDependencyError(err) {
		return LyricResult{}, err
	}
	if candidate.Name != "" {
		fallbackSources := []Source{SourceQMusic, SourceKugou, SourceNetease}
		for _, fbSource := range fallbackSources {
			if fbSource == candidate.Source {
				continue
			}
			matches, searchErr := service.music.Search(ctx, fbSource, candidate.Name)
			if searchErr != nil {
				continue
			}
			type fallback struct {
				candidate Candidate
				score     float64
			}
			fallbacks := make([]fallback, 0, len(matches))
			for _, match := range matches {
				artist := candidate.Artist
				if artist == "" {
					artist = candidate.Name
				}
				fallbacks = append(fallbacks, fallback{candidate: match, score: tagMatchScore(candidate.Name, match.Name) + tagArtistMatchScore(artist, match.Artist)})
			}
			sort.SliceStable(fallbacks, func(left, right int) bool { return fallbacks[left].score > fallbacks[right].score })
			if len(fallbacks) > 0 && fallbacks[0].score >= 2 {
				fbResult, fbErr := service.music.Lyric(ctx, fbSource, fallbacks[0].candidate, verbatim)
				if fbErr == nil && strings.TrimSpace(fbResult.Content) != "" {
					if !verbatim || fbResult.Timing == lyrics.TimingWord {
						return fbResult, nil
					}
				}
			}
		}
	}
	if err == nil && strings.TrimSpace(result.Content) != "" {
		return result, nil
	}
	return LyricResult{}, nil
}

func transientScrapingDependencyError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	applicationError, ok := apperror.As(err)
	if !ok {
		return false
	}
	if retryable, exists := applicationError.Metadata["retryable"].(bool); exists && !retryable {
		return false
	}
	return applicationError.Code == apperror.CodeDependencyUnavailable ||
		applicationError.Code == apperror.CodeRateLimited
}

func metadataLyrics(result LyricResult) (*MetadataLyrics, error) {
	if strings.TrimSpace(result.Content) == "" {
		return nil, nil
	}
	format := "PLAIN"
	if lrcPattern.MatchString(result.Content) {
		format = "LRC"
	}
	if !lyrics.ValidTiming(string(result.Timing)) {
		return nil, apperror.Validation("Lyrics timing is missing or invalid")
	}
	if err := lyrics.ValidateDocument(format, result.Timing, result.Content); err != nil {
		return nil, apperror.Validation("Lyrics timing does not match lyrics content")
	}
	return &MetadataLyrics{Content: result.Content, Format: format, Language: "und", Timing: result.Timing}, nil
}

func validSmartSources(input []Source) []Source {
	if len(input) == 0 {
		return append([]Source(nil), defaultSmartSources...)
	}
	seen := make(map[Source]struct{}, len(input))
	result := make([]Source, 0, len(input))
	for _, source := range input {
		if !isSearchableSource(source) {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	if len(result) == 0 {
		return append([]Source(nil), defaultSmartSources...)
	}
	return result
}

func validateCandidate(candidate Candidate) error {
	if candidate.ID == "" || javascriptLength(candidate.ID) > 2_000 || candidate.Name == "" || javascriptLength(candidate.Name) > 300 ||
		javascriptLength(candidate.LyricID) > 2_000 ||
		candidate.DurationMS < 0 || candidate.DurationMS > 24*60*60*1_000 {
		return apperror.Validation("The scraping candidate is missing required fields")
	}
	if !isSearchableSource(candidate.Source) {
		return apperror.Validation("The scraping candidate source is invalid")
	}
	return nil
}

func isVerbatimSource(source Source) bool {
	return source == SourceQMusic || source == SourceNetease || source == SourceKugou
}

func validateVerbatimCandidate(candidate Candidate, verbatim bool) error {
	if verbatim && !isVerbatimSource(candidate.Source) {
		return unsupportedVerbatimSourceError()
	}
	return nil
}

func unsupportedVerbatimSourceError() error {
	return apperror.Validation("The music platform source does not support verbatim lyrics")
}

func cleanScrapedText(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(emTagPattern.ReplaceAllString(fmt.Sprint(value), ""))
}

func normalizeForTagMatch(value any) string {
	cleaned := strings.ToLower(norm.NFKC.String(cleanScrapedText(value)))
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) || character == '\u3000' {
			return -1
		}
		switch character {
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015':
			return '-'
		default:
			return character
		}
	}, cleaned)
}

func tagMatchScore(left, right any) float64 {
	first := normalizeForTagMatch(left)
	second := normalizeForTagMatch(right)
	if first == "" || second == "" {
		return 0
	}
	if first == second {
		return 2
	}
	if strings.Contains(first, second) || strings.Contains(second, first) {
		return 1
	}
	return 0
}

func tagArtistMatchScore(left, right any) float64 {
	artists := splitArtists(cleanScrapedText(right))
	if len(artists) > 1 {
		limit := min(2, len(artists))
		score := 0.0
		for _, artist := range artists[:limit] {
			score += tagMatchScore(left, artist)
		}
		return score
	}
	return tagMatchScore(left, right)
}

func scoreCandidate(query SearchInput, candidate Candidate) (float64, float64, float64, float64) {
	title := ""
	if query.Title != nil && *query.Title != "" {
		title = *query.Title
	} else if query.Query != nil {
		title = *query.Query
	}
	artist := ""
	if query.Artist != nil && !isUnknownArtistPlaceholder(*query.Artist) {
		artist = *query.Artist
	}
	album := ""
	if query.Album != nil {
		album = *query.Album
	}
	titleScore := tagMatchScore(title, candidate.Name)
	artistQuery := artist
	if artistQuery == "" {
		artistQuery = title
	}
	artistScore := tagArtistMatchScore(artistQuery, candidate.Artist)
	albumQuery := album
	if albumQuery == "" {
		albumQuery = title
	}
	albumScore := tagMatchScore(albumQuery, candidate.Album)
	if artist != "" && artistScore == 0 {
		artistScore = -2
	}
	if artist == "" && artistScore >= 1 && titleScore >= 1 {
		titleScore = 2
	}
	return titleScore, artistScore, albumScore, titleScore + artistScore + albumScore
}

func isUnknownArtistPlaceholder(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), unknownArtistPlaceholder)
}

func reliableTagMatch(candidate Candidate, mode MatchMode) bool {
	if mode == MatchSimple {
		return valueOrZero(candidate.TitleScore) == 2
	}
	return valueOrZero(candidate.Score) >= 3
}

func matchesMissingFields(metadata MetadataSnapshot, fields []MissingField) bool {
	if len(fields) == 0 {
		return true
	}
	for _, field := range fields {
		switch field {
		case MissingArtist:
			found := false
			for _, credit := range metadata.Credits {
				if credit.Role == "PRIMARY" {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		case MissingAlbum:
			if metadata.Album == nil || strings.TrimSpace(*metadata.Album) == "" {
				return true
			}
		case MissingYear:
			if metadata.ReleaseDate == nil {
				return true
			}
		case MissingGenre:
			if len(metadata.Genres) == 0 {
				return true
			}
		case MissingLyrics:
			if metadata.Lyrics == nil || strings.TrimSpace(metadata.Lyrics.Content) == "" {
				return true
			}
		case MissingCover:
			if !metadata.HasArtwork {
				return true
			}
		}
	}
	return false
}

func splitArtists(value string) []string {
	parts := artistSeparator.Split(value, -1)
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, min(100, len(parts)))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(norm.NFKC.String(part))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, part)
		if len(result) == 100 {
			break
		}
	}
	return result
}

func scrapedReleaseDate(value string) any {
	match := releaseDatePattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return nil
	}
	return match[1]
}

func numberPair(value string) (any, *int) {
	parts := strings.SplitN(value, "/", 2)
	parsed, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	var number any
	if err == nil && parsed > 0 {
		number = parsed
	}
	if len(parts) < 2 {
		return number, nil
	}
	total, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || total < 1 {
		return number, nil
	}
	return number, &total
}

func emptyCandidateValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return typed == ""
	case []string:
		return len(typed) == 0
	case []MetadataCredit:
		return len(typed) == 0
	}
	return false
}

func sameMetadataValue(left, right any) bool {
	leftJSON, leftErr := json.Marshal(normalizeComparable(left))
	rightJSON, rightErr := json.Marshal(normalizeComparable(right))
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func normalizeComparable(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return value
	}
	return normalizeJSONValue(decoded)
}

func normalizeJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return normalizeText(typed)
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = normalizeJSONValue(child)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = normalizeJSONValue(child)
		}
		return result
	default:
		return typed
	}
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(strings.TrimSpace(value))), " ")
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func javascriptLength(value string) int {
	length := 0
	for _, character := range value {
		if character > 0xffff {
			length += 2
		} else {
			length++
		}
	}
	return length
}

func floatPointer(value float64) *float64 { return &value }
func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func messageOf(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	return message
}

var (
	emTagPattern       = regexp.MustCompile(`(?i)</?em>`)
	artistSeparator    = regexp.MustCompile(`[,\x{FF0C}\x{3001}/&]+`)
	releaseDatePattern = regexp.MustCompile(`\b(\d{4}(?:-\d{2}(?:-\d{2})?)?)\b`)
	lrcPattern         = regexp.MustCompile(`\[\d{1,3}:\d{2}(?:[.:]\d{1,3})?]`)
)
