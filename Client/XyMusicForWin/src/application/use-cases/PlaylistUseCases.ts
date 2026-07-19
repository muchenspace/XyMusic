import type { PlaylistRepository } from "../ports/PlaylistRepository";
import type { Playlist, PlaylistDetail, PlaylistVisibility } from "../../domain/music";
import type { PlaylistSort } from "../../domain/pagination";

export class PlaylistUseCases {
  constructor(private readonly repository: PlaylistRepository) {}

  list(sort: PlaylistSort, cursor?: string, limit = 50, signal?: AbortSignal) { return this.repository.list(sort, cursor, limit, signal); }
  get(playlistId: string, signal?: AbortSignal) { return this.repository.get(playlistId, signal); }
  getPage(playlistId: string, cursor?: string, limit = 100, signal?: AbortSignal) { return this.repository.getPage(playlistId, cursor, limit, signal); }
  create(name: string, description: string, visibility: PlaylistVisibility) {
    return this.repository.create(name, description, visibility);
  }
  update(playlist: Playlist, changes: { name?: string; description?: string; visibility?: PlaylistVisibility }) {
    return this.repository.update(playlist, changes);
  }
  delete(playlist: Playlist) { return this.repository.delete(playlist); }
  addTrack(playlist: Playlist, trackId: string) { return this.repository.addTrack(playlist, trackId); }
  removeTrack(playlist: PlaylistDetail, entryId: string) { return this.repository.removeTrack(playlist, entryId); }
  reorder(playlist: PlaylistDetail, orderedEntryIds: string[]) { return this.repository.reorder(playlist, orderedEntryIds); }
}
