import { computed, onScopeDispose, ref } from "vue";
import { defineStore } from "pinia";
import type { Album, Artist, Playlist, PlaylistDetail, PlaylistEntry, PlaylistVisibility, Track } from "../../domain/music";
import type { LibraryView } from "../../application/navigation";
import type { FavoriteSort, PlaylistSort } from "../../domain/pagination";
import type { LibraryListSnapshot, PlaylistMutationHost } from "../../application/services/LibraryBrowserService";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useLibraryStore = defineStore("library-view", () => {
  const { catalog, library, playlists: playlistUseCases, libraryBrowser } = useApplicationServices();
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
  let detailSource: { kind: "album" | "artist" | "playlist"; id: string } | null = null;
  const favoriteOverrides = new Map<string, boolean>();
  const playlistMutationHost: PlaylistMutationHost = {
    currentDetail: () => selectedPlaylist.value,
    isMutating: () => playlistMutating.value,
    setMutating: (value) => { playlistMutating.value = value; },
    applyDetail: (detail) => {
      selectedPlaylist.value = detail;
      syncSelectedPlaylist();
    },
    applySummary: (detail) => {
      replacePlaylist(detail);
      libraryBrowser.invalidateList("playlists");
    },
  };

  const visibleTracks = computed(() => tracks.value);

  async function navigate(view: LibraryView, force = false, cacheBeforeNavigate = true) {
    if (cacheBeforeNavigate) cacheCurrentList();
    libraryBrowser.cancelRequest();
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
    loading.value = view !== "discover" && view !== "settings";
    if (view === "discover" || view === "settings") return;
    await libraryBrowser.runPage<LibraryPageResult>({
      abortPrevious: true,
      load: (signal) => loadLibraryPage(view, undefined, signal),
      apply: (page) => applyLibraryPage(view, page),
      onError: (cause) => {
        error.value = errorMessage(cause);
        setRetry(() => navigate(view, true, false));
      },
      retry: () => navigate(view, true, false),
      onSettled: () => {
        loading.value = false;
        if (!error.value) cacheCurrentList();
      },
    });
  }

  async function loadMore(): Promise<void> {
    const cursor = nextCursor.value;
    if (!cursor || loadingMore.value || loading.value) return;
    const view = activeView.value;
    loadingMore.value = true;
    error.value = "";
    clearRetry();
    await libraryBrowser.runPage<LibraryPageResult>({
      abortPrevious: false,
      load: (signal) => loadLibraryPage(view, cursor, signal),
      apply: (page) => appendLibraryPage(view, page),
      onError: (cause) => {
        error.value = errorMessage(cause);
        setRetry(loadMore);
      },
      retry: loadMore,
      onSettled: () => {
        loadingMore.value = false;
        if (!error.value) cacheCurrentList();
      },
    });
  }

  async function changeSort(value: string): Promise<void> {
    const view = activeView.value;
    cacheCurrentList();
    if (view === "favorites") favoriteSort.value = value as FavoriteSort;
    if (view === "playlists") playlistSort.value = value as PlaylistSort;
    libraryBrowser.deleteListCache(view, favoriteSort.value, playlistSort.value);
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
    loadingMore.value = false;
    detailLoadingMore.value = false;
    detailSource = { kind: "playlist", id: playlist.id };
    detailOpen.value = true;
    heading.value = playlist.title;
    tracks.value = [];
    selectedPlaylist.value = null;
    detailNextCursor.value = null;
    loading.value = true;
    error.value = "";
    clearRetry();
    await libraryBrowser.runPage<PlaylistDetail>({
      abortPrevious: true,
      load: (signal) => playlistUseCases.getPage(playlist.id, undefined, DETAIL_PAGE_SIZE, signal),
      apply: (detail) => {
        const applied = applyPlaylistFavoriteOverrides(detail);
        selectedPlaylist.value = applied;
        tracks.value = applied.entries.map((entry) => entry.track);
        detailNextCursor.value = detail.nextCursor ?? null;
      },
      onError: (cause) => {
        error.value = errorMessage(cause);
        setRetry(() => openPlaylist(playlist));
      },
      retry: () => openPlaylist(playlist),
      onSettled: () => { loading.value = false; },
    });
  }

  async function loadMoreCollection(): Promise<void> {
    const source = detailSource;
    const cursor = detailNextCursor.value;
    if (!detailOpen.value || !source || !cursor || detailLoadingMore.value || loading.value) return;
    detailLoadingMore.value = true;
    error.value = "";
    clearRetry();
    await libraryBrowser.runPage<CollectionPage>({
      abortPrevious: false,
      load: (signal) => loadCollectionPage(source, cursor, signal),
      apply: (page) => applyCollectionPage(source, page),
      onError: (cause) => {
        error.value = errorMessage(cause);
        setRetry(loadMoreCollection);
      },
      retry: loadMoreCollection,
      onSettled: () => { detailLoadingMore.value = false; },
    });
  }

  async function createPlaylist(name: string, description: string, visibility: PlaylistVisibility) {
    const created = await playlistUseCases.create(name, description, visibility);
    playlists.value.unshift(created);
    libraryBrowser.invalidateList("playlists");
    return created;
  }

  async function updatePlaylist(playlist: Playlist, name: string, description: string, visibility: PlaylistVisibility) {
    const updated = await playlistUseCases.update(playlist, { name, description, visibility });
    replacePlaylist(updated);
    if (selectedPlaylist.value?.id === updated.id) selectedPlaylist.value = { ...selectedPlaylist.value, ...updated };
    heading.value = updated.title;
    libraryBrowser.invalidateList("playlists");
    return updated;
  }

  async function deletePlaylist(playlist: Playlist) {
    await playlistUseCases.delete(playlist);
    playlists.value = playlists.value.filter((item) => item.id !== playlist.id);
    libraryBrowser.invalidateList("playlists");
    selectedPlaylist.value = null;
    tracks.value = [];
    await navigate("playlists");
  }

  async function addTrack(playlist: Playlist, trackId: string) {
    const updated = await libraryBrowser.addTrack(playlist, trackId);
    replacePlaylist(updated);
    libraryBrowser.invalidateList("playlists");
    if (selectedPlaylist.value?.id === playlist.id) await openPlaylist(updated);
  }

  async function removeEntry(entryId: string) {
    await removeEntries([entryId]);
  }

  async function removeEntries(entryIds: string[]): Promise<number> {
    return libraryBrowser.removeEntries(entryIds, playlistMutationHost);
  }

  async function moveEntry(entryId: string, direction: -1 | 1) {
    if (detailNextCursor.value) return;
    const detail = selectedPlaylist.value;
    if (!detail) return;
    const orderedEntryIds = libraryBrowser.movedEntryIds(detail.entries, entryId, direction);
    if (!orderedEntryIds) return;
    await reorderEntries(orderedEntryIds);
  }

  async function reorderEntries(orderedEntryIds: string[]): Promise<void> {
    await libraryBrowser.reorderEntries(orderedEntryIds, playlistMutationHost, Boolean(detailNextCursor.value));
  }

  function syncSelectedPlaylist(): void {
    const detail = selectedPlaylist.value;
    if (!detail) return;
    tracks.value = detail.entries.map((entry) => entry.track);
    replacePlaylist(detail);
    libraryBrowser.invalidateList("playlists");
  }

  function removeFavorite(trackId: string) {
    tracks.value = tracks.value.filter((track) => track.id !== trackId);
    libraryBrowser.invalidateList("favorites");
  }

  function setFavorite(trackId: string, favorite: boolean): void {
    favoriteOverrides.set(trackId, favorite);
    updateFavorite(tracks.value, trackId, favorite);
    if (selectedPlaylist.value) updateFavorite(selectedPlaylist.value.entries.map((entry) => entry.track), trackId, favorite);
    libraryBrowser.setCachedFavorite(trackId, favorite);
    libraryBrowser.invalidateList("favorites");
  }

  function replacePlaylist(playlist: Playlist) {
    const index = playlists.value.findIndex((item) => item.id === playlist.id);
    if (index >= 0) playlists.value[index] = playlist;
  }

  function setPlaylists(value: Playlist[]): void {
    playlists.value = [...value];
    libraryBrowser.invalidateList("playlists");
  }

  function cacheCurrentList(): void {
    if (detailOpen.value || loading.value) return;
    const view = activeView.value;
    const snapshot: LibraryListSnapshot = {
      tracks: [...tracks.value],
      nextCursor: nextCursor.value,
    };
    if (view === "playlists") snapshot.playlists = [...playlists.value];
    libraryBrowser.cacheList(view, favoriteSort.value, playlistSort.value, snapshot);
  }

  function restoreCachedList(view: LibraryView): boolean {
    const cached = libraryBrowser.restoreList(view, favoriteSort.value, playlistSort.value);
    if (!cached) return false;
    tracks.value = applyFavoriteOverrides(cached.tracks, view === "favorites");
    if (view === "playlists" && cached.playlists) playlists.value = [...cached.playlists];
    nextCursor.value = cached.nextCursor;
    return true;
  }

  async function loadTrackCollection(title: string, source: { kind: "album" | "artist"; id: string }, loader: (signal: AbortSignal) => Promise<{ items: Track[]; nextCursor: string | null }>) {
    loadingMore.value = false;
    detailLoadingMore.value = false;
    detailOpen.value = true;
    detailSource = source;
    heading.value = title;
    tracks.value = [];
    selectedPlaylist.value = null;
    detailNextCursor.value = null;
    loading.value = true;
    error.value = "";
    clearRetry();
    await libraryBrowser.runPage<{ items: Track[]; nextCursor: string | null }>({
      abortPrevious: true,
      load: (signal) => loader(signal),
      apply: (result) => {
        tracks.value = applyFavoriteOverrides(result.items);
        detailNextCursor.value = result.nextCursor;
      },
      onError: (cause) => {
        error.value = errorMessage(cause);
        setRetry(() => loadTrackCollection(title, source, loader));
      },
      retry: () => loadTrackCollection(title, source, loader),
      onSettled: () => { loading.value = false; },
    });
  }

  async function loadLibraryPage(view: LibraryView, cursor: string | undefined, signal: AbortSignal): Promise<LibraryPageResult> {
    if (view === "recent") {
      const page = await library.history(cursor, PAGE_SIZE, signal);
      return { tracks: page.items, playlists: null, nextCursor: page.nextCursor };
    }
    if (view === "favorites") {
      const page = await library.favorites(favoriteSort.value, cursor, PAGE_SIZE, signal);
      return { tracks: page.items, playlists: null, nextCursor: page.nextCursor };
    }
    const page = await playlistUseCases.list(playlistSort.value, cursor, PAGE_SIZE, signal);
    return { tracks: [], playlists: page.items, nextCursor: page.nextCursor };
  }

  function applyLibraryPage(view: LibraryView, page: LibraryPageResult): void {
    if (view === "playlists") {
      playlists.value = page.playlists ?? [];
      nextCursor.value = page.nextCursor;
      return;
    }
    tracks.value = applyFavoriteOverrides(page.tracks, view === "favorites");
    nextCursor.value = page.nextCursor;
  }

  function appendLibraryPage(view: LibraryView, page: LibraryPageResult): void {
    if (view === "playlists") {
      playlists.value.push(...page.playlists ?? []);
      nextCursor.value = page.nextCursor;
      return;
    }
    tracks.value.push(...applyFavoriteOverrides(page.tracks, view === "favorites"));
    nextCursor.value = page.nextCursor;
  }

  async function loadCollectionPage(
    source: { kind: "album" | "artist" | "playlist"; id: string },
    cursor: string,
    signal: AbortSignal,
  ): Promise<CollectionPage> {
    if (source.kind === "album") {
      const page = await catalog.albumTracksPage(source.id, cursor, DETAIL_PAGE_SIZE, signal);
      return { kind: "tracks", items: page.items, nextCursor: page.nextCursor };
    }
    if (source.kind === "artist") {
      const page = await catalog.artistTracksPage(source.id, cursor, DETAIL_PAGE_SIZE, signal);
      return { kind: "tracks", items: page.items, nextCursor: page.nextCursor };
    }
    return { kind: "playlist", detail: await playlistUseCases.getPage(source.id, cursor, DETAIL_PAGE_SIZE, signal) };
  }

  function applyCollectionPage(source: { kind: "album" | "artist" | "playlist"; id: string }, page: CollectionPage): void {
    if (page.kind === "tracks") {
      tracks.value.push(...applyFavoriteOverrides(page.items));
      detailNextCursor.value = page.nextCursor;
      return;
    }
    if (selectedPlaylist.value?.id !== source.id) return;
    const entries = page.detail.entries.map((entry) => ({ ...entry, track: applyFavoriteOverride(entry.track) }));
    const mergedEntries = sortPlaylistEntriesNewestFirst([...selectedPlaylist.value.entries, ...entries]);
    selectedPlaylist.value = {
      ...selectedPlaylist.value,
      version: page.detail.version,
      trackCount: page.detail.trackCount,
      entries: mergedEntries,
      nextCursor: page.detail.nextCursor,
    };
    tracks.value = mergedEntries.map((entry) => entry.track);
    detailNextCursor.value = page.detail.nextCursor ?? null;
  }

  function cancelPending(): void {
    libraryBrowser.cancelRequest();
    loading.value = false;
    loadingMore.value = false;
    detailLoadingMore.value = false;
  }

  onScopeDispose(cancelPending);

  async function retry(): Promise<void> {
    const action = libraryBrowser.takeRetry();
    if (!action) return;
    retryAvailable.value = false;
    error.value = "";
    await action();
  }

  function setRetry(action: () => Promise<void>): void {
    libraryBrowser.setRetry(action);
    retryAvailable.value = true;
  }

  function clearRetry(): void {
    libraryBrowser.clearRetry();
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
    libraryBrowser.clearListCache();
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
      entries: sortPlaylistEntriesNewestFirst(detail.entries)
        .map((entry) => ({ ...entry, track: applyFavoriteOverride(entry.track) })),
    };
  }

  return { activeView, tracks, playlists, selectedPlaylist, detailOpen, heading, loading, loadingMore, detailLoadingMore, playlistMutating, error, retryAvailable, nextCursor, detailNextCursor, favoriteSort, playlistSort, visibleTracks, navigate, retry, loadMore, loadMoreCollection, changeSort, openAlbum, openArtist, openPlaylist, createPlaylist, updatePlaylist, deletePlaylist, addTrack, removeEntry, removeEntries, moveEntry, reorderEntries, removeFavorite, setFavorite, setPlaylists, cancelPending, reset };
});

const VIEW_TITLES: Record<LibraryView, string> = { discover: "发现音乐", recent: "最近播放", favorites: "喜欢的音乐", playlists: "我的歌单", settings: "设置" };
const PAGE_SIZE = 50;
const DETAIL_PAGE_SIZE = 100;

interface LibraryPageResult {
  tracks: Track[];
  playlists: Playlist[] | null;
  nextCursor: string | null;
}

type CollectionPage =
  | { kind: "tracks"; items: Track[]; nextCursor: string | null }
  | { kind: "playlist"; detail: PlaylistDetail };

function sortPlaylistEntriesNewestFirst(entries: PlaylistEntry[]): PlaylistEntry[] {
  return [...entries].sort((left, right) => right.position - left.position);
}

function updateFavorite(items: Track[], trackId: string, favorite: boolean): void {
  for (const track of items) if (track.id === trackId) track.liked = favorite;
}
