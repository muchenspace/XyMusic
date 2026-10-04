package adminmetadata

import (
	"math"
	"regexp"

	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

// filterMetadataPatch is the metadata patch contract validator owned by the
// service layer. The transport routes only decode JSON and delegate field
// semantics (types, lengths, date formats) to this single service-layer
// implementation; the service normalization layer consumes the filtered
// patch.
func filterMetadataPatch(input map[string]any) (map[string]any, error) {
	patch := make(map[string]any)
	for _, field := range editableFields {
		name := string(field)
		value, present := input[name]
		if !present {
			continue
		}
		switch field {
		case FieldTitle:
			text, ok := routeString(value, 1, 300, nil)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = text
		case FieldCredits:
			credits, err := routeCredits(value)
			if err != nil {
				return nil, err
			}
			patch[name] = credits
		case FieldAlbumArtists:
			values, err := routeStringArray(value, 1, 100, 1, 200)
			if err != nil {
				return nil, err
			}
			patch[name] = values
		case FieldAlbum:
			if value == nil {
				patch[name] = nil
				continue
			}
			text, ok := routeString(value, 1, 300, nil)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = text
		case FieldReleaseDate:
			if value == nil {
				patch[name] = nil
				continue
			}
			text, ok := routeString(value, 1, 10, releaseDateRoutePattern)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = text
		case FieldTrackNumber, FieldTrackTotal:
			if value == nil {
				patch[name] = nil
				continue
			}
			number, ok := routeInteger(value, 1, 9_999)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = number
		case FieldDiscNumber, FieldDiscTotal:
			if value == nil {
				patch[name] = nil
				continue
			}
			number, ok := routeInteger(value, 1, 999)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = number
		case FieldGenres:
			values, err := routeStringArray(value, 0, 100, 1, 100)
			if err != nil {
				return nil, err
			}
			patch[name] = values
		case FieldBPM:
			if value == nil {
				patch[name] = nil
				continue
			}
			number, ok := routeNumber(value, 1, 999.99)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = number
		case FieldISRC:
			if value == nil {
				patch[name] = nil
				continue
			}
			text, ok := routeString(value, 12, 12, isrcRoutePattern)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = text
		case FieldComment:
			if value == nil {
				patch[name] = nil
				continue
			}
			text, ok := routeString(value, 1, 20_000, nil)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = text
		case FieldCopyright:
			if value == nil {
				patch[name] = nil
				continue
			}
			text, ok := routeString(value, 1, 2_000, nil)
			if !ok {
				return nil, routeContractError()
			}
			patch[name] = text
		case FieldLyrics:
			if value == nil {
				patch[name] = nil
				continue
			}
			lyrics, err := routeLyrics(value)
			if err != nil {
				return nil, err
			}
			patch[name] = lyrics
		}
	}
	return patch, nil
}

func routeCredits(value any) ([]any, error) {
	items, ok := value.([]any)
	if !ok || len(items) < 1 || len(items) > 100 {
		return nil, routeContractError()
	}
	result := make([]any, 0, len(items))
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, routeContractError()
		}
		name, err := requiredRouteString(item, "name", 1, 200, nil)
		if err != nil {
			return nil, err
		}
		role, err := requiredRouteString(item, "role", 1, 20, nil)
		if err != nil || !validCreditRole(CreditRole(role)) {
			return nil, routeContractError()
		}
		result = append(result, map[string]any{"name": name, "role": role})
	}
	return result, nil
}

func routeLyrics(value any) (map[string]any, error) {
	item, ok := value.(map[string]any)
	if !ok {
		return nil, routeContractError()
	}
	content, err := requiredRouteString(item, "content", 1, 500_000, nil)
	if err != nil {
		return nil, err
	}
	format, err := requiredRouteString(item, "format", 1, 10, nil)
	if err != nil || (format != "LRC" && format != "PLAIN") {
		return nil, routeContractError()
	}
	language, err := requiredRouteString(item, "language", 1, 35, languageRoutePattern)
	if err != nil {
		return nil, err
	}
	rawTiming, exists := item["timing"]
	if !exists {
		return nil, routeContractError()
	}
	timing, ok := rawTiming.(string)
	if !ok {
		return nil, routeContractError()
	}
	if !sharedlyrics.ValidTiming(timing) {
		return nil, routeContractError()
	}
	if err := sharedlyrics.ValidateDocument(format, sharedlyrics.Timing(timing), content); err != nil {
		return nil, routeContractError()
	}
	return map[string]any{"content": content, "format": format, "language": language, "timing": timing}, nil
}

func routeStringArray(value any, minItems, maxItems, minLength, maxLength int) ([]any, error) {
	items, ok := value.([]any)
	if !ok || len(items) < minItems || len(items) > maxItems {
		return nil, routeContractError()
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		text, ok := routeString(item, minLength, maxLength, nil)
		if !ok {
			return nil, routeContractError()
		}
		result = append(result, text)
	}
	return result, nil
}

func routeNumber(value any, minimum, maximum float64) (float64, bool) {
	number, ok := floatingNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < minimum || number > maximum {
		return 0, false
	}
	return number, true
}

var (
	releaseDateRoutePattern = regexp.MustCompile(`^[0-9]{4}(?:-[0-9]{2}(?:-[0-9]{2})?)?$`)
	isrcRoutePattern        = regexp.MustCompile(`^[A-Za-z]{2}[A-Za-z0-9]{3}[0-9]{7}$`)
	languageRoutePattern    = regexp.MustCompile(`^(?:[A-Za-z]{2,8}(?:-[A-Za-z0-9]{2,8})*|und)$`)
)
