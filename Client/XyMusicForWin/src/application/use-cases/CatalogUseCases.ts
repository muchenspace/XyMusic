import type { CatalogRepository } from "../ports/CatalogRepository";
import type { PlaylistRepository } from "../ports/PlaylistRepository";

export class CatalogUseCases {
  constructor(
    private readonly repository: CatalogRepository,
    private readonly playlists: PlaylistRepository,
  ) {}

  async home(signal?: AbortSignal) {
    const results = await Promise.allSettled([
      this.repository.listPublishedTracks("PUBLISHED_DESC", undefined, 10, signal),
      this.repository.listAlbums("RELEASE_DATE_DESC", undefined, 1, signal),
      this.playlists.list("UPDATED_DESC", undefined, 50, signal),
    ]);
    if (results.every((result) => result.status === "rejected")) throw (results[0] as PromiseRejectedResult).reason;
    const tracks = fulfilled(results[0], { items: [], nextCursor: null });
    const albums = fulfilled(results[1], { items: [], nextCursor: null });
    const playlists = fulfilled(results[2], { items: [], nextCursor: null });
    return { featured: albums.items[0], playlists: playlists.items, tracks: tracks.items };
  }
  search(query: string, signal?: AbortSignal) { return this.repository.search(query, signal); }
  searchTracks(query: string, cursor: string, limit = 50, signal?: AbortSignal) { return this.repository.searchTracks(query, cursor, limit, signal); }
  searchArtists(query: string, cursor: string, limit = 50, signal?: AbortSignal) { return this.repository.searchArtists(query, cursor, limit, signal); }
  searchAlbums(query: string, cursor: string, limit = 50, signal?: AbortSignal) { return this.repository.searchAlbums(query, cursor, limit, signal); }
  lyrics(trackId: string, signal?: AbortSignal) { return this.repository.getLyrics(trackId, signal); }
  albumTracks(albumId: string, signal?: AbortSignal) { return this.repository.getAlbumTracks(albumId, signal); }
  artistTracks(artistId: string, signal?: AbortSignal) { return this.repository.getArtistTracks(artistId, signal); }
  albumTracksPage(albumId: string, cursor?: string, limit = 100, signal?: AbortSignal) { return this.repository.getAlbumTracksPage(albumId, cursor, limit, signal); }
  artistTracksPage(artistId: string, cursor?: string, limit = 100, signal?: AbortSignal) { return this.repository.getArtistTracksPage(artistId, cursor, limit, signal); }
  randomAlbums(limit: number, signal?: AbortSignal) { return this.repository.getRandomAlbums(limit, signal); }
  randomTracks(limit: number, signal?: AbortSignal) { return this.repository.getRandomTracks(limit, signal); }
}

function fulfilled<T>(result: PromiseSettledResult<T>, fallback: T): T {
  return result.status === "fulfilled" ? result.value : fallback;
}
