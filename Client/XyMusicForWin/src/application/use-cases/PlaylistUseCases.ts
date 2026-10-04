import type { Playlist, PlaylistDetail, PlaylistVisibility } from "../../domain/music";
import type { PlaylistSort } from "../../domain/pagination";
import { CancellationSource } from "../ports/Cancellation";
import type { PlaylistRepository } from "../ports/PlaylistRepository";

export type PlaylistPickerOutcome =
  | { readonly kind: "success"; readonly items: Playlist[]; readonly nextCursor: string | null }
  | { readonly kind: "error"; readonly cause: unknown }
  | { readonly kind: "stale" };

export class PlaylistUseCases {
  private pickerRequest = 0;
  private pickerController: CancellationSource | null = null;

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

  /**
   * Paged picker load that owns cancellation and deduplicated merging with the
   * already visible playlists. The first page (no cursor) keeps server order
   * first; later pages append unseen items after the visible list.
   */
  async listForPicker(cursor: string | undefined, visible: readonly Playlist[]): Promise<PlaylistPickerOutcome> {
    const controller = new CancellationSource();
    this.pickerController = controller;
    const request = ++this.pickerRequest;
    try {
      const page = await this.repository.list("UPDATED_DESC", cursor, PICKER_PAGE_SIZE, controller.signal);
      if (request !== this.pickerRequest || controller.aborted) return { kind: "stale" };
      const items = cursor === undefined
        ? mergeById(page.items, visible)
        : mergeById(visible, page.items);
      return { kind: "success", items, nextCursor: page.nextCursor };
    } catch (cause) {
      if (request !== this.pickerRequest || controller.aborted) return { kind: "stale" };
      return { kind: "error", cause };
    } finally {
      if (request === this.pickerRequest) this.pickerController = null;
    }
  }

  cancelPickerLoad(): void {
    this.pickerRequest += 1;
    this.pickerController?.abort();
    this.pickerController = null;
  }
}

function mergeById(primary: readonly Playlist[], secondary: readonly Playlist[]): Playlist[] {
  const merged = new Map(primary.map((playlist) => [playlist.id, playlist]));
  for (const playlist of secondary) if (!merged.has(playlist.id)) merged.set(playlist.id, playlist);
  return [...merged.values()];
}

export const PICKER_PAGE_SIZE = 100;
