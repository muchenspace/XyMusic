import type { Album, HomeFeed, SearchResults, SearchScope, Track } from "../../domain/music";
import { CancellationSource } from "../ports/Cancellation";
import type { TaskScheduler } from "../ports/TaskScheduler";
import type { CatalogUseCases } from "../use-cases/CatalogUseCases";

export type FeedRequestOutcome<T> =
  | { readonly kind: "success"; readonly value: T }
  | { readonly kind: "error"; readonly cause: unknown }
  | { readonly kind: "stale" };

/** Reactive bridge the store exposes so search orchestration can project results. */
export interface SearchProjection {
  query(): string;
  results(): SearchResults | null;
  resultsQuery(): string;
  applyResults(results: SearchResults, query: string): void;
  clearResults(): void;
  setSearching(value: boolean): void;
  setLoadingScope(scope: SearchScope | null): void;
  isSearching(): boolean;
  loadingScope(): SearchScope | null;
  showError(cause: unknown, fallback: string): void;
  clearError(): void;
  applyFavoriteOverrides(tracks: Track[]): void;
}

/**
 * Owns the home feed request races, the debounced search pipeline and the
 * bounded search cache. Presentation keeps only the reactive projection of
 * the returned values and the user-facing messages.
 */
export class HomeFeedService {
  private loadRequest = 0;
  private loadController: CancellationSource | null = null;
  private randomAlbumsRequest = 0;
  private randomAlbumsController: CancellationSource | null = null;
  private randomTracksRequest = 0;
  private randomTracksController: CancellationSource | null = null;
  private searchController: CancellationSource | null = null;
  private cancelSearchTimer: (() => void) | null = null;
  private failedSearchScope: SearchScope | "initial" | null = null;
  private readonly searchCache = new Map<string, SearchResults>();

  constructor(
    private readonly catalog: CatalogUseCases,
    private readonly scheduler: TaskScheduler,
  ) {}

  async loadHome(): Promise<FeedRequestOutcome<HomeFeed>> {
    const request = ++this.loadRequest;
    this.loadController?.abort();
    const controller = new CancellationSource();
    this.loadController = controller;
    try {
      const loaded = await this.catalog.home(controller.signal);
      if (request !== this.loadRequest || controller.aborted) return { kind: "stale" };
      return { kind: "success", value: loaded };
    } catch (cause) {
      if (request === this.loadRequest && !controller.aborted) return { kind: "error", cause };
      return { kind: "stale" };
    } finally {
      if (request === this.loadRequest) this.loadController = null;
    }
  }

  async loadRandomAlbums(): Promise<FeedRequestOutcome<Album[]>> {
    const request = ++this.randomAlbumsRequest;
    this.randomAlbumsController?.abort();
    const controller = new CancellationSource();
    this.randomAlbumsController = controller;
    try {
      const albums = await this.catalog.randomAlbums(5, controller.signal);
      if (request !== this.randomAlbumsRequest || controller.aborted) return { kind: "stale" };
      return { kind: "success", value: albums };
    } catch (cause) {
      if (request === this.randomAlbumsRequest && !controller.aborted) return { kind: "error", cause };
      return { kind: "stale" };
    } finally {
      if (request === this.randomAlbumsRequest) this.randomAlbumsController = null;
    }
  }

  async loadRandomTracks(): Promise<FeedRequestOutcome<Track[]>> {
    const request = ++this.randomTracksRequest;
    this.randomTracksController?.abort();
    const controller = new CancellationSource();
    this.randomTracksController = controller;
    try {
      const tracks = await this.catalog.randomTracks(10, controller.signal);
      if (request !== this.randomTracksRequest || controller.aborted) return { kind: "stale" };
      return { kind: "success", value: tracks };
    } catch (cause) {
      if (request === this.randomTracksRequest && !controller.aborted) return { kind: "error", cause };
      return { kind: "stale" };
    } finally {
      if (request === this.randomTracksRequest) this.randomTracksController = null;
    }
  }

  updateSearch(value: string, projection: SearchProjection): void {
    this.cancelSearchTimer?.();
    this.cancelSearchTimer = null;
    this.searchController?.abort();
    this.searchController = null;
    projection.setLoadingScope(null);
    this.failedSearchScope = null;
    const query = value.trim();
    if (!query) {
      projection.clearResults();
      projection.setSearching(false);
      return;
    }
    const cacheKey = normalizedSearchKey(query);
    const cached = this.searchCache.get(cacheKey);
    if (cached) {
      this.searchCache.delete(cacheKey);
      this.searchCache.set(cacheKey, cached);
      projection.applyResults(cached, query);
      projection.setSearching(false);
      return;
    }
    projection.setSearching(true);
    this.cancelSearchTimer = this.scheduler.delay(() => {
      this.cancelSearchTimer = null;
      void this.performSearch(query, cacheKey, projection);
    }, SEARCH_DEBOUNCE_MS);
  }

  retrySearch(projection: SearchProjection): void {
    const query = projection.query();
    const failedScope = this.failedSearchScope;
    if (!query || !failedScope) return;
    projection.clearError();
    this.failedSearchScope = null;
    if (failedScope !== "initial") {
      void this.loadMoreSearch(failedScope, projection);
      return;
    }
    this.cancelSearchTimer?.();
    this.cancelSearchTimer = null;
    this.searchController?.abort();
    this.searchController = null;
    projection.setSearching(true);
    void this.performSearch(query, normalizedSearchKey(query), projection);
  }

  async loadMoreSearch(scope: SearchScope, projection: SearchProjection): Promise<void> {
    const query = projection.query();
    const result = projection.results();
    const cursor = result?.nextCursors?.[scope];
    if (!query || projection.resultsQuery() !== query || !result || !cursor || projection.isSearching() || projection.loadingScope()) return;
    const controller = new CancellationSource();
    this.searchController?.abort();
    this.searchController = controller;
    projection.setLoadingScope(scope);
    projection.clearError();
    this.failedSearchScope = null;
    try {
      if (scope === "tracks") {
        const page = await this.catalog.searchTracks(query, cursor, 50, controller.signal);
        if (this.isCurrentSearch(controller, query, result, projection)) {
          projection.applyFavoriteOverrides(page.items);
          result.tracks = appendUnique(result.tracks, page.items);
          result.nextCursors!.tracks = page.nextCursor;
          this.rememberSearchResult(normalizedSearchKey(query), result);
        }
      } else if (scope === "artists") {
        const page = await this.catalog.searchArtists(query, cursor, 50, controller.signal);
        if (this.isCurrentSearch(controller, query, result, projection)) {
          result.artists = appendUnique(result.artists, page.items);
          result.nextCursors!.artists = page.nextCursor;
          this.rememberSearchResult(normalizedSearchKey(query), result);
        }
      } else {
        const page = await this.catalog.searchAlbums(query, cursor, 50, controller.signal);
        if (this.isCurrentSearch(controller, query, result, projection)) {
          result.albums = appendUnique(result.albums, page.items);
          result.nextCursors!.albums = page.nextCursor;
          this.rememberSearchResult(normalizedSearchKey(query), result);
        }
      }
    } catch (cause) {
      if (!controller.aborted) {
        this.failedSearchScope = scope;
        projection.showError(cause, "加载更多搜索结果失败");
      }
    } finally {
      if (this.searchController === controller) {
        this.searchController = null;
        projection.setLoadingScope(null);
      }
    }
  }

  updateCachedFavorites(trackId: string, favorite: boolean): void {
    for (const result of this.searchCache.values()) {
      const track = result.tracks.find((item) => item.id === trackId);
      if (track) track.liked = favorite;
    }
  }

  cancelPending(): void {
    this.loadRequest += 1;
    this.loadController?.abort();
    this.loadController = null;
    this.randomAlbumsRequest += 1;
    this.randomAlbumsController?.abort();
    this.randomAlbumsController = null;
    this.randomTracksRequest += 1;
    this.randomTracksController?.abort();
    this.randomTracksController = null;
    this.searchController?.abort();
    this.searchController = null;
    this.cancelSearchTimer?.();
    this.cancelSearchTimer = null;
    this.failedSearchScope = null;
  }

  clearSearchCache(): void {
    this.searchCache.clear();
  }

  private async performSearch(query: string, cacheKey: string, projection: SearchProjection): Promise<void> {
    const controller = new CancellationSource();
    this.searchController = controller;
    try {
      const result = await this.catalog.search(query, controller.signal);
      if (this.searchController === controller && projection.query() === query) {
        const normalized = { ...result, nextCursors: result.nextCursors ?? emptySearchCursors() };
        projection.applyFavoriteOverrides(normalized.tracks);
        projection.applyResults(normalized, query);
        this.failedSearchScope = null;
        this.rememberSearchResult(cacheKey, normalized);
      }
    } catch (cause) {
      if (!controller.aborted && projection.query() === query) {
        this.failedSearchScope = "initial";
        projection.showError(cause, "搜索失败");
      }
    } finally {
      if (this.searchController === controller) {
        this.searchController = null;
        projection.setSearching(false);
      }
    }
  }

  private isCurrentSearch(controller: CancellationSource, query: string, result: SearchResults, projection: SearchProjection): boolean {
    return this.searchController === controller
      && !controller.aborted
      && projection.query() === query
      && projection.results() === result;
  }

  private rememberSearchResult(key: string, result: SearchResults): void {
    this.searchCache.delete(key);
    this.searchCache.set(key, result);
    while (this.searchCache.size > MAX_SEARCH_CACHE_ENTRIES) this.searchCache.delete(this.searchCache.keys().next().value!);
  }
}

function emptySearchCursors() {
  return { tracks: null, artists: null, albums: null };
}

function appendUnique<T extends { id: string }>(current: T[], incoming: T[]): T[] {
  const seen = new Set(current.map((item) => item.id));
  return [...current, ...incoming.filter((item) => !seen.has(item.id) && seen.add(item.id))];
}

function normalizedSearchKey(query: string): string {
  return query.trim().toLocaleLowerCase();
}

const MAX_SEARCH_CACHE_ENTRIES = 10;
export const SEARCH_DEBOUNCE_MS = 250;
