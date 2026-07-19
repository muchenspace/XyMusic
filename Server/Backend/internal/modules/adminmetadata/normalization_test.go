package adminmetadata

import (
	"reflect"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestNormalizeMetadataSnapshotAndOverrides(t *testing.T) {
	snapshot, err := NormalizeMetadataSnapshot(map[string]any{
		"title": "  Song\tTitle  ",
		"credits": []any{
			map[string]any{"name": "Artist", "role": "PRIMARY"},
			map[string]any{"name": " artist ", "role": "PRIMARY"},
			map[string]any{"name": "Writer", "role": "COMPOSER"},
		},
		"albumArtists": []string{"Artist", " artist "},
		"album":        "Album", "releaseDate": "2026-07",
		"trackNumber": 2, "trackTotal": 10,
		"discNumber": 1, "discTotal": 2,
		"genres": []any{"Rock", " rock ", "Pop"}, "bpm": 120.126,
		"isrc": "us-abc-1234567", "comment": " line 1\r\nline 2\rline 3 ",
		"copyright":  nil,
		"lyrics":     map[string]any{"content": " lyric\r\nline 2\r", "format": "PLAIN", "language": "EN-us"},
		"hasArtwork": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "Song Title" || len(snapshot.Credits) != 2 ||
		!reflect.DeepEqual(snapshot.AlbumArtists, []string{"Artist"}) ||
		!reflect.DeepEqual(snapshot.Genres, []string{"Rock", "Pop"}) ||
		snapshot.BPM == nil || *snapshot.BPM != 120.13 ||
		snapshot.ISRC == nil || *snapshot.ISRC != "USABC1234567" ||
		snapshot.Comment == nil || *snapshot.Comment != "line 1\nline 2\nline 3" ||
		snapshot.Lyrics == nil || snapshot.Lyrics.Language != "en-us" ||
		snapshot.Lyrics.Content != "lyric\nline 2" {
		t.Fatalf("snapshot=%+v", snapshot)
	}

	patch, err := NormalizeMetadataPatch(map[string]any{
		"album": nil, "trackNumber": 3, "bpm": 99.5, "comment": " note ",
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := UpdateMetadataOverrides(MetadataOverrides{"genres": []any{"Old"}}, patch, []string{"genres"})
	if err != nil {
		t.Fatal(err)
	}
	if next["album"] != nil || next["trackNumber"] != 3 || next["bpm"] != 99.5 ||
		next["comment"] != "note" {
		t.Fatalf("overrides=%#v", next)
	}
	if _, exists := next["genres"]; exists {
		t.Fatalf("reset field remains: %#v", next)
	}
	effective, err := ApplyMetadataOverrides(snapshot, next)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Album != nil || effective.TrackNumber == nil || *effective.TrackNumber != 3 ||
		effective.HasArtwork != snapshot.HasArtwork {
		t.Fatalf("effective=%+v", effective)
	}
}

func TestMetadataValidationRejectsInvalidRelationshipsAndUnknownFields(t *testing.T) {
	base := validSnapshotValue()
	base["trackNumber"] = nil
	base["trackTotal"] = 10
	if _, err := NormalizeMetadataSnapshot(base); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("track total error=%v", err)
	}
	if _, err := NormalizeMetadataPatch(map[string]any{"unknown": true}); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("unknown error=%v", err)
	}
	if _, err := UpdateMetadataOverrides(MetadataOverrides{}, map[string]any{"title": "x"}, []string{"title"}); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("patch/reset error=%v", err)
	}
}

func TestMetadataOverridesForTargetAndChangedFields(t *testing.T) {
	raw, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	target := raw
	target.Title = "Changed"
	target.Genres = []string{"Rock"}
	overrides, err := MetadataOverridesForTarget(raw, target)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedOverrideFields(overrides), []string{"title", "genres"}) {
		t.Fatalf("overrides=%#v", overrides)
	}
	if !reflect.DeepEqual(MetadataChangedFields(raw, target), []string{"title", "genres"}) {
		t.Fatalf("changed=%v", MetadataChangedFields(raw, target))
	}
}

func validSnapshotValue() map[string]any {
	return map[string]any{
		"title":        "Song",
		"credits":      []any{map[string]any{"name": "Artist", "role": "PRIMARY"}},
		"albumArtists": []any{"Artist"}, "album": nil, "releaseDate": nil,
		"trackNumber": nil, "trackTotal": nil, "discNumber": nil, "discTotal": nil,
		"genres": []any{}, "bpm": nil, "isrc": nil, "comment": nil,
		"copyright": nil, "lyrics": nil, "hasArtwork": false,
	}
}
