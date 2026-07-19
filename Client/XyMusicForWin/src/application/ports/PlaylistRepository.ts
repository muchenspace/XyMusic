import type { Playlist, PlaylistDetail, PlaylistVisibility } from "../../domain/music";
import type { CursorPage, PlaylistSort } from "../../domain/pagination";

export interface PlaylistRepository {
  list(sort: PlaylistSort, cursor?: string, limit?: number, signal?: AbortSignal): Promise<CursorPage<Playlist>>;
  get(playlistId: string, signal?: AbortSignal): Promise<PlaylistDetail>;
  getPage(playlistId: string, cursor?: string, limit?: number, signal?: AbortSignal): Promise<PlaylistDetail>;
  create(name: string, description: string, visibility: PlaylistVisibility): Promise<Playlist>;
  update(playlist: Playlist, changes: { name?: string; description?: string; visibility?: PlaylistVisibility }): Promise<Playlist>;
  delete(playlist: Playlist): Promise<void>;
  addTrack(playlist: Playlist, trackId: string): Promise<number>;
  removeTrack(playlist: PlaylistDetail, entryId: string): Promise<number>;
  reorder(playlist: PlaylistDetail, orderedEntryIds: string[]): Promise<number>;
}
