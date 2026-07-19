import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { Album, Artist, Playlist, PlaylistDetail, PlaylistVisibility, Track } from "../../domain/music";
import type { LibraryView } from "../../domain/navigation";
import type { FavoriteSort, PlaylistSort } from "../../domain/pagination";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useLibraryStore = defineStore("library-view", () => {
  const { catalog, library, playlists: playlistUseCases } = useApplicationServices();
  const activeView = ref<LibraryView>("discover");
  const tracks = ref<Track[]>([]);
  const playlists = ref<Playlist[]>([]);
  const selectedPlaylist = ref<PlaylistDetail | null>(null);
  const detailOpen = ref(false);
  const heading = ref("");
  const loading = ref(false);
  const loadingMore = ref(false);
  const detailLoadingMore = ref(false);
  const playlistMutating = ref(false);
  const error = ref("");
  const retryAvailable = ref(false);
  const nextCursor = ref<string | null>(null);
  const detailNextCursor = ref<string | null>(null);
  const favoriteSort = ref<FavoriteSort>("FAVORITED_DESC");
  const playlistSort = ref<PlaylistSort>("UPDATED_DESC");
  let requestId = 0;
  let requestController: AbortController | null = null;
  let retryAction: (() => Promise<void>) | null = null;
  let detailSource: { kind: "album" | "artist" | "playlist"; id: string } | null = null;
  const listCache = new Map<string, ListSnapshot>();
  const favoriteOverrides = new Map<string, boolean>();

  const visibleTracks = computed(() => tracks.value);

  async function navigate(view: LibraryView, force = false, cacheBeforeNavigate = true) {
    if (cacheBeforeNavigate) cacheCurrentList();
    requestController?.abort();
    requestController = null;
    const currentRequest = ++requestId;
    loadingMore.value = false;
    activeView.value = view;
    selectedPlaylist.value = null;
    detailOpen.value = false;
    detailSource = null;
    detailNextCursor.value = null;
    detailLoadingMore.value = false;
    tracks.value = [];
    nextCursor.value = null;
    error.value = "";
    clearRetry();
    heading.value = VIEW_TITLES[view];
    if (!force && restoreCachedList(view)) {
      loading.value = false;
      return;
    }
    loading.value = view !== "discover" && view !== "settings" && view !== "diagnostics";
    try {
      if (view === "discover" || view === "settings" || view === "diagnostics") return;
      const controller = new AbortController();
      requestController = controller;
      if (view === "recent") {
        const result = await library.history(undefined, PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) {
          tracks.value = applyFavoriteOverrides(result.items);
          nextCursor.value = result.nextCursor;
        }
      }
      if (view === "favorites") {
        const result = await library.favorites(favoriteSort.value, undefined, PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) {
          tracks.value = applyFavoriteOverrides(result.items, true);
          nextCursor.value = result.nextCursor;
        }
      }
      if (view === "playlists") {
        const result = await playlistUseCases.list(playlistSort.value, undefined, PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) {
          playlists.value = result.items;
          nextCursor.value = result.nextCursor;
        }
      }
    } catch (cause) {
      if (currentRequest === requestId && !requestController?.signal.aborted) {
        error.value = errorMessage(cause);
        setRetry(() => navigate(view, true, false));
      }
    } finally {
      if (currentRequest === requestId) {
        loading.value = false;
        requestController = null;
        if (!error.value) cacheCurrentList();
      }
    }
  }

  async function loadMore(): Promise<void> {
    const cursor = nextCursor.value;
    if (!cursor || loadingMore.value || loading.value) return;
    const view = activeView.value;
    const controller = new AbortController();
    requestController = controller;
    const currentRequest = ++requestId;
    loadingMore.value = true;
    error.value = "";
    clearRetry();
    try {
      if (view === "recent") {
        const page = await library.history(cursor, PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) { tracks.value.push(...applyFavoriteOverrides(page.items)); nextCursor.value = page.nextCursor; }
      } else if (view === "favorites") {
        const page = await library.favorites(favoriteSort.value, cursor, PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) { tracks.value.push(...applyFavoriteOverrides(page.items, true)); nextCursor.value = page.nextCursor; }
      } else if (view === "playlists") {
        const page = await playlistUseCases.list(playlistSort.value, cursor, PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) { playlists.value.push(...page.items); nextCursor.value = page.nextCursor; }
      }
    } catch (cause) {
      if (currentRequest === requestId && !controller.signal.aborted) {
        error.value = errorMessage(cause);
        setRetry(loadMore);
      }
    } finally {
      if (currentRequest === requestId) {
        loadingMore.value = false;
        requestController = null;
        if (!error.value) cacheCurrentList();
      }
    }
  }

  async function changeSort(value: string): Promise<void> {
    const view = activeView.value;
    cacheCurrentList();
    if (view === "favorites") favoriteSort.value = value as FavoriteSort;
    if (view === "playlists") playlistSort.value = value as PlaylistSort;
    listCache.delete(listCacheKey(view));
    await navigate(view, true, false);
  }

  async function openAlbum(album: Album) {
    cacheCurrentList();
    await loadTrackCollection(album.title, { kind: "album", id: album.id }, (signal) => catalog.albumTracksPage(album.id, undefined, DETAIL_PAGE_SIZE, signal));
  }
  async function openArtist(artist: Artist) {
    cacheCurrentList();
    await loadTrackCollection(artist.name, { kind: "artist", id: artist.id }, (signal) => catalog.artistTracksPage(artist.id, undefined, DETAIL_PAGE_SIZE, signal));
  }

  async function openPlaylist(playlist: Playlist) {
    cacheCurrentList();
    activeView.value = "playlists";
    requestController?.abort();
    loadingMore.value = false;
    detailLoadingMore.value = false;
    const controller = new AbortController();
    requestController = controller;
    const currentRequest = ++requestId;
    detailSource = { kind: "playlist", id: playlist.id };
    detailOpen.value = true;
    heading.value = playlist.title;
    tracks.value = [];
    selectedPlaylist.value = null;
    detailNextCursor.value = null;
    loading.value = true;
    error.value = "";
    clearRetry();
    try {
      const detail = await playlistUseCases.getPage(playlist.id, undefined, DETAIL_PAGE_SIZE, controller.signal);
      if (currentRequest === requestId && !controller.signal.aborted) {
        selectedPlaylist.value = applyPlaylistFavoriteOverrides(detail);
        tracks.value = selectedPlaylist.value.entries.map((entry) => entry.track);
        detailNextCursor.value = detail.nextCursor ?? null;
      }
    } catch (cause) {
      if (currentRequest === requestId && !controller.signal.aborted) {
        error.value = errorMessage(cause);
        setRetry(() => openPlaylist(playlist));
      }
    } finally {
      if (currentRequest === requestId) {
        loading.value = false;
        requestController = null;
      }
    }
  }

  async function loadMoreCollection(): Promise<void> {
    const source = detailSource;
    const cursor = detailNextCursor.value;
    if (!detailOpen.value || !source || !cursor || detailLoadingMore.value || loading.value) return;
    const controller = new AbortController();
    requestController = controller;
    const currentRequest = ++requestId;
    detailLoadingMore.value = true;
    error.value = "";
    clearRetry();
    try {
      if (source.kind === "album") {
        const page = await catalog.albumTracksPage(source.id, cursor, DETAIL_PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) { tracks.value.push(...applyFavoriteOverrides(page.items)); detailNextCursor.value = page.nextCursor; }
      } else if (source.kind === "artist") {
        const page = await catalog.artistTracksPage(source.id, cursor, DETAIL_PAGE_SIZE, controller.signal);
        if (currentRequest === requestId) { tracks.value.push(...applyFavoriteOverrides(page.items)); detailNextCursor.value = page.nextCursor; }
      } else {
        const page = await playlistUseCases.getPage(source.id, cursor, DETAIL_PAGE_SIZE, controller.signal);
        if (currentRequest === requestId && selectedPlaylist.value?.id === source.id) {
          const entries = page.entries.map((entry) => ({ ...entry, track: applyFavoriteOverride(entry.track) }));
          selectedPlaylist.value = {
            ...selectedPlaylist.value,
            version: page.version,
            trackCount: page.trackCount,
            entries: [...selectedPlaylist.value.entries, ...entries],
            nextCursor: page.nextCursor,
          };
          tracks.value.push(...entries.map((entry) => entry.track));
          detailNextCursor.value = page.nextCursor ?? null;
        }
      }
    } catch (cause) {
      if (currentRequest === requestId && !controller.signal.aborted) {
        error.value = errorMessage(cause);
        setRetry(loadMoreCollection);
      }
    } finally {
      if (currentRequest === requestId) {
        detailLoadingMore.value = false;
        requestController = null;
      }
    }
  }

  async function createPlaylist(name: string, description: string, visibility: PlaylistVisibility) {
    const created = await playlistUseCases.create(name, description, visibility);
    playlists.value.unshift(created);
    invalidateListCache("playlists");
    return created;
  }

  async function updatePlaylist(playlist: Playlist, name: string, description: string, visibility: PlaylistVisibility) {
    const updated = await playlistUseCases.update(playlist, { name, description, visibility });
    replacePlaylist(updated);
    if (selectedPlaylist.value?.id === updated.id) selectedPlaylist.value = { ...selectedPlaylist.value, ...updated };
    heading.value = updated.title;
    invalidateListCache("playlists");
    return updated;
  }

  async function deletePlaylist(playlist: Playlist) {
    await playlistUseCases.delete(playlist);
    playlists.value = playlists.value.filter((item) => item.id !== playlist.id);
    invalidateListCache("playlists");
    selectedPlaylist.value = null;
    tracks.value = [];
    await navigate("playlists");
  }

  async function addTrack(playlist: Playlist, trackId: string) {
    const version = await playlistUseCases.addTrack(playlist, trackId);
    replacePlaylist({ ...playlist, version, trackCount: playlist.trackCount + 1 });
    invalidateListCache("playlists");
    if (selectedPlaylist.value?.id === playlist.id) await openPlaylist({ ...playlist, version });
  }

  async function removeEntry(entryId: string) {
    await removeEntries([entryId]);
  }

  async function removeEntries(entryIds: string[]): Promise<number> {
    if (!selectedPlaylist.value || playlistMutating.value) return 0;
    const requested = new Set(entryIds);
    let removed = 0;
    playlistMutating.value = true;
    try {
      for (const entryId of requested) {
        const detail: PlaylistDetail | null = selectedPlaylist.value;
        if (!detail?.entries.some((entry) => entry.id === entryId)) continue;
        const version = await playlistUseCases.removeTrack(detail, entryId);
        removed += 1;
        const updated: PlaylistDetail = { ...detail, version, trackCount: Math.max(0, detail.trackCount - 1) };
        if (selectedPlaylist.value !== detail) {
          replacePlaylist(updated);
          invalidateListCache("playlists");
          return removed;
        }
        selectedPlaylist.value = {
          ...updated,
          entries: detail.entries.filter((entry) => entry.id !== entryId),
        };
        syncSelectedPlaylist();
      }
      return removed;
    } finally {
      playlistMutating.value = false;
    }
  }

  async function moveEntry(entryId: string, direction: -1 | 1) {
    if (detailNextCursor.value) return;
    const detail = selectedPlaylist.value;
    if (!detail) return;
    const entries = [...detail.entries];
    const index = entries.findIndex((entry) => entry.id === entryId);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= entries.length) return;
    [entries[index], entries[target]] = [entries[target]!, entries[index]!];
    await reorderEntries(entries.map((entry) => entry.id));
  }

  async function reorderEntries(orderedEntryIds: string[]): Promise<void> {
    if (detailNextCursor.value) return;
    const detail = selectedPlaylist.value;
    if (!detail || playlistMutating.value) return;
    const currentIds = detail.entries.map((entry) => entry.id);
    if (!isSameEntrySet(currentIds, orderedEntryIds) || currentIds.every((id, index) => id === orderedEntryIds[index])) return;
    playlistMutating.value = true;
    try {
      const byId = new Map(detail.entries.map((entry) => [entry.id, entry]));
      const version = await playlistUseCases.reorder(detail, orderedEntryIds);
      if (selectedPlaylist.value !== detail) {
        replacePlaylist({ ...detail, version });
        invalidateListCache("playlists");
        return;
      }
      selectedPlaylist.value = {
        ...detail,
        version,
        entries: orderedEntryIds.map((id, position) => ({ ...byId.get(id)!, position })),
      };
      syncSelectedPlaylist();
    } finally {
      playlistMutating.value = false;
    }
  }

  function syncSelectedPlaylist(): void {
    const detail = selectedPlaylist.value;
    if (!detail) return;
    tracks.value = detail.entries.map((entry) => entry.track);
    replacePlaylist(detail);
    invalidateListCache("playlists");
  }

  function removeFavorite(trackId: string) {
    tracks.value = tracks.value.filter((track) => track.id !== trackId);
    invalidateListCache("favorites");
  }

  function setFavorite(trackId: string, favorite: boolean): void {
    favoriteOverrides.set(trackId, favorite);
    updateFavorite(tracks.value, trackId, favorite);
    if (selectedPlaylist.value) updateFavorite(selectedPlaylist.value.entries.map((entry) => entry.track), trackId, favorite);
    for (const snapshot of listCache.values()) updateFavorite(snapshot.tracks, trackId, favorite);
    invalidateListCache("favorites");
  }

  function replacePlaylist(playlist: Playlist) {
    const index = playlists.value.findIndex((item) => item.id === playlist.id);
    if (index >= 0) playlists.value[index] = playlist;
  }

  function setPlaylists(value: Playlist[]): void {
    playlists.value = [...value];
    invalidateListCache("playlists");
  }

  function cacheCurrentList(): void {
    if (detailOpen.value || !isListView(activeView.value) || loading.value) return;
    const key = listCacheKey(activeView.value);
    listCache.delete(key);
    const snapshot: ListSnapshot = {
      tracks: [...tracks.value],
      nextCursor: nextCursor.value,
    };
    if (activeView.value === "playlists") snapshot.playlists = [...playlists.value];
    listCache.set(key, snapshot);
    while (listCache.size > MAX_LIST_CACHE_ENTRIES) listCache.delete(listCache.keys().next().value!);
  }

  function restoreCachedList(view: LibraryView): boolean {
    if (!isListView(view)) return false;
    const key = listCacheKey(view);
    const cached = listCache.get(key);
    if (!cached) return false;
    listCache.delete(key);
    listCache.set(key, cached);
    tracks.value = applyFavoriteOverrides(cached.tracks, view === "favorites");
    if (view === "playlists" && cached.playlists) playlists.value = [...cached.playlists];
    nextCursor.value = cached.nextCursor;
    return true;
  }

  function invalidateListCache(view: LibraryView): void {
    for (const key of listCache.keys()) if (key.startsWith(`${view}:`)) listCache.delete(key);
  }

  function listCacheKey(view: LibraryView): string {
    if (view === "favorites") return `${view}:${favoriteSort.value}`;
    if (view === "playlists") return `${view}:${playlistSort.value}`;
    return `${view}:default`;
  }

  async function loadTrackCollection(title: string, source: { kind: "album" | "artist"; id: string }, loader: (signal: AbortSignal) => Promise<{ items: Track[]; nextCursor: string | null }>) {
    requestController?.abort();
    loadingMore.value = false;
    detailLoadingMore.value = false;
    const controller = new AbortController();
    requestController = controller;
    const currentRequest = ++requestId;
    detailOpen.value = true;
    detailSource = source;
    heading.value = title;
    tracks.value = [];
    selectedPlaylist.value = null;
    detailNextCursor.value = null;
    loading.value = true;
    error.value = "";
    clearRetry();
    try {
      const result = await loader(controller.signal);
      if (currentRequest === requestId && !controller.signal.aborted) {
        tracks.value = applyFavoriteOverrides(result.items);
        detailNextCursor.value = result.nextCursor;
      }
    }
    catch (cause) {
      if (currentRequest === requestId && !controller.signal.aborted) {
        error.value = errorMessage(cause);
        setRetry(() => loadTrackCollection(title, source, loader));
      }
    }
    finally {
      if (currentRequest === requestId) {
        loading.value = false;
        requestController = null;
      }
    }
  }

  function cancelPending(): void {
    requestId += 1;
    requestController?.abort();
    requestController = null;
    loading.value = false;
    loadingMore.value = false;
    detailLoadingMore.value = false;
  }

  async function retry(): Promise<void> {
    const action = retryAction;
    if (!action) return;
    clearRetry();
    error.value = "";
    await action();
  }

  function setRetry(action: () => Promise<void>): void {
    retryAction = action;
    retryAvailable.value = true;
  }

  function clearRetry(): void {
    retryAction = null;
    retryAvailable.value = false;
  }

  function reset() {
    cancelPending();
    activeView.value = "discover";
    tracks.value = [];
    playlists.value = [];
    selectedPlaylist.value = null;
    detailOpen.value = false;
    heading.value = "";
    loading.value = false;
    loadingMore.value = false;
    playlistMutating.value = false;
    nextCursor.value = null;
    detailNextCursor.value = null;
    detailSource = null;
    clearRetry();
    listCache.clear();
    favoriteOverrides.clear();
    error.value = "";
  }

  function applyFavoriteOverrides(items: Track[], favoritesOnly = false): Track[] {
    const updated = items.map(applyFavoriteOverride);
    return favoritesOnly ? updated.filter((track) => favoriteOverrides.get(track.id) !== false) : updated;
  }

  function applyFavoriteOverride(track: Track): Track {
    const favorite = favoriteOverrides.get(track.id);
    if (favorite !== undefined) track.liked = favorite;
    return track;
  }

  function applyPlaylistFavoriteOverrides(detail: PlaylistDetail): PlaylistDetail {
    return {
      ...detail,
      entries: detail.entries.map((entry) => ({ ...entry, track: applyFavoriteOverride(entry.track) })),
    };
  }

  return { activeView, tracks, playlists, selectedPlaylist, detailOpen, heading, loading, loadingMore, detailLoadingMore, playlistMutating, error, retryAvailable, nextCursor, detailNextCursor, favoriteSort, playlistSort, visibleTracks, navigate, retry, loadMore, loadMoreCollection, changeSort, openAlbum, openArtist, openPlaylist, createPlaylist, updatePlaylist, deletePlaylist, addTrack, removeEntry, removeEntries, moveEntry, reorderEntries, removeFavorite, setFavorite, setPlaylists, cancelPending, reset };
});

const VIEW_TITLES: Record<LibraryView, string> = { discover: "发现音乐", recent: "最近播放", favorites: "喜欢的音乐", playlists: "我的歌单", settings: "设置", diagnostics: "诊断" };
const PAGE_SIZE = 50;
const DETAIL_PAGE_SIZE = 100;
const MAX_LIST_CACHE_ENTRIES = 8;

interface ListSnapshot {
  tracks: Track[];
  playlists?: Playlist[];
  nextCursor: string | null;
}

function isListView(view: LibraryView): boolean {
  return view === "favorites" || view === "playlists";
}

function isSameEntrySet(currentIds: string[], orderedIds: string[]): boolean {
  if (currentIds.length !== orderedIds.length) return false;
  const current = new Set(currentIds);
  const ordered = new Set(orderedIds);
  return current.size === currentIds.length
    && ordered.size === orderedIds.length
    && orderedIds.every((id) => current.has(id));
}

function updateFavorite(items: Track[], trackId: string, favorite: boolean): void {
  for (const track of items) if (track.id === trackId) track.liked = favorite;
}
