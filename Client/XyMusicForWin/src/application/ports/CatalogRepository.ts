import type { Album, Artist, Lyrics, SearchResults, Track } from "../../domain/music";
import type { AlbumSort, CursorPage, TrackSort } from "../../domain/pagination";

export interface CatalogRepository {
  listPublishedTracks(sort: TrackSort, cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Track>>;
  search(query: string, signal?: AbortSignal): Promise<SearchResults>;
  searchTracks(query: string, cursor: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Track>>;
  searchArtists(query: string, cursor: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Artist>>;
  searchAlbums(query: string, cursor: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Album>>;
  getLyrics(trackId: string, signal?: AbortSignal): Promise<Lyrics | null>;
  getAlbumTracks(albumId: string, signal?: AbortSignal): Promise<Track[]>;
  getArtistTracks(artistId: string, signal?: AbortSignal): Promise<Track[]>;
  getAlbumTracksPage(albumId: string, cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Track>>;
  getArtistTracksPage(artistId: string, cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Track>>;
  getRandomAlbums(limit: number, signal?: AbortSignal): Promise<Album[]>;
  getRandomTracks(limit: number, signal?: AbortSignal): Promise<Track[]>;
  listAlbums(sort: AlbumSort, cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Album>>;
}
