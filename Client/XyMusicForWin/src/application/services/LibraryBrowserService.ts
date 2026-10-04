import type { Playlist, PlaylistDetail, PlaylistEntry, Track } from "../../domain/music";
import type { LibraryView } from "../navigation";
import type { FavoriteSort, PlaylistSort } from "../../domain/pagination";
import type { PlaylistUseCases } from "../use-cases/PlaylistUseCases";
import { CancellationSource } from "../ports/Cancellation";

export interface LibraryListSnapshot {
  tracks: Track[];
  playlists?: Playlist[];
  nextCursor: string | null;
}

export interface LibraryPageRequest {
  readonly controller: CancellationSource;
  isCurrent(): boolean;
}

export interface LibraryPageOptions<T> {
  abortPrevious: boolean;
  load(signal: AbortSignal): Promise<T>;
  apply(value: T): void;
  onError(cause: unknown): void;
  retry: () => Promise<void>;
  onSettled(): void;
}

/** Reactive read/write bridge the store provides for playlist optimistic updates. */
export interface PlaylistMutationHost {
  currentDetail(): PlaylistDetail | null;
  isMutating(): boolean;
  setMutating(value: boolean): void;
  applyDetail(detail: PlaylistDetail): void;
  applySummary(detail: PlaylistDetail): void;
}

/**
 * Owns library list pagination races, retry bookkeeping, the bounded list
 * cache and playlist optimistic-update state. Presentation keeps only the
 * reactive projection of these results.
 */
export class LibraryBrowserService {
  private requestGeneration = 0;
  private requestController: CancellationSource | null = null;
  private retryAction: (() => Promise<void>) | null = null;
  private readonly listCache = new Map<string, LibraryListSnapshot>();

  constructor(private readonly playlists: PlaylistUseCases) {}

  beginRequest(abortPrevious: boolean): LibraryPageRequest {
    if (abortPrevious) this.requestController?.abort();
    this.requestController = null;
    const generation = ++this.requestGeneration;
    const controller = new CancellationSource();
    this.requestController = controller;
    return {
      controller,
      isCurrent: () => generation === this.requestGeneration && !controller.aborted,
    };
  }

  settle(request: LibraryPageRequest): void {
    if (this.requestController === request.controller) this.requestController = null;
  }

  async runPage<T>(options: LibraryPageOptions<T>): Promise<void> {
    const request = this.beginRequest(options.abortPrevious);
    try {
      const value = await options.load(request.controller.signal);
      if (request.isCurrent()) options.apply(value);
    } catch (cause) {
      if (request.isCurrent()) {
        this.retryAction = options.retry;
        options.onError(cause);
      }
    } finally {
      if (request.isCurrent()) {
        this.settle(request);
        options.onSettled();
      }
    }
  }

  cancelRequest(): void {
    this.requestGeneration += 1;
    this.requestController?.abort();
    this.requestController = null;
  }

  setRetry(action: () => Promise<void>): void {
    this.retryAction = action;
  }

  takeRetry(): (() => Promise<void>) | null {
    const action = this.retryAction;
    this.retryAction = null;
    return action;
  }

  clearRetry(): void {
    this.retryAction = null;
  }

  cacheList(view: LibraryView, favoriteSort: FavoriteSort, playlistSort: PlaylistSort, snapshot: LibraryListSnapshot): void {
    if (!isCacheableView(view)) return;
    const key = listCacheKey(view, favoriteSort, playlistSort);
    this.listCache.delete(key);
    this.listCache.set(key, snapshot);
    while (this.listCache.size > MAX_LIST_CACHE_ENTRIES) this.listCache.delete(this.listCache.keys().next().value!);
  }

  restoreList(view: LibraryView, favoriteSort: FavoriteSort, playlistSort: PlaylistSort): LibraryListSnapshot | null {
    if (!isCacheableView(view)) return null;
    const key = listCacheKey(view, favoriteSort, playlistSort);
    const cached = this.listCache.get(key);
    if (!cached) return null;
    this.listCache.delete(key);
    this.listCache.set(key, cached);
    return cached;
  }

  deleteListCache(view: LibraryView, favoriteSort: FavoriteSort, playlistSort: PlaylistSort): void {
    this.listCache.delete(listCacheKey(view, favoriteSort, playlistSort));
  }

  invalidateList(view: LibraryView): void {
    for (const key of this.listCache.keys()) if (key.startsWith(`${view}:`)) this.listCache.delete(key);
  }

  clearListCache(): void {
    this.listCache.clear();
  }

  setCachedFavorite(trackId: string, favorite: boolean): void {
    for (const snapshot of this.listCache.values()) {
      for (const track of snapshot.tracks) if (track.id === trackId) track.liked = favorite;
    }
  }

  async addTrack(playlist: Playlist, trackId: string): Promise<Playlist> {
    const version = await this.playlists.addTrack(playlist, trackId);
    return { ...playlist, version, trackCount: playlist.trackCount + 1 };
  }

  async removeEntries(entryIds: string[], host: PlaylistMutationHost): Promise<number> {
    if (!host.currentDetail() || host.isMutating()) return 0;
    host.setMutating(true);
    const requested = new Set(entryIds);
    let removed = 0;
    try {
      for (const entryId of requested) {
        const detail = host.currentDetail();
        if (!detail?.entries.some((entry) => entry.id === entryId)) continue;
        const version = await this.playlists.removeTrack(detail, entryId);
        removed += 1;
        const updated: PlaylistDetail = { ...detail, version, trackCount: Math.max(0, detail.trackCount - 1) };
        if (host.currentDetail() !== detail) {
          host.applySummary(updated);
          return removed;
        }
        host.applyDetail({
          ...updated,
          entries: detail.entries.filter((entry) => entry.id !== entryId),
        });
      }
      return removed;
    } finally {
      host.setMutating(false);
    }
  }

  async reorderEntries(orderedEntryIds: string[], host: PlaylistMutationHost, detailHasMore: boolean): Promise<void> {
    if (detailHasMore) return;
    const detail = host.currentDetail();
    if (!detail || host.isMutating()) return;
    const currentIds = detail.entries.map((entry) => entry.id);
    if (!isSameEntrySet(currentIds, orderedEntryIds) || currentIds.every((id, index) => id === orderedEntryIds[index])) return;
    host.setMutating(true);
    try {
      const byId = new Map(detail.entries.map((entry) => [entry.id, entry]));
      const version = await this.playlists.reorder(detail, orderedEntryIds);
      if (host.currentDetail() !== detail) {
        host.applySummary({ ...detail, version });
        return;
      }
      host.applyDetail({
        ...detail,
        version,
        entries: orderedEntryIds.map((id, index) => ({
          ...byId.get(id)!,
          position: orderedEntryIds.length - 1 - index,
        })),
      });
    } finally {
      host.setMutating(false);
    }
  }

  movedEntryIds(entries: readonly PlaylistEntry[], entryId: string, direction: -1 | 1): string[] | null {
    const ordered = entries.map((entry) => entry.id);
    const index = ordered.findIndex((id) => id === entryId);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= ordered.length) return null;
    [ordered[index], ordered[target]] = [ordered[target]!, ordered[index]!];
    return ordered;
  }
}

function isCacheableView(view: LibraryView): boolean {
  return view === "favorites" || view === "playlists";
}

function listCacheKey(view: LibraryView, favoriteSort: FavoriteSort, playlistSort: PlaylistSort): string {
  if (view === "favorites") return `${view}:${favoriteSort}`;
  if (view === "playlists") return `${view}:${playlistSort}`;
  return `${view}:default`;
}

function isSameEntrySet(currentIds: readonly string[], orderedIds: readonly string[]): boolean {
  if (currentIds.length !== orderedIds.length) return false;
  const current = new Set(currentIds);
  const ordered = new Set(orderedIds);
  return current.size === currentIds.length
    && ordered.size === orderedIds.length
    && orderedIds.every((id) => current.has(id));
}

export const MAX_LIST_CACHE_ENTRIES = 8;
