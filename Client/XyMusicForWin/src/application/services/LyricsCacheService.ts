import type { Lyrics } from "../../domain/music";
import type { CatalogRepository } from "../ports/CatalogRepository";
import { CancellationSource } from "../ports/Cancellation";

export type LyricsLoadOutcome =
  | { readonly kind: "success"; readonly value: Lyrics | null }
  | { readonly kind: "error"; readonly cause: unknown }
  | { readonly kind: "stale" };

export interface CachedLyricsRestore {
  readonly found: boolean;
  readonly value: Lyrics | null;
}

/**
 * Owns the bounded lyrics cache (most recently used last) and the per-track
 * request race so presentation only projects the resolved value. The catalog
 * port keeps the AbortSignal boundary and infrastructure access out of
 * presentation.
 */
export class LyricsCacheService {
  private currentTrackId = "";
  private request = 0;
  private controller: CancellationSource | null = null;
  private readonly cache = new Map<string, Lyrics | null>();

  constructor(private readonly catalog: Pick<CatalogRepository, "getLyrics">) {}

  /** True while the same track already has an in-flight request. */
  isDuplicate(trackId: string): boolean {
    return this.currentTrackId === trackId && this.controller !== null;
  }

  /** Returns a cached entry (including a cached "no lyrics") without fetching. */
  restore(trackId: string): CachedLyricsRestore {
    if (!this.cache.has(trackId)) return { found: false, value: null };
    const value = this.cache.get(trackId) ?? null;
    this.cache.delete(trackId);
    this.cache.set(trackId, value);
    return { found: true, value };
  }

  async load(trackId: string): Promise<LyricsLoadOutcome> {
    this.controller?.abort();
    this.controller = null;
    const request = ++this.request;
    this.currentTrackId = trackId;
    const controller = new CancellationSource();
    this.controller = controller;
    try {
      const value = await this.catalog.getLyrics(trackId, controller.signal);
      if (request !== this.request || controller.aborted) return { kind: "stale" };
      this.remember(trackId, value);
      return { kind: "success", value };
    } catch (cause) {
      if (request !== this.request || controller.aborted) return { kind: "stale" };
      this.cache.delete(trackId);
      return { kind: "error", cause };
    } finally {
      if (request === this.request) this.controller = null;
    }
  }

  /** Aborts in-flight work without touching the cache (used on track/session changes). */
  reset(): void {
    this.request += 1;
    this.controller?.abort();
    this.controller = null;
    this.currentTrackId = "";
  }

  clear(): void {
    this.reset();
    this.cache.clear();
  }

  private remember(trackId: string, value: Lyrics | null): void {
    this.cache.delete(trackId);
    this.cache.set(trackId, value);
    while (this.cache.size > MAX_LYRICS_CACHE_ENTRIES) this.cache.delete(this.cache.keys().next().value!);
  }
}

export const MAX_LYRICS_CACHE_ENTRIES = 30;
