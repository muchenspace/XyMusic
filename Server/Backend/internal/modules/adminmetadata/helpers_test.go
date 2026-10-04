package adminmetadata

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

func intPointer(value int) *int { return &value }
