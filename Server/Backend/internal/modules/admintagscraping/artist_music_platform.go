package admintagscraping

import (
	"context"
	"net/url"
	"strings"
)

func (platform *ProductionMusicPlatform) SearchArtists(
	ctx context.Context,
	source Source,
	query string,
) ([]ArtistCandidate, error) {
	key := string(source) + "\x00" + strings.TrimSpace(query)
	call, leader := platform.beginArtistSearch(key)
	if !leader {
		result, err := awaitArtistSearch(ctx, call)
		if err != nil {
			return nil, normalizeUpstreamError(err, ctx)
		}
		return result, nil
	}
	result, err := platform.searchArtistsUncached(ctx, source, query)
	platform.finishArtistSearch(key, call, result, err)
	if err != nil {
		return nil, normalizeUpstreamError(err, ctx)
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) searchArtistsUncached(
	ctx context.Context,
	source Source,
	query string,
) ([]ArtistCandidate, error) {
	switch source {
	case SourceQMusic:
		return platform.searchQQArtists(ctx, query)
	default:
		return nil, artistSourceValidationError()
	}
}
func (platform *ProductionMusicPlatform) searchQQArtists(
	ctx context.Context,
	query string,
) ([]ArtistCandidate, error) {
	parameters := url.Values{"format": {"json"}, "key": {query}}
	data, err := platform.requestJSON(
		ctx,
		"https://c.y.qq.com/splcloud/fcgi-bin/smartbox_new.fcg?"+parameters.Encode(),
		requestOptions{Headers: map[string]string{"Referer": "https://y.qq.com/"}},
	)
	if err != nil {
		return nil, err
	}
	result := make([]ArtistCandidate, 0)
	seen := make(map[string]struct{})
	for _, singerValue := range sliceValue(mapValue(mapValue(data["data"])["singer"])["itemlist"]) {
		singer := mapValue(singerValue)
		id := stringValue(singer["mid"])
		name := cleanScrapedText(singer["name"])
		if id == "" || name == "" {
			continue
		}
		key := string(SourceQMusic) + ":" + id
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ArtistCandidate{
			Source: SourceQMusic,
			ID:     id,
			Name:   name,
			ImageURL: "https://y.qq.com/music/photo_new/T001R500x500M000" +
				url.PathEscape(id) + ".jpg",
			Aliases: []string{},
		})
	}
	return result, nil
}
