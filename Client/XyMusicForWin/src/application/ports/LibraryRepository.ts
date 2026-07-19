import type { Track } from "../../domain/music";
import type { CursorPage, FavoriteSort } from "../../domain/pagination";

export interface LibraryRepository {
  setFavorite(trackId: string, favorite: boolean, signal?: AbortSignal): Promise<void>;
  getFavoriteTracks(sort: FavoriteSort, cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Track>>;
  getHistoryTracks(cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Track>>;
}
