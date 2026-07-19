package adminmetadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/shared/apperror"
)

var (
	languagePattern = regexp.MustCompile(`^(?:[a-z]{2,8}(?:-[a-z0-9]{2,8})*|und)$`)
	isrcPattern     = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{3}[0-9]{7}$`)
)

func NormalizeMetadataSnapshot(value any) (MetadataSnapshot, error) {
	input, err := objectValue(value, "Metadata snapshot")
	if err != nil {
		return MetadataSnapshot{}, err
	}
	allowed := editableFieldSet()
	allowed["hasArtwork"] = struct{}{}
	if err := rejectUnknownKeys(input, allowed); err != nil {
		return MetadataSnapshot{}, err
	}

	credits, err := normalizeCredits(input["credits"], true)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	primary := make([]string, 0, len(credits))
	for _, credit := range credits {
		if credit.Role == CreditPrimary {
			primary = append(primary, credit.Name)
		}
	}
	albumArtists, err := normalizeAlbumArtists(input["albumArtists"], primary, true)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	title, err := requiredText(input["title"], "title", 300)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	album, err := nullableText(input["album"], "album", 300, false)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	releaseDate, err := normalizeReleaseDate(input["releaseDate"])
	if err != nil {
		return MetadataSnapshot{}, err
	}
	trackNumber, err := nullableInteger(input["trackNumber"], "trackNumber", 1, 9_999)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	trackTotal, err := nullableInteger(input["trackTotal"], "trackTotal", 1, 9_999)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	discNumber, err := nullableInteger(input["discNumber"], "discNumber", 1, 999)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	discTotal, err := nullableInteger(input["discTotal"], "discTotal", 1, 999)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	genres, err := normalizeStringArray(input["genres"], "genres", 100, 100)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	bpm, err := nullableNumber(input["bpm"], "bpm", 1, 999.99)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	isrc, err := normalizeISRC(input["isrc"])
	if err != nil {
		return MetadataSnapshot{}, err
	}
	comment, err := nullableText(input["comment"], "comment", 20_000, true)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	copyright, err := nullableText(input["copyright"], "copyright", 2_000, true)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	lyrics, err := normalizeLyrics(input["lyrics"])
	if err != nil {
		return MetadataSnapshot{}, err
	}
	hasArtwork, ok := input["hasArtwork"].(bool)
	if !ok {
		return MetadataSnapshot{}, apperror.Validation("hasArtwork must be a boolean")
	}
	snapshot := MetadataSnapshot{
		Title: title, Credits: credits, AlbumArtists: albumArtists, Album: album,
		ReleaseDate: releaseDate, TrackNumber: trackNumber, TrackTotal: trackTotal,
		DiscNumber: discNumber, DiscTotal: discTotal, Genres: genres, BPM: bpm,
		ISRC: isrc, Comment: comment, Copyright: copyright, Lyrics: lyrics,
		HasArtwork: hasArtwork,
	}
	if err := validateNumberTotals(snapshot); err != nil {
		return MetadataSnapshot{}, err
	}
	return snapshot, nil
}

func NormalizeMetadataPatch(value any) (MetadataOverrides, error) {
	input, err := objectValue(value, "Metadata patch")
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(input, editableFieldSet()); err != nil {
		return nil, err
	}
	patch := make(MetadataOverrides, len(input))
	for key, value := range input {
		switch EditableField(key) {
		case FieldTitle:
			patch[key], err = requiredText(value, key, 300)
		case FieldCredits:
			patch[key], err = normalizeCredits(value, false)
		case FieldAlbumArtists:
			patch[key], err = normalizeAlbumArtists(value, nil, false)
		case FieldAlbum:
			var normalized *string
			normalized, err = nullableText(value, key, 300, false)
			patch[key] = optionalValue(normalized)
		case FieldReleaseDate:
			var normalized *string
			normalized, err = normalizeReleaseDate(value)
			patch[key] = optionalValue(normalized)
		case FieldTrackNumber, FieldTrackTotal:
			var normalized *int
			normalized, err = nullableInteger(value, key, 1, 9_999)
			patch[key] = optionalValue(normalized)
		case FieldDiscNumber, FieldDiscTotal:
			var normalized *int
			normalized, err = nullableInteger(value, key, 1, 999)
			patch[key] = optionalValue(normalized)
		case FieldGenres:
			patch[key], err = normalizeStringArray(value, key, 100, 100)
		case FieldBPM:
			var normalized *float64
			normalized, err = nullableNumber(value, key, 1, 999.99)
			patch[key] = optionalValue(normalized)
		case FieldISRC:
			var normalized *string
			normalized, err = normalizeISRC(value)
			patch[key] = optionalValue(normalized)
		case FieldComment:
			var normalized *string
			normalized, err = nullableText(value, key, 20_000, true)
			patch[key] = optionalValue(normalized)
		case FieldCopyright:
			var normalized *string
			normalized, err = nullableText(value, key, 2_000, true)
			patch[key] = optionalValue(normalized)
		case FieldLyrics:
			var normalized *MetadataLyrics
			normalized, err = normalizeLyrics(value)
			patch[key] = optionalValue(normalized)
		default:
			err = apperror.Validation("Unknown metadata field: " + key)
		}
		if err != nil {
			return nil, err
		}
	}
	return patch, nil
}

func NormalizeMetadataOverrides(value any) (MetadataOverrides, error) {
	return NormalizeMetadataPatch(value)
}

func ApplyMetadataOverrides(raw MetadataSnapshot, overrides MetadataOverrides) (MetadataSnapshot, error) {
	combined, err := snapshotObject(raw)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	for key, value := range overrides {
		combined[key] = value
	}
	combined["hasArtwork"] = raw.HasArtwork
	return NormalizeMetadataSnapshot(combined)
}

func UpdateMetadataOverrides(
	current MetadataOverrides,
	patchValue any,
	resetFields []string,
) (MetadataOverrides, error) {
	patch, err := NormalizeMetadataPatch(patchValue)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(resetFields))
	for _, field := range resetFields {
		if _, valid := editableFieldSet()[field]; !valid {
			return nil, apperror.Validation("Unknown metadata field: " + field)
		}
		if _, duplicate := seen[field]; duplicate {
			return nil, apperror.Validation("resetFields must contain unique metadata fields")
		}
		seen[field] = struct{}{}
		if _, patched := patch[field]; patched {
			return nil, apperror.Validation("Metadata field cannot be patched and reset together: " + field)
		}
	}
	if len(patch) == 0 && len(resetFields) == 0 {
		return nil, apperror.Validation("At least one metadata field must be changed")
	}
	next := make(MetadataOverrides, len(current)+len(patch))
	for key, value := range current {
		next[key] = value
	}
	for key, value := range patch {
		next[key] = value
	}
	for field := range seen {
		delete(next, field)
	}
	return NormalizeMetadataOverrides(next)
}

func MetadataChangedFields(previous, next MetadataSnapshot) []string {
	left, _ := snapshotObject(previous)
	right, _ := snapshotObject(next)
	fields := make([]string, 0, len(editableFields))
	for _, field := range editableFields {
		if !stableEqual(left[string(field)], right[string(field)]) {
			fields = append(fields, string(field))
		}
	}
	return fields
}

func MetadataSnapshotsEqual(left, right MetadataSnapshot) bool {
	return stableEqual(left, right)
}

func MetadataOverridesForTarget(raw, target MetadataSnapshot) (MetadataOverrides, error) {
	rawObject, err := snapshotObject(raw)
	if err != nil {
		return nil, err
	}
	targetObject, err := snapshotObject(target)
	if err != nil {
		return nil, err
	}
	overrides := make(MetadataOverrides)
	for _, field := range editableFields {
		name := string(field)
		if !stableEqual(rawObject[name], targetObject[name]) {
			overrides[name] = targetObject[name]
		}
	}
	return NormalizeMetadataOverrides(overrides)
}

func decodeSnapshot(raw json.RawMessage) (MetadataSnapshot, error) {
	value, err := decodeJSONValue(raw)
	if err != nil {
		return MetadataSnapshot{}, fmt.Errorf("decode metadata snapshot: %w", err)
	}
	return NormalizeMetadataSnapshot(value)
}

func decodeOverrides(raw json.RawMessage) (MetadataOverrides, error) {
	value, err := decodeJSONValue(raw)
	if err != nil {
		return nil, fmt.Errorf("decode metadata overrides: %w", err)
	}
	return NormalizeMetadataOverrides(value)
}

func encodeJSON(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode metadata JSON: %w", err)
	}
	return encoded, nil
}

func objectValue(value any, label string) (map[string]any, error) {
	if raw, ok := value.(json.RawMessage); ok {
		decoded, err := decodeJSONValue(raw)
		if err != nil {
			return nil, apperror.Validation(label + " must be an object")
		}
		value = decoded
	}
	if raw, ok := value.([]byte); ok {
		decoded, err := decodeJSONValue(raw)
		if err != nil {
			return nil, apperror.Validation(label + " must be an object")
		}
		value = decoded
	}
	if typed, ok := value.(MetadataOverrides); ok {
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = child
		}
		return result, nil
	}
	input, ok := value.(map[string]any)
	if !ok || input == nil {
		return nil, apperror.Validation(label + " must be an object")
	}
	return input, nil
}

func decodeJSONValue(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("multiple JSON values")
	}
	return value, nil
}

func snapshotObject(snapshot MetadataSnapshot) (map[string]any, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	value, err := decodeJSONValue(raw)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func normalizeCredits(value any, addFallbackPrimary bool) ([]MetadataCredit, error) {
	items, ok := value.([]any)
	if !ok {
		if typed, typedOK := value.([]MetadataCredit); typedOK {
			items = make([]any, len(typed))
			for index := range typed {
				items[index] = map[string]any{"name": typed[index].Name, "role": string(typed[index].Role)}
			}
		} else {
			return nil, apperror.Validation("credits must be an array")
		}
	}
	if len(items) > 100 {
		return nil, apperror.Validation("credits cannot contain more than 100 entries")
	}
	seen := make(map[string]struct{}, len(items))
	credits := make([]MetadataCredit, 0, len(items)+1)
	for _, item := range items {
		credit, err := objectValue(item, "credit")
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(credit, map[string]struct{}{"name": {}, "role": {}}); err != nil {
			return nil, err
		}
		name, err := requiredText(credit["name"], "credit.name", 200)
		if err != nil {
			return nil, err
		}
		role, ok := credit["role"].(string)
		if !ok || !validCreditRole(CreditRole(role)) {
			return nil, apperror.Validation("credit.role is invalid")
		}
		key := role + ":" + normalizeLookup(name)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		credits = append(credits, MetadataCredit{Name: name, Role: CreditRole(role)})
	}
	hasPrimary := false
	for _, credit := range credits {
		if credit.Role == CreditPrimary {
			hasPrimary = true
			break
		}
	}
	if len(credits) == 0 || !hasPrimary {
		if !addFallbackPrimary {
			return nil, apperror.Validation("At least one PRIMARY artist credit is required")
		}
		credits = append([]MetadataCredit{{Name: "Unknown Artist", Role: CreditPrimary}}, credits...)
	}
	return credits, nil
}

func normalizeAlbumArtists(value any, fallback []string, allowFallback bool) ([]string, error) {
	if (value == nil) && allowFallback {
		return append([]string(nil), fallback...), nil
	}
	artists, err := normalizeStringArray(value, "albumArtists", 100, 200)
	if err != nil {
		return nil, err
	}
	if len(artists) > 0 {
		return artists, nil
	}
	if allowFallback && len(fallback) > 0 {
		return append([]string(nil), fallback...), nil
	}
	return nil, apperror.Validation("At least one album artist is required")
}

func normalizeLyrics(value any) (*MetadataLyrics, error) {
	if value == nil {
		return nil, nil
	}
	if typed, ok := value.(MetadataLyrics); ok {
		value = map[string]any{"content": typed.Content, "format": typed.Format, "language": typed.Language}
	}
	if typed, ok := value.(*MetadataLyrics); ok {
		if typed == nil {
			return nil, nil
		}
		value = map[string]any{"content": typed.Content, "format": typed.Format, "language": typed.Language}
	}
	input, err := objectValue(value, "lyrics")
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(input, map[string]struct{}{"content": {}, "format": {}, "language": {}}); err != nil {
		return nil, err
	}
	content, err := requiredRawText(input["content"], "lyrics.content", 500_000)
	if err != nil {
		return nil, err
	}
	format, ok := input["format"].(string)
	if !ok || (format != "LRC" && format != "PLAIN") {
		return nil, apperror.Validation("lyrics.format is invalid")
	}
	language, err := requiredText(input["language"], "lyrics.language", 35)
	if err != nil {
		return nil, err
	}
	language = strings.ToLower(language)
	if !languagePattern.MatchString(language) {
		return nil, apperror.Validation("lyrics.language must be a valid language tag")
	}
	return &MetadataLyrics{Content: content, Format: format, Language: language}, nil
}

func normalizeReleaseDate(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if pointer, ok := value.(*string); ok {
		if pointer == nil {
			return nil, nil
		}
		value = *pointer
	}
	candidate, ok := value.(string)
	if !ok {
		return nil, apperror.Validation("releaseDate must be a date string or null")
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return nil, nil
	}
	if matched, _ := regexp.MatchString(`^[0-9]{4}$`, candidate); matched {
		return &candidate, nil
	}
	if matched, _ := regexp.MatchString(`^[0-9]{4}-[0-9]{2}$`, candidate); matched {
		month, _ := strconv.Atoi(candidate[5:7])
		if month >= 1 && month <= 12 {
			return &candidate, nil
		}
		return nil, apperror.Validation("releaseDate is invalid")
	}
	if matched, _ := regexp.MatchString(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`, candidate); !matched {
		return nil, apperror.Validation("releaseDate must use YYYY, YYYY-MM or YYYY-MM-DD")
	}
	parsed, err := time.Parse("2006-01-02", candidate)
	if err != nil || parsed.Format("2006-01-02") != candidate {
		return nil, apperror.Validation("releaseDate is invalid")
	}
	return &candidate, nil
}

func normalizeISRC(value any) (*string, error) {
	text, err := nullableText(value, "isrc", 14, false)
	if err != nil || text == nil {
		return text, err
	}
	normalized := strings.ToUpper(strings.ReplaceAll(*text, "-", ""))
	if !isrcPattern.MatchString(normalized) {
		return nil, apperror.Validation("isrc must be a valid 12-character ISRC")
	}
	return &normalized, nil
}

func normalizeStringArray(value any, field string, maxItems, maxLength int) ([]string, error) {
	var items []any
	switch typed := value.(type) {
	case []any:
		items = typed
	case []string:
		items = make([]any, len(typed))
		for index := range typed {
			items[index] = typed[index]
		}
	default:
		return nil, apperror.Validation(field + " must be an array")
	}
	if len(items) > maxItems {
		return nil, apperror.Validation(field + " contains too many entries")
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, err := requiredText(item, field, maxLength)
		if err != nil {
			return nil, err
		}
		key := normalizeLookup(text)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, text)
	}
	return result, nil
}

func requiredText(value any, field string, maximum int) (string, error) {
	if pointer, ok := value.(*string); ok {
		if pointer == nil {
			return "", apperror.Validation(field + " must be a string")
		}
		value = *pointer
	}
	text, ok := value.(string)
	if !ok {
		return "", apperror.Validation(field + " must be a string")
	}
	if strings.ContainsRune(text, '\x00') {
		return "", apperror.Validation(field + " contains an invalid character")
	}
	text = strings.Join(strings.Fields(norm.NFKC.String(text)), " ")
	if text == "" || javascriptLength(text) > maximum {
		return "", apperror.Validation(field + " is invalid")
	}
	return text, nil
}

func requiredRawText(value any, field string, maximum int) (string, error) {
	if pointer, ok := value.(*string); ok {
		if pointer == nil {
			return "", apperror.Validation(field + " must be a string")
		}
		value = *pointer
	}
	text, ok := value.(string)
	if !ok {
		return "", apperror.Validation(field + " must be a string")
	}
	if strings.ContainsRune(text, '\x00') {
		return "", apperror.Validation(field + " contains an invalid character")
	}
	text = strings.TrimSpace(normalizeLineEndings(norm.NFC.String(text)))
	if text == "" || javascriptLength(text) > maximum {
		return "", apperror.Validation(field + " is invalid")
	}
	return text, nil
}

func normalizeLineEndings(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func nullableText(value any, field string, maximum int, preserveWhitespace bool) (*string, error) {
	if pointer, ok := value.(*string); ok {
		if pointer == nil {
			return nil, nil
		}
		value = *pointer
	}
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok && text == "" {
		return nil, nil
	}
	var (
		text string
		err  error
	)
	if preserveWhitespace {
		text, err = requiredRawText(value, field, maximum)
	} else {
		text, err = requiredText(value, field, maximum)
	}
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func nullableInteger(value any, field string, minimum, maximum int) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if pointer, ok := value.(*int); ok {
		if pointer == nil {
			return nil, nil
		}
		value = *pointer
	}
	number, ok := exactInteger(value)
	if !ok || number < int64(minimum) || number > int64(maximum) {
		return nil, apperror.Validation(field + " is invalid")
	}
	converted := int(number)
	return &converted, nil
}

func nullableNumber(value any, field string, minimum, maximum float64) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	if pointer, ok := value.(*float64); ok {
		if pointer == nil {
			return nil, nil
		}
		value = *pointer
	}
	number, ok := floatingNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < minimum || number > maximum {
		return nil, apperror.Validation(field + " is invalid")
	}
	number = math.Round(number*100) / 100
	return &number, nil
}

func validateNumberTotals(snapshot MetadataSnapshot) error {
	if snapshot.TrackTotal != nil && snapshot.TrackNumber == nil {
		return apperror.Validation("trackTotal requires trackNumber")
	}
	if snapshot.TrackNumber != nil && snapshot.TrackTotal != nil && *snapshot.TrackNumber > *snapshot.TrackTotal {
		return apperror.Validation("trackNumber cannot exceed trackTotal")
	}
	if snapshot.DiscTotal != nil && snapshot.DiscNumber == nil {
		return apperror.Validation("discTotal requires discNumber")
	}
	if snapshot.DiscNumber != nil && snapshot.DiscTotal != nil && *snapshot.DiscNumber > *snapshot.DiscTotal {
		return apperror.Validation("discNumber cannot exceed discTotal")
	}
	return nil
}

func rejectUnknownKeys(input map[string]any, allowed map[string]struct{}) error {
	for key := range input {
		if _, valid := allowed[key]; !valid {
			return apperror.Validation("Unknown metadata field: " + key)
		}
	}
	return nil
}

func editableFieldSet() map[string]struct{} {
	result := make(map[string]struct{}, len(editableFields))
	for _, field := range editableFields {
		result[string(field)] = struct{}{}
	}
	return result
}

func validCreditRole(role CreditRole) bool {
	return role == CreditPrimary || role == CreditFeatured || role == CreditComposer ||
		role == CreditLyricist || role == CreditProducer
}

func exactInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.Trunc(number) != number || math.Abs(number) > float64(1<<53-1) {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		parsed, err := strconv.ParseInt(string(number), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func floatingNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case json.Number:
		parsed, err := strconv.ParseFloat(string(number), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stableEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func normalizeLookup(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}

func javascriptLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func sortedOverrideFields(overrides MetadataOverrides) []string {
	fields := make([]string, 0, len(overrides))
	for _, field := range editableFields {
		if _, present := overrides[string(field)]; present {
			fields = append(fields, string(field))
		}
	}
	return fields
}

func optionalValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}
