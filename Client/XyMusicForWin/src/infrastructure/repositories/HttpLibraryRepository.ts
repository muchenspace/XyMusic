import type { LibraryRepository } from "../../application/ports/LibraryRepository";
import type { Track } from "../../domain/music";
import type { CursorPage, FavoriteSort } from "../../domain/pagination";
import { ApiClient } from "../http/ApiClient";
import { mapCursorPage, normalizePageLimit, paginationError } from "../http/cursorPagination";
import { mapTrack } from "../http/musicMappers";
import type { TrackDto } from "../http/musicDtos";

export class HttpLibraryRepository implements LibraryRepository {
  constructor(private readonly api: ApiClient) {}

  async setFavorite(trackId: string, favorite: boolean, signal?: AbortSignal): Promise<void> {
    await this.api.request(`api/v1/library/favorites/${encodeURIComponent(trackId)}`, {
      method: favorite ? "PUT" : "DELETE",
      signal,
    });
  }

  async getFavoriteTracks(sort: FavoriteSort, cursor?: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Track>> {
    const params = new URLSearchParams({ sort, limit: String(normalizePageLimit(limit)) });
    if (cursor) params.set("cursor", cursor);
    const page = await this.api.request<{ items: Array<{ track: TrackDto }>; nextCursor?: string | null }>(`api/v1/library/favorites?${params}`, { signal });
    return mapCursorPage(page, (item) => {
      if (!item || typeof item !== "object" || !("track" in item) || !item.track) throw paginationError("收藏分页包含无效曲目");
      return mapTrack(item.track);
    }, "收藏分页响应缺少数据列表");
  }

  async getHistoryTracks(cursor?: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Track>> {
    const params = new URLSearchParams({ limit: String(normalizePageLimit(limit)) });
    if (cursor) params.set("cursor", cursor);
    const page = await this.api.request<{ items: Array<{ track: TrackDto }>; nextCursor?: string | null }>(`api/v1/library/history?${params}`, { signal });
    return mapCursorPage(page, (item) => {
      if (!item || typeof item !== "object" || !("track" in item) || !item.track) throw paginationError("播放历史分页包含无效曲目");
      return mapTrack(item.track);
    }, "播放历史分页响应缺少数据列表");
  }
}
