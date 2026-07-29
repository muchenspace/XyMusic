package adminmetadata

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestPresentRevisionIncludesLyricTiming(t *testing.T) {
	record := RevisionRecord{
		ID:      "revision-1",
		TrackID: "track-1",
		Effective: json.RawMessage(`{
			"title":"Song","credits":[],"albumArtists":[],"album":null,
			"releaseDate":null,"trackNumber":null,"trackTotal":null,"discNumber":null,
			"discTotal":null,"genres":[],"bpm":null,"isrc":null,"comment":null,
			"copyright":null,"lyrics":{"content":"[00:01.00]<00:01.00>word","format":"LRC","language":"und","timing":"WORD"},
			"hasArtwork":false
		}`),
		Overrides: json.RawMessage(`{}`),
	}
	summary, err := presentRevision(record)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"timing":"WORD"`) {
		t.Fatalf("revision summary omitted lyric timing: %s", payload)
	}
}

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
		"lyrics":     map[string]any{"content": " lyric\r\nline 2\r", "format": "PLAIN", "language": "EN-us", "timing": "LINE"},
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

func TestMetadataLyricsRequireConsistentTiming(t *testing.T) {
	tests := []struct {
		name   string
		lyrics map[string]any
	}{
		{
			name:   "missing timing",
			lyrics: map[string]any{"content": "plain text", "format": "PLAIN", "language": "und"},
		},
		{
			name:   "plain word timing",
			lyrics: map[string]any{"content": "plain text", "format": "PLAIN", "language": "und", "timing": "WORD"},
		},
		{
			name:   "incomplete word timing",
			lyrics: map[string]any{"content": "[00:01.00]<00:01.00>word\n[00:02.00]line", "format": "LRC", "language": "und", "timing": "WORD"},
		},
		{
			name:   "word content declared as line",
			lyrics: map[string]any{"content": "[00:01.00]<00:01.00>word", "format": "LRC", "language": "und", "timing": "LINE"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validSnapshotValue()
			value["lyrics"] = test.lyrics
			if _, err := NormalizeMetadataSnapshot(value); !apperror.IsCode(err, apperror.CodeValidationError) {
				t.Fatalf("NormalizeMetadataSnapshot() error = %v", err)
			}
		})
	}
}

func TestNormalizeMetadataRejectsNonCanonicalLyricTiming(t *testing.T) {
	lyricsValue := func(timing string) map[string]any {
		return map[string]any{
			"content": "[00:01.00]<00:01.00>word", "format": "LRC", "language": "und", "timing": timing,
		}
	}
	for _, timing := range []string{"word", " WORD "} {
		t.Run("snapshot_"+timing, func(t *testing.T) {
			value := validSnapshotValue()
			value["lyrics"] = lyricsValue(timing)
			if _, err := NormalizeMetadataSnapshot(value); !apperror.IsCode(err, apperror.CodeValidationError) {
				t.Fatalf("NormalizeMetadataSnapshot() timing %q error = %v", timing, err)
			}
		})
		t.Run("overrides_"+timing, func(t *testing.T) {
			if _, err := NormalizeMetadataOverrides(map[string]any{"lyrics": lyricsValue(timing)}); !apperror.IsCode(err, apperror.CodeValidationError) {
				t.Fatalf("NormalizeMetadataOverrides() timing %q error = %v", timing, err)
			}
		})
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
