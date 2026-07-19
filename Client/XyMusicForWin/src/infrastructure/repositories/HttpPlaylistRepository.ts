import type { PlaylistRepository } from "../../application/ports/PlaylistRepository";
import type { Playlist, PlaylistDetail, PlaylistVisibility } from "../../domain/music";
import type { CursorPage, PlaylistSort } from "../../domain/pagination";
import { ApiClient } from "../http/ApiClient";
import { collectContinuation, mapCursorPage, normalizePageLimit, paginationError, withCursor } from "../http/cursorPagination";
import { mapPlaylist, mapTrack } from "../http/musicMappers";
import type { PageDto, PlaylistDetailDto, PlaylistDto } from "../http/musicDtos";

export class HttpPlaylistRepository implements PlaylistRepository {
  constructor(private readonly api: ApiClient) {}

  async list(sort: PlaylistSort, cursor?: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Playlist>> {
    const params = new URLSearchParams({ sort, limit: String(normalizePageLimit(limit)) });
    if (cursor) params.set("cursor", cursor);
    const page = await this.api.request<PageDto<PlaylistDto>>(`api/v1/playlists?${params}`, { signal });
    return mapCursorPage(page, mapPlaylist, "歌单分页响应缺少数据列表");
  }

  async get(playlistId: string, signal?: AbortSignal): Promise<PlaylistDetail> {
    const path = `api/v1/playlists/${encodeURIComponent(playlistId)}?limit=100`;
    const first = await this.api.request<PlaylistDetailDto>(path, { signal });
    if (!Array.isArray(first.entries)) throw paginationError("歌单详情缺少曲目列表");
    const entries = await collectContinuation(
      { items: first.entries, nextCursor: first.nextCursor },
      async (cursor): Promise<PageDto<PlaylistDetailDto["entries"][number]>> => {
        const page = await this.api.request<PlaylistDetailDto>(withCursor(path, cursor), { signal });
        if (page.id !== first.id || !Array.isArray(page.entries)) throw paginationError("歌单分页响应无效");
        return { items: page.entries, nextCursor: page.nextCursor };
      },
      signal,
      "歌单分页响应无效",
    );
    return {
      ...mapPlaylist(first, 0),
      entries: entries.map((entry) => ({ id: entry.id, position: entry.position, track: mapTrack(entry.track) })),
      nextCursor: null,
    };
  }

  async getPage(playlistId: string, cursor?: string, limit = 100, signal?: AbortSignal): Promise<PlaylistDetail> {
    const params = new URLSearchParams({ limit: String(Math.max(1, Math.min(100, Math.round(limit)))) });
    if (cursor) params.set("cursor", cursor);
    const page = await this.api.request<PlaylistDetailDto>(`api/v1/playlists/${encodeURIComponent(playlistId)}?${params}`, { signal });
    if (!Array.isArray(page.entries)) throw paginationError("歌单详情缺少曲目列表");
    return {
      ...mapPlaylist(page, 0),
      entries: page.entries.map((entry) => ({ id: entry.id, position: entry.position, track: mapTrack(entry.track) })),
      nextCursor: page.nextCursor || null,
    };
  }

  async create(name: string, description: string, visibility: PlaylistVisibility): Promise<Playlist> {
    const result = await this.api.request<PlaylistDto>("api/v1/playlists", {
      method: "POST", headers: idempotencyHeaders(), body: JSON.stringify({ name, description: description || null, visibility }),
    });
    return mapPlaylist(result, 0);
  }

  async update(playlist: Playlist, changes: { name?: string; description?: string; visibility?: PlaylistVisibility }): Promise<Playlist> {
    const result = await this.api.request<PlaylistDto>(`api/v1/playlists/${encodeURIComponent(playlist.id)}`, {
      method: "PATCH", headers: idempotencyHeaders(), body: JSON.stringify({ expectedVersion: playlist.version, ...changes }),
    });
    return mapPlaylist(result, 0);
  }

  async delete(playlist: Playlist): Promise<void> {
    await this.api.request(`api/v1/playlists/${encodeURIComponent(playlist.id)}?expectedVersion=${playlist.version}`, {
      method: "DELETE", headers: idempotencyHeaders(),
    });
  }

  async addTrack(playlist: Playlist, trackId: string): Promise<number> {
    const result = await this.api.request<{ version: number }>(`api/v1/playlists/${encodeURIComponent(playlist.id)}/tracks`, {
      method: "POST", headers: idempotencyHeaders(), body: JSON.stringify({ expectedVersion: playlist.version, trackId, insertAfterEntryId: null }),
    });
    return result.version;
  }

  async removeTrack(playlist: PlaylistDetail, entryId: string): Promise<number> {
    const result = await this.api.request<{ version: number }>(`api/v1/playlists/${encodeURIComponent(playlist.id)}/tracks/${encodeURIComponent(entryId)}?expectedVersion=${playlist.version}`, {
      method: "DELETE", headers: idempotencyHeaders(),
    });
    return result.version;
  }

  async reorder(playlist: PlaylistDetail, orderedEntryIds: string[]): Promise<number> {
    const result = await this.api.request<{ version: number }>(`api/v1/playlists/${encodeURIComponent(playlist.id)}/tracks/order`, {
      method: "PATCH", headers: idempotencyHeaders(), body: JSON.stringify({ expectedVersion: playlist.version, orderedEntryIds }),
    });
    return result.version;
  }
}

function idempotencyHeaders(): HeadersInit {
  return { "Idempotency-Key": crypto.randomUUID() };
}
