package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/shared/apperror"
)

var defaultSmartSources = []Source{SourceQMusic, SourceNetease, SourceMigu, SourceKugou}

type ServiceDependencies struct {
	Store                   Store
	Music                   MusicPlatform
	Fingerprinter           Fingerprinter
	Artwork                 ArtworkApplier
	DefaultLibraryDirectory string
}

type Service struct {
	store                   Store
	music                   MusicPlatform
	fingerprinter           Fingerprinter
	artwork                 ArtworkApplier
	defaultLibraryDirectory string
}

var (
	_ ScrapingAPI    = (*Service)(nil)
	_ BatchProcessor = (*Service)(nil)
)

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
		store: dependencies.Store, music: dependencies.Music, fingerprinter: dependencies.Fingerprinter,
		artwork: dependencies.Artwork, defaultLibraryDirectory: dependencies.DefaultLibraryDirectory,
	}, nil
}

func (service *Service) Search(ctx context.Context, input SearchInput) ([]Candidate, error) {
	title := ""
	if input.Title != nil {
		title = cleanScrapedText(*input.Title)
	}
	if title == "" && input.Query != nil {
		title = cleanScrapedText(*input.Query)
	}
	if title == "" {
		return nil, apperror.Validation("Search text must not be empty")
	}
	sources := []Source{input.Source}
	if input.Source == SourceSmart {
		sources = validSmartSources(input.Sources)
	}
	var candidates []Candidate
	if input.Source != SourceSmart {
		result, err := service.music.Search(ctx, sources[0], title)
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
				items, err := service.music.Search(ctx, source, title)
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

func (service *Service) Fingerprint(ctx context.Context, trackID string) ([]Candidate, error) {
	if service.fingerprinter == nil {
		return nil, apperror.DependencyUnavailable("Audio fingerprinting is not configured: install Chromaprint and configure fpcalc")
	}
	source, err := service.store.FingerprintSource(ctx, trackID)
	if err != nil {
		return nil, err
	}
	root := source.RootPath
	if strings.TrimSpace(root) == "" {
		root = service.defaultLibraryDirectory
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve music root: %w", err)
	}
	filePath := source.SourcePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(root, filePath)
	}
	filePath, err = filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve fingerprint source: %w", err)
	}
	relative, err := filepath.Rel(root, filePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, apperror.Forbidden("The source file path is outside the music library")
	}
	fingerprint, err := service.fingerprinter.Fingerprint(ctx, filePath, source.StartMS, source.EndMS)
	if err != nil {
		return nil, err
	}
	return service.music.AcoustID(ctx, fingerprint.DurationSeconds, fingerprint.Fingerprint)
}

func (service *Service) Artwork(ctx context.Context, url string) (DownloadedArtwork, error) {
	return service.music.DownloadArtwork(ctx, url)
}

func (service *Service) TrackMetadata(ctx context.Context, trackID string) (TrackMetadata, error) {
	return service.store.Metadata(ctx, trackID)
}

func (service *Service) Apply(
	ctx context.Context,
	actorID string,
	traceID string,
	trackID string,
	input ApplyInput,
) (ApplyResult, error) {
	if err := validateCandidate(input.Candidate); err != nil {
		return ApplyResult{}, err
	}
	reason := normalizeText(input.Reason)
	if reason == "" || javascriptLength(reason) > 500 || input.ExpectedVersion < 1 {
		return ApplyResult{}, apperror.Validation("A valid expectedVersion and reason are required")
	}
	if err := checkApplyCancellation(ctx, input); err != nil {
		return ApplyResult{}, err
	}
	current, err := service.store.Metadata(ctx, trackID)
	if err != nil {
		return ApplyResult{}, err
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
		content, lyricErr := service.lyrics(ctx, candidate)
		switch {
		case lyricErr != nil:
			warnings = append(warnings, "Lyrics retrieval failed: "+messageOf(lyricErr))
		case strings.TrimSpace(content) == "":
			warnings = append(warnings, "No lyrics were returned")
		default:
			format := "PLAIN"
			if lrcPattern.MatchString(content) {
				format = "LRC"
			}
			lyrics := MetadataLyrics{Content: content, Format: format, Language: "und"}
			if !sameMetadataValue(lyrics, current.Effective.Lyrics) {
				patch["lyrics"] = lyrics
				appliedFields = append(appliedFields, "lyrics")
			}
		}
	}

	metadata := current
	if len(patch) > 0 {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		metadata, err = service.store.UpdateMetadata(ctx, actorID, traceID, trackID, input.ExpectedVersion, patch, reason)
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
	if input.Fields.Cover && candidate.AlbumImg != "" {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		albumID, lookupErr := service.store.TrackAlbumID(ctx, trackID)
		if lookupErr != nil {
			warnings = append(warnings, "Cover application failed: "+messageOf(lookupErr))
		} else if albumID == nil {
			warnings = append(warnings, "The track has no album; cover artwork was skipped")
		} else {
			artwork, artworkErr := service.music.DownloadArtwork(ctx, candidate.AlbumImg)
			if artworkErr == nil {
				artworkErr = checkApplyCancellation(ctx, input)
			}
			if artworkErr == nil {
				artworkErr = service.artwork.ApplyAlbumArtwork(ctx, actorID, traceID, *albumID, artwork)
			}
			if artworkErr != nil {
				warnings = append(warnings, "Cover application failed: "+messageOf(artworkErr))
			} else {
				coverApplied = true
			}
		}
	}
	if len(appliedFields) == 0 && !coverApplied && len(warnings) == 0 {
		warnings = append(warnings, "Existing metadata already has the selected values")
	}

	var writebackJob *WritebackJob
	if input.WriteBack {
		if err := checkApplyCancellation(ctx, input); err != nil {
			return ApplyResult{}, err
		}
		job, enqueueErr := service.store.EnqueueWriteback(ctx, actorID, traceID, trackID, metadata.Version, reason)
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

func (service *Service) lyrics(ctx context.Context, candidate Candidate) (string, error) {
	if candidate.Source == SourceAcoustID {
		return "", nil
	}
	text, err := service.music.Lyric(ctx, candidate.Source, candidate.ID)
	if err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	if candidate.Source == SourceQMusic && err != nil {
		return "", err
	}
	if candidate.Source != SourceQMusic && candidate.Name != "" {
		matches, searchErr := service.music.Search(ctx, SourceQMusic, candidate.Name)
		if searchErr != nil {
			return "", searchErr
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
			return service.music.Lyric(ctx, SourceQMusic, fallbacks[0].candidate.ID)
		}
	}
	return "", nil
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
	if candidate.ID == "" || javascriptLength(candidate.ID) > 2_000 || candidate.Name == "" || javascriptLength(candidate.Name) > 300 {
		return apperror.Validation("The scraping candidate is missing required fields")
	}
	if !isSearchableSource(candidate.Source) && candidate.Source != SourceAcoustID {
		return apperror.Validation("The scraping candidate source is invalid")
	}
	return nil
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
	if query.Artist != nil {
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
