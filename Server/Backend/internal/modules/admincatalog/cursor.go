package admincatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

// listCursorValue is intentionally small: cursors are sent on every page
// request and must carry only the last ordered value plus the deterministic
// tie-breaker. The query scope is signed by CursorCodec so a cursor cannot be
// reused with a different filter/order.
type listCursorValue struct {
	Value string `json:"value"`
	ID    string `json:"id"`
	Null  bool   `json:"null,omitempty"`
	Total *int   `json:"total,omitempty"`
}

func trackCursorScope(input TrackListInput) string {
	return scopedListCursor("tracks", strings.Join([]string{
		strings.TrimSpace(input.Search), input.Sort, string(input.Order),
		string(input.Status), string(input.MetadataStatus), input.SourceID,
	}, "\x00"))
}

func listCursorTotalHint(cursor *ListCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func scopedListCursor(kind, value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("admin:%s:%s", kind, hex.EncodeToString(digest[:]))
}

func decodeTrackCursor(codec *pagination.CursorCodec, scope, encoded, sort string) (*ListCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[listCursorValue](codec, scope, encoded)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidCursor()
	}
	if sort == "createdAt" || sort == "updatedAt" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, value.Value)
		if parseErr != nil {
			return nil, invalidCursor()
		}
		value.Value = parsed.UTC().Format(time.RFC3339Nano)
	}
	return &ListCursor{Value: value.Value, ID: value.ID, Null: value.Null, Total: value.Total}, nil
}

func encodeTrackCursor(codec *pagination.CursorCodec, scope, sort string, record TrackRecord, total int) (string, error) {
	totalValue := total
	value := listCursorValue{ID: record.ID, Total: &totalValue}
	switch sort {
	case "title":
		value.Value = record.NormalizedTitle
	case "createdAt":
		value.Value = record.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updatedAt":
		value.Value = record.UpdatedAt.UTC().Format(time.RFC3339Nano)
	case "status":
		value.Value = string(record.AudioStatus)
	default:
		return "", invalidCursor()
	}
	return pagination.EncodeCursor(codec, scope, value)
}

func invalidCursor() error {
	return apperror.InvalidCursor("\u5206\u9875\u6e38\u6807\u65e0\u6548")
}

func artistCursorScope(input ListInput) string {
	return scopedListCursor("artists", strings.Join([]string{
		strings.TrimSpace(input.Search), input.Sort, string(input.Order),
	}, "\x00"))
}

func albumCursorScope(input ListInput) string {
	return scopedListCursor("albums", strings.Join([]string{
		strings.TrimSpace(input.Search), input.Sort, string(input.Order),
	}, "\x00"))
}

func decodeCatalogCursor(
	codec *pagination.CursorCodec,
	scope, encoded, sort string,
	releaseDate bool,
) (*ListCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[listCursorValue](codec, scope, encoded)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidCursor()
	}
	if releaseDate {
		if value.Null {
			if value.Value != "" {
				return nil, invalidCursor()
			}
		} else if parsed, parseErr := time.Parse("2006-01-02", value.Value); parseErr != nil {
			return nil, invalidCursor()
		} else {
			value.Value = parsed.Format("2006-01-02")
		}
	} else if sort == "createdAt" || sort == "updatedAt" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, value.Value)
		if parseErr != nil {
			return nil, invalidCursor()
		}
		value.Value = parsed.UTC().Format(time.RFC3339Nano)
	}
	return &ListCursor{Value: value.Value, ID: value.ID, Null: value.Null, Total: value.Total}, nil
}

func encodeArtistCursor(codec *pagination.CursorCodec, scope, sort string, record ArtistRecord, total int) (string, error) {
	totalValue := total
	value := listCursorValue{ID: record.ID, Total: &totalValue}
	switch sort {
	case "name":
		value.Value = record.NormalizedName
	case "createdAt":
		value.Value = record.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updatedAt":
		value.Value = record.UpdatedAt.UTC().Format(time.RFC3339Nano)
	default:
		return "", invalidCursor()
	}
	return pagination.EncodeCursor(codec, scope, value)
}

func encodeAlbumCursor(codec *pagination.CursorCodec, scope, sort string, record AlbumRecord, total int) (string, error) {
	totalValue := total
	value := listCursorValue{ID: record.ID, Total: &totalValue}
	switch sort {
	case "title":
		value.Value = record.NormalizedTitle
	case "createdAt":
		value.Value = record.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updatedAt":
		value.Value = record.UpdatedAt.UTC().Format(time.RFC3339Nano)
	case "releaseDate":
		if record.ReleaseDate == nil {
			value.Null = true
		} else {
			value.Value = *record.ReleaseDate
		}
	default:
		return "", invalidCursor()
	}
	return pagination.EncodeCursor(codec, scope, value)
}

type duplicateGroupCursorValue struct {
	Key   string `json:"key"`
	Total *int   `json:"total,omitempty"`
}

type duplicateAlbumCursorValue struct {
	ID    string `json:"id"`
	Total *int   `json:"total,omitempty"`
}

func duplicateGroupCursorScope(input DuplicateAlbumInput) string {
	return scopedListCursor("duplicate-albums", input.AlbumID)
}

func duplicateAlbumCursorScope(input DuplicateAlbumInput) string {
	return scopedListCursor("duplicate-album-members", input.AlbumID)
}

func duplicateGroupCursorTotalHint(cursor *DuplicateGroupCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func duplicateAlbumCursorTotalHint(cursor *DuplicateAlbumCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func decodeDuplicateGroupCursor(codec *pagination.CursorCodec, scope, encoded string) (*DuplicateGroupCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[duplicateGroupCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.Key == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidCursor()
	}
	return &DuplicateGroupCursor{Key: value.Key, Total: value.Total}, nil
}

func encodeDuplicateGroupCursor(codec *pagination.CursorCodec, scope string, group DuplicateAlbumGroupPage, total int) (string, error) {
	if group.Key == "" {
		return "", invalidCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, duplicateGroupCursorValue{Key: group.Key, Total: &totalValue})
}

func decodeDuplicateAlbumCursor(codec *pagination.CursorCodec, scope, encoded string) (*DuplicateAlbumCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[duplicateAlbumCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidCursor()
	}
	return &DuplicateAlbumCursor{ID: value.ID, Total: value.Total}, nil
}

func encodeDuplicateAlbumCursor(codec *pagination.CursorCodec, scope string, album AlbumRecord, total int) (string, error) {
	if album.ID == "" {
		return "", invalidCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, duplicateAlbumCursorValue{ID: album.ID, Total: &totalValue})
}

type albumTrackCursorValue struct {
	DiscNumber      *int   `json:"discNumber"`
	TrackNumber     *int   `json:"trackNumber"`
	NormalizedTitle string `json:"normalizedTitle"`
	ID              string `json:"id"`
	Total           *int   `json:"total,omitempty"`
}

type trackLyricCursorValue struct {
	IsDefault bool   `json:"isDefault"`
	Language  string `json:"language"`
	ID        string `json:"id"`
	Total     *int   `json:"total,omitempty"`
}

func albumTrackCursorScope(albumID string) string {
	return scopedListCursor("album-tracks", albumID)
}

func trackLyricCursorScope(trackID string) string {
	return scopedListCursor("track-lyrics", trackID)
}

func albumTrackCursorTotalHint(cursor *AlbumTrackCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func trackLyricCursorTotalHint(cursor *TrackLyricCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func decodeAlbumTrackCursor(codec *pagination.CursorCodec, scope, encoded string) (*AlbumTrackCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[albumTrackCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidCursor()
	}
	return &AlbumTrackCursor{DiscNumber: value.DiscNumber, TrackNumber: value.TrackNumber, NormalizedTitle: value.NormalizedTitle, ID: value.ID, Total: value.Total}, nil
}

func encodeAlbumTrackCursor(codec *pagination.CursorCodec, scope string, record TrackRecord, total int) (string, error) {
	if record.ID == "" {
		return "", invalidCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, albumTrackCursorValue{
		DiscNumber: record.DiscNumber, TrackNumber: record.TrackNumber,
		NormalizedTitle: record.NormalizedTitle, ID: record.ID, Total: &totalValue,
	})
}

func decodeTrackLyricCursor(codec *pagination.CursorCodec, scope, encoded string) (*TrackLyricCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[trackLyricCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidCursor()
	}
	return &TrackLyricCursor{IsDefault: value.IsDefault, Language: value.Language, ID: value.ID, Total: value.Total}, nil
}

func encodeTrackLyricCursor(codec *pagination.CursorCodec, scope string, record LyricRecord, total int) (string, error) {
	if record.ID == "" {
		return "", invalidCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, trackLyricCursorValue{IsDefault: record.IsDefault, Language: record.Language, ID: record.ID, Total: &totalValue})
}
