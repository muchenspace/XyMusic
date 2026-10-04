import { computed, onScopeDispose, ref } from "vue";
import { defineStore } from "pinia";
import type { Album, HomeFeed, Playlist, SearchResults, SearchScope, Track } from "../../domain/music";
import type { SearchProjection } from "../../application/services/HomeFeedService";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useHomeStore = defineStore("home", () => {
  const { homeFeed } = useApplicationServices();
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
  const favoriteOverrides = new Map<string, boolean>();

  const filteredTracks = computed(() => searchResults.value?.tracks ?? (search.value.trim() ? [] : feed.value?.tracks ?? []));

  const projection: SearchProjection = {
    query: () => search.value.trim(),
    results: () => searchResults.value,
    resultsQuery: () => searchResultsQuery.value,
    applyResults: (results, query) => {
      searchResults.value = results;
      searchResultsQuery.value = query;
    },
    clearResults: () => {
      searchResults.value = null;
      searchResultsQuery.value = "";
    },
    setSearching: (value) => { searching.value = value; },
    setLoadingScope: (scope) => { searchLoadingScope.value = scope; },
    isSearching: () => searching.value,
    loadingScope: () => searchLoadingScope.value,
    showError: (cause, fallback) => { searchError.value = errorMessage(cause, fallback); },
    clearError: () => { searchError.value = ""; },
    applyFavoriteOverrides: (tracks) => applyFavoriteOverrides(tracks),
  };

  async function load() {
    void loadRandomAlbums();
    void loadRandomTracks();
    loading.value = true;
    feedError.value = "";
    const outcome = await homeFeed.loadHome();
    if (outcome.kind === "stale") return;
    if (outcome.kind === "error") {
      feedError.value = errorMessage(outcome.cause, "加载失败");
      loading.value = false;
      return;
    }
    applyFavoriteOverrides(outcome.value.tracks);
    feed.value = outcome.value;
    loading.value = false;
  }

  async function loadRandomAlbums(): Promise<void> {
    randomAlbumsLoading.value = true;
    randomAlbumsError.value = "";
    const outcome = await homeFeed.loadRandomAlbums();
    if (outcome.kind === "stale") return;
    if (outcome.kind === "error") {
      randomAlbumsError.value = errorMessage(outcome.cause, "随机专辑加载失败");
      randomAlbumsLoading.value = false;
      return;
    }
    randomAlbums.value = outcome.value;
    randomAlbumsLoading.value = false;
  }

  async function loadRandomTracks(): Promise<void> {
    randomTracksLoading.value = true;
    randomTracksError.value = "";
    const outcome = await homeFeed.loadRandomTracks();
    if (outcome.kind === "stale") return;
    if (outcome.kind === "error") {
      randomTracksError.value = errorMessage(outcome.cause, "随机歌曲加载失败");
      randomTracksLoading.value = false;
      return;
    }
    applyFavoriteOverrides(outcome.value);
    randomTracks.value = outcome.value;
    randomTracksLoading.value = false;
  }

  function updateSearch(value: string) {
    search.value = value;
    searchError.value = "";
    homeFeed.updateSearch(value, projection);
  }

  function retrySearch(): void {
    homeFeed.retrySearch(projection);
  }

  async function loadMoreSearch(scope: SearchScope): Promise<void> {
    await homeFeed.loadMoreSearch(scope, projection);
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
    homeFeed.updateCachedFavorites(trackId, favorite);
  }

  function setPlaylists(playlists: Playlist[]) {
    if (feed.value) feed.value.playlists = [...playlists];
  }

  function reset() {
    cancelPendingRequests();
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
    homeFeed.clearSearchCache();
    favoriteOverrides.clear();
  }

  function cancelPendingRequests(): void {
    homeFeed.cancelPending();
  }

  onScopeDispose(cancelPendingRequests);

  function applyFavoriteOverrides(tracks: Track[]): void {
    for (const track of tracks) {
      const favorite = favoriteOverrides.get(track.id);
      if (favorite !== undefined) track.liked = favorite;
    }
  }

  return { feed, randomAlbums, randomTracks, search, searchResults, searchResultsQuery, loading, randomAlbumsLoading, randomTracksLoading, searching, searchLoadingScope, feedError, randomAlbumsError, randomTracksError, searchError, error, filteredTracks, load, loadRandomAlbums, loadRandomTracks, updateSearch, retrySearch, loadMoreSearch, setFavorite, setPlaylists, reset };
});
