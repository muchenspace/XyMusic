import { computed, ref } from "vue";
import { defineStore } from "pinia";
import type { Album, HomeFeed, Playlist, SearchResults, SearchScope, Track } from "../../domain/music";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useHomeStore = defineStore("home", () => {
  const catalog = useApplicationServices().catalog;
  const feed = ref<HomeFeed | null>(null);
  const randomAlbums = ref<Album[]>([]);
  const randomTracks = ref<Track[]>([]);
  const search = ref("");
  const searchResults = ref<SearchResults | null>(null);
  const searchResultsQuery = ref("");
  const loading = ref(false);
  const randomAlbumsLoading = ref(false);
  const randomTracksLoading = ref(false);
  const searching = ref(false);
  const searchLoadingScope = ref<SearchScope | null>(null);
  const feedError = ref("");
  const randomAlbumsError = ref("");
  const randomTracksError = ref("");
  const searchError = ref("");
  const error = computed(() => search.value.trim() ? searchError.value : feedError.value);
  let searchTimer: number | undefined;
  let searchController: AbortController | null = null;
  let loadController: AbortController | null = null;
  let randomAlbumsController: AbortController | null = null;
  let randomTracksController: AbortController | null = null;
  let loadRequest = 0;
  let randomAlbumsRequest = 0;
  let randomTracksRequest = 0;
  let failedSearchScope: SearchScope | "initial" | null = null;
  const searchCache = new Map<string, SearchResults>();
  const favoriteOverrides = new Map<string, boolean>();

  const filteredTracks = computed(() => searchResults.value?.tracks ?? (search.value.trim() ? [] : feed.value?.tracks ?? []));

  async function load() {
    void loadRandomAlbums();
    void loadRandomTracks();
    const request = ++loadRequest;
    loadController?.abort();
    const controller = new AbortController();
    loadController = controller;
    loading.value = true;
    feedError.value = "";
    try {
      const loaded = await catalog.home(controller.signal);
      if (request !== loadRequest || controller.signal.aborted) return;
      applyFavoriteOverrides(loaded.tracks);
      feed.value = loaded;
    }
    catch (cause) {
      if (request === loadRequest && !controller.signal.aborted) feedError.value = errorMessage(cause, "加载失败");
    }
    finally {
      if (request === loadRequest) {
        loading.value = false;
        loadController = null;
      }
    }
  }

  async function loadRandomAlbums(): Promise<void> {
    const request = ++randomAlbumsRequest;
    randomAlbumsController?.abort();
    const controller = new AbortController();
    randomAlbumsController = controller;
    randomAlbumsLoading.value = true;
    randomAlbumsError.value = "";
    try {
      const albums = await catalog.randomAlbums(5, controller.signal);
      if (request === randomAlbumsRequest && !controller.signal.aborted) randomAlbums.value = albums;
    } catch (cause) {
      if (request === randomAlbumsRequest && !controller.signal.aborted) randomAlbumsError.value = errorMessage(cause, "随机专辑加载失败");
    } finally {
      if (request === randomAlbumsRequest) {
        randomAlbumsLoading.value = false;
        randomAlbumsController = null;
      }
    }
  }

  async function loadRandomTracks(): Promise<void> {
    const request = ++randomTracksRequest;
    randomTracksController?.abort();
    const controller = new AbortController();
    randomTracksController = controller;
    randomTracksLoading.value = true;
    randomTracksError.value = "";
    try {
      const tracks = await catalog.randomTracks(10, controller.signal);
      if (request === randomTracksRequest && !controller.signal.aborted) {
        applyFavoriteOverrides(tracks);
        randomTracks.value = tracks;
      }
    } catch (cause) {
      if (request === randomTracksRequest && !controller.signal.aborted) randomTracksError.value = errorMessage(cause, "随机歌曲加载失败");
    } finally {
      if (request === randomTracksRequest) {
        randomTracksLoading.value = false;
        randomTracksController = null;
      }
    }
  }

  function updateSearch(value: string) {
    search.value = value;
    searchError.value = "";
    window.clearTimeout(searchTimer);
    searchController?.abort();
    searchController = null;
    searchLoadingScope.value = null;
    failedSearchScope = null;
    const query = value.trim();
    if (!query) {
      searchResults.value = null;
      searchResultsQuery.value = "";
      searching.value = false;
      return;
    }
    const cacheKey = normalizedSearchKey(query);
    const cached = searchCache.get(cacheKey);
    if (cached) {
      searchCache.delete(cacheKey);
      searchCache.set(cacheKey, cached);
      searchResults.value = cached;
      searchResultsQuery.value = query;
      searching.value = false;
      return;
    }
    searching.value = true;
    searchTimer = window.setTimeout(() => void performSearch(query, cacheKey), 250);
  }

  async function performSearch(query: string, cacheKey: string): Promise<void> {
    const controller = new AbortController();
    searchController = controller;
    try {
      const result = await catalog.search(query, controller.signal);
      if (searchController === controller && search.value.trim() === query) {
        const normalized = { ...result, nextCursors: result.nextCursors ?? emptySearchCursors() };
        applyFavoriteOverrides(normalized.tracks);
        searchResults.value = normalized;
        searchResultsQuery.value = query;
        failedSearchScope = null;
        rememberSearchResult(cacheKey, normalized);
      }
    }
    catch (cause) {
      if (!controller.signal.aborted && search.value.trim() === query) {
        failedSearchScope = "initial";
        searchError.value = errorMessage(cause, "搜索失败");
      }
    }
    finally {
      if (searchController === controller) {
        searchController = null;
        searching.value = false;
      }
    }
  }

  function retrySearch(): void {
    const query = search.value.trim();
    const failedScope = failedSearchScope;
    if (!query || !failedScope) return;
    searchError.value = "";
    failedSearchScope = null;
    if (failedScope !== "initial") {
      void loadMoreSearch(failedScope);
      return;
    }
    window.clearTimeout(searchTimer);
    searchController?.abort();
    searchController = null;
    searching.value = true;
    void performSearch(query, normalizedSearchKey(query));
  }

  async function loadMoreSearch(scope: SearchScope): Promise<void> {
    const query = search.value.trim();
    const result = searchResults.value;
    const cursor = result?.nextCursors?.[scope];
    if (!query || searchResultsQuery.value !== query || !result || !cursor || searching.value || searchLoadingScope.value) return;
    const controller = new AbortController();
    searchController?.abort();
    searchController = controller;
    searchLoadingScope.value = scope;
    searchError.value = "";
    failedSearchScope = null;
    try {
      if (scope === "tracks") {
        const page = await catalog.searchTracks(query, cursor, 50, controller.signal);
        if (isCurrentSearch(controller, query, result)) {
          applyFavoriteOverrides(page.items);
          result.tracks = appendUnique(result.tracks, page.items);
          result.nextCursors!.tracks = page.nextCursor;
          rememberSearchResult(normalizedSearchKey(query), result);
        }
      } else if (scope === "artists") {
        const page = await catalog.searchArtists(query, cursor, 50, controller.signal);
        if (isCurrentSearch(controller, query, result)) {
          result.artists = appendUnique(result.artists, page.items);
          result.nextCursors!.artists = page.nextCursor;
          rememberSearchResult(normalizedSearchKey(query), result);
        }
      } else {
        const page = await catalog.searchAlbums(query, cursor, 50, controller.signal);
        if (isCurrentSearch(controller, query, result)) {
          result.albums = appendUnique(result.albums, page.items);
          result.nextCursors!.albums = page.nextCursor;
          rememberSearchResult(normalizedSearchKey(query), result);
        }
      }
    } catch (cause) {
      if (!controller.signal.aborted) {
        failedSearchScope = scope;
        searchError.value = errorMessage(cause, "加载更多搜索结果失败");
      }
    } finally {
      if (searchController === controller) {
        searchController = null;
        searchLoadingScope.value = null;
      }
    }
  }

  function isCurrentSearch(controller: AbortController, query: string, result: SearchResults): boolean {
    return searchController === controller && !controller.signal.aborted && search.value.trim() === query && searchResults.value === result;
  }

  function rememberSearchResult(key: string, result: SearchResults): void {
    searchCache.delete(key);
    searchCache.set(key, result);
    while (searchCache.size > MAX_SEARCH_CACHE_ENTRIES) searchCache.delete(searchCache.keys().next().value!);
  }

  function setFavorite(trackId: string, favorite: boolean) {
    favoriteOverrides.set(trackId, favorite);
    const update = (tracks: Track[]) => {
      const track = tracks.find((item) => item.id === trackId);
      if (track) track.liked = favorite;
    };
    if (feed.value) update(feed.value.tracks);
    update(randomTracks.value);
    if (searchResults.value) update(searchResults.value.tracks);
    for (const result of searchCache.values()) update(result.tracks);
  }

  function setPlaylists(playlists: Playlist[]) {
    if (feed.value) feed.value.playlists = [...playlists];
  }

  function reset() {
    loadRequest += 1;
    loadController?.abort();
    loadController = null;
    randomAlbumsRequest += 1;
    randomAlbumsController?.abort();
    randomAlbumsController = null;
    randomTracksRequest += 1;
    randomTracksController?.abort();
    randomTracksController = null;
    searchController?.abort();
    searchController = null;
    window.clearTimeout(searchTimer);
    feed.value = null;
    randomAlbums.value = [];
    randomTracks.value = [];
    search.value = "";
    searchResults.value = null;
    searchResultsQuery.value = "";
    loading.value = false;
    randomAlbumsLoading.value = false;
    randomTracksLoading.value = false;
    searching.value = false;
    searchLoadingScope.value = null;
    feedError.value = "";
    randomAlbumsError.value = "";
    randomTracksError.value = "";
    searchError.value = "";
    failedSearchScope = null;
    searchCache.clear();
    favoriteOverrides.clear();
  }

  function applyFavoriteOverrides(tracks: Track[]): void {
    for (const track of tracks) {
      const favorite = favoriteOverrides.get(track.id);
      if (favorite !== undefined) track.liked = favorite;
    }
  }

  return { feed, randomAlbums, randomTracks, search, searchResults, searchResultsQuery, loading, randomAlbumsLoading, randomTracksLoading, searching, searchLoadingScope, feedError, randomAlbumsError, randomTracksError, searchError, error, filteredTracks, load, loadRandomAlbums, loadRandomTracks, updateSearch, retrySearch, loadMoreSearch, setFavorite, setPlaylists, reset };
});

function emptySearchCursors() { return { tracks: null, artists: null, albums: null }; }

function appendUnique<T extends { id: string }>(current: T[], incoming: T[]): T[] {
  const seen = new Set(current.map((item) => item.id));
  return [...current, ...incoming.filter((item) => !seen.has(item.id) && seen.add(item.id))];
}

function normalizedSearchKey(query: string): string { return query.trim().toLocaleLowerCase(); }

const MAX_SEARCH_CACHE_ENTRIES = 10;
