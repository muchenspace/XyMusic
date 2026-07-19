package admintagscraping

import (
	"context"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"

	"xymusic/server/internal/shared/apperror"
)

func (service *Service) SearchArtists(
	ctx context.Context,
	input ArtistSearchInput,
) ([]ArtistCandidate, error) {
	query := cleanScrapedText(input.Query)
	if query == "" || javascriptLength(query) > 300 {
		return nil, apperror.Validation("Artist search text must not be empty")
	}
	if input.Source != SourceSmart && !isArtistSearchableSource(input.Source) {
		return nil, artistSourceValidationError()
	}
	if input.Source != SourceSmart && len(input.Sources) > 0 {
		return nil, artistSourceValidationError()
	}

	sources := []Source{input.Source}
	if input.Source == SourceSmart {
		sources = validSmartArtistSources(input.Sources)
		if len(sources) == 0 {
			return nil, artistSourceValidationError()
		}
	}

	type artistSearchResult struct {
		source Source
		items  []ArtistCandidate
		err    error
	}
	results := make([]artistSearchResult, 0, len(sources))
	if input.Source != SourceSmart {
		items, err := service.music.SearchArtists(ctx, sources[0], query)
		if err != nil {
			return nil, err
		}
		results = append(results, artistSearchResult{source: sources[0], items: items})
	} else {
		channel := make(chan artistSearchResult, len(sources))
		var wait sync.WaitGroup
		for _, source := range sources {
			source := source
			wait.Add(1)
			go func() {
				defer wait.Done()
				items, err := service.music.SearchArtists(ctx, source, query)
				channel <- artistSearchResult{source: source, items: items, err: err}
			}()
		}
		wait.Wait()
		close(channel)
		for result := range channel {
			results = append(results, result)
		}
	}
	if input.Source == SourceSmart {
		successes := 0
		var firstError error
		for _, result := range results {
			if result.err == nil {
				successes++
			} else if firstError == nil {
				firstError = result.err
			}
		}
		if successes == 0 && firstError != nil {
			return nil, firstError
		}
	}

	seen := make(map[string]struct{})
	candidates := make([]ArtistCandidate, 0)
	for _, result := range results {
		if result.err != nil {
			continue
		}
		for _, candidate := range result.items {
			candidate.Source = result.source
			candidate.ID = strings.TrimSpace(candidate.ID)
			candidate.Name = cleanScrapedText(candidate.Name)
			candidate.ImageURL = strings.TrimSpace(candidate.ImageURL)
			candidate.Aliases = normalizeArtistAliases(candidate.Aliases)
			candidate.Score = artistCandidateScore(query, candidate)
			if candidate.Score <= 0 || validateArtistCandidate(candidate) != nil {
				continue
			}
			key := string(candidate.Source) + ":" + candidate.ID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].Score != candidates[right].Score {
			return candidates[left].Score > candidates[right].Score
		}
		return candidates[left].Name < candidates[right].Name
	})
	limit := 10
	if input.Source == SourceSmart {
		limit = 15
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func (service *Service) ApplyArtistArtwork(
	ctx context.Context,
	actorID string,
	traceID string,
	artistID string,
	input ArtistArtworkApplyInput,
) (ArtistArtworkApplyResult, error) {
	if input.ExpectedVersion < 1 || input.ExpectedVersion == math.MaxInt {
		return ArtistArtworkApplyResult{}, apperror.Validation("expectedVersion is invalid")
	}
	reason := normalizeText(input.Reason)
	if javascriptLength(reason) < 2 || javascriptLength(reason) > 500 {
		return ArtistArtworkApplyResult{}, apperror.Validation("reason is invalid")
	}
	if err := validateArtistCandidate(input.Candidate); err != nil {
		return ArtistArtworkApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ArtistArtworkApplyResult{}, err
	}
	artwork, err := service.music.DownloadArtwork(ctx, input.Candidate.ImageURL)
	if err != nil {
		return ArtistArtworkApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ArtistArtworkApplyResult{}, err
	}
	applyContext := withArtistArtworkDetails(ctx, reason, input.Candidate)
	if err := service.artwork.ApplyArtistArtwork(
		applyContext,
		actorID,
		traceID,
		artistID,
		input.ExpectedVersion,
		input.Overwrite,
		artwork,
	); err != nil {
		return ArtistArtworkApplyResult{}, err
	}
	return ArtistArtworkApplyResult{Applied: true, Version: input.ExpectedVersion + 1}, nil
}

func validateArtistCandidate(candidate ArtistCandidate) error {
	if !isArtistSearchableSource(candidate.Source) ||
		strings.TrimSpace(candidate.ID) == "" || javascriptLength(candidate.ID) > 2_000 ||
		cleanScrapedText(candidate.Name) == "" || javascriptLength(candidate.Name) > 300 ||
		strings.TrimSpace(candidate.ImageURL) == "" || javascriptLength(candidate.ImageURL) > 2_000 ||
		math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) || candidate.Score < 0 || candidate.Score > 2 ||
		len(candidate.Aliases) > 20 {
		return apperror.Validation("The artist artwork candidate is invalid")
	}
	for _, alias := range candidate.Aliases {
		if cleanScrapedText(alias) == "" || javascriptLength(alias) > 200 {
			return apperror.Validation("The artist artwork candidate is invalid")
		}
	}
	parsed, err := url.Parse(candidate.ImageURL)
	if err != nil || validateArtworkURL(parsed) != nil {
		return apperror.Validation("The artist artwork URL is invalid or untrusted")
	}
	allowed := []string(nil)
	switch candidate.Source {
	case SourceQMusic:
		allowed = []string{"y.qq.com"}
	case SourceNetease:
		allowed = []string{"music.126.net"}
	}
	if !allowedHost(parsed.Hostname(), allowed) {
		return apperror.Validation("The artist artwork URL does not match its source")
	}
	return nil
}

func normalizeArtistAliases(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, min(20, len(input)))
	for _, value := range input {
		value = cleanScrapedText(value)
		key := normalizeForTagMatch(value)
		if value == "" || key == "" || javascriptLength(value) > 200 {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) == 20 {
			break
		}
	}
	return result
}

func artistCandidateScore(query string, candidate ArtistCandidate) float64 {
	score := tagMatchScore(query, candidate.Name)
	for _, alias := range candidate.Aliases {
		score = max(score, tagMatchScore(query, alias))
	}
	return score
}

func validSmartArtistSources(input []Source) []Source {
	if len(input) == 0 {
		return append([]Source(nil), defaultSmartArtistSources...)
	}
	seen := make(map[Source]struct{}, len(input))
	result := make([]Source, 0, len(input))
	for _, source := range input {
		if !isArtistSearchableSource(source) {
			return nil
		}
		if _, exists := seen[source]; exists {
			return nil
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	return result
}

func isArtistSearchableSource(source Source) bool {
	for _, candidate := range searchableArtistSources {
		if source == candidate {
			return true
		}
	}
	return false
}

func artistSourceValidationError() error {
	return apperror.Validation("The artist scraping source is invalid")
}
