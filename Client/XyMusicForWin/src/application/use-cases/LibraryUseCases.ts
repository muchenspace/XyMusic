import type { LibraryRepository } from "../ports/LibraryRepository";
import type { FavoriteSort } from "../../domain/pagination";

export class LibraryUseCases {
  constructor(private readonly repository: LibraryRepository) {}

  favorites(sort: FavoriteSort, cursor?: string, limit = 50, signal?: AbortSignal) { return this.repository.getFavoriteTracks(sort, cursor, limit, signal); }
  history(cursor?: string, limit = 50, signal?: AbortSignal) { return this.repository.getHistoryTracks(cursor, limit, signal); }
  favorite(trackId: string, favorite: boolean, signal?: AbortSignal) {
    return this.repository.setFavorite(trackId, favorite, signal);
  }
}
