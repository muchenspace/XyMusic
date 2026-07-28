package admintagscraping

import (
	"context"
	"net/url"
)

func (platform *ProductionMusicPlatform) SearchArtists(
	ctx context.Context,
	source Source,
	query string,
) ([]ArtistCandidate, error) {
	var result []ArtistCandidate
	var err error
	switch source {
	case SourceQMusic:
		result, err = platform.searchQQArtists(ctx, query)
	default:
		return nil, artistSourceValidationError()
	}
	if err != nil {
		return nil, normalizeUpstreamError(err, ctx)
	}
	return result, nil
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
