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
