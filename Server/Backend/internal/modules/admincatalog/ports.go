package admincatalog

import (
	"context"

	"xymusic/server/internal/modules/catalog"
)

type Store interface {
	ListArtists(context.Context, ArtistQuery) ([]ArtistRecord, int, error)
	FindArtist(context.Context, string) (ArtistRecord, error)
	ListAlbums(context.Context, AlbumQuery) ([]AlbumRecord, int, error)
	FindDuplicateAlbums(context.Context, DuplicateAlbumQuery) (DuplicateAlbumPage, error)
	FindAlbum(context.Context, string, int, int) (AlbumRecord, []TrackRecord, int, error)
	ListTracks(context.Context, TrackQuery) ([]TrackRecord, int, error)
	FindTrack(context.Context, string, int, int) (TrackRecord, int, error)
}

type ArtworkPresenter interface {
	Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error)
}

// CursorStore contains the keyset implementations for detail collections. It
// is kept separate from Store so small/local test stores can retain the legacy
// offset methods while production uses cursor pagination for every admin list.
type CursorStore interface {
	FindAlbumCursor(context.Context, string, int, *AlbumTrackCursor, *int) (AlbumRecord, []TrackRecord, int, error)
	FindTrackCursor(context.Context, string, int, *TrackLyricCursor, *int) (TrackRecord, int, error)
}
