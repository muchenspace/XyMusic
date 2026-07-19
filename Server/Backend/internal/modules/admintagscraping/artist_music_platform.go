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
	var result []ArtistCandidate
	var err error
	switch source {
	case SourceNetease:
		result, err = platform.searchNeteaseArtists(ctx, query)
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

func (platform *ProductionMusicPlatform) searchNeteaseArtists(
	ctx context.Context,
	query string,
) ([]ArtistCandidate, error) {
	data, err := platform.neteaseForward(ctx, "https://music.163.com/api/cloudsearch/pc", map[string]any{
		"s": query, "type": 100, "limit": 10, "offset": 0,
	})
	if err != nil {
		return nil, err
	}
	result := make([]ArtistCandidate, 0)
	seen := make(map[string]struct{})
	for _, value := range sliceValue(mapValue(data["result"])["artists"]) {
		artist := mapValue(value)
		id := stringValue(artist["id"])
		name := cleanScrapedText(artist["name"])
		imageURL := firstArtistImageURL(artist, "picUrl", "img1v1Url")
		if id == "" || name == "" || imageURL == "" {
			continue
		}
		key := string(SourceNetease) + ":" + id
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		aliases := artistAliases(artist["alias"], artist["transNames"])
		result = append(result, ArtistCandidate{
			Source: SourceNetease, ID: id, Name: name, ImageURL: imageURL, Aliases: aliases,
		})
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

func firstArtistImageURL(value map[string]any, fields ...string) string {
	for _, field := range fields {
		candidate := strings.TrimSpace(stringValue(value[field]))
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "http://") {
			candidate = "https://" + strings.TrimPrefix(candidate, "http://")
		}
		parsed, err := url.Parse(candidate)
		if err == nil && validateArtworkURL(parsed) == nil {
			return parsed.String()
		}
	}
	return ""
}

func artistAliases(values ...any) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, value := range values {
		for _, raw := range sliceValue(value) {
			alias := cleanScrapedText(raw)
			key := normalizeForTagMatch(alias)
			if alias == "" || key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, alias)
			if len(result) == 20 {
				return result
			}
		}
	}
	return result
}
