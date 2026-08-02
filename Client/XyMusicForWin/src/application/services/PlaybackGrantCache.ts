import type { PlaybackGrant, PlaybackQuality } from "../../domain/music";
import type { PlaybackUseCases } from "../use-cases/PlaybackUseCases";

interface CachedGrant {
  url: string;
  expiresAt: string;
  selectedQuality: string;
}

export interface PlaybackGrantResolution {
  grant: PlaybackGrant;
  refreshed: boolean;
}

export class PlaybackGrantCache {
  private readonly grants = new Map<string, CachedGrant>();
  private generation = 0;

  constructor(
    private readonly playback: PlaybackUseCases,
    private readonly maxEntries = DEFAULT_MAX_ENTRIES,
  ) {
    if (!Number.isInteger(maxEntries) || maxEntries < 1) throw new Error("Playback grant cache size must be positive");
  }

  async get(trackId: string, quality: PlaybackQuality, signal?: AbortSignal, force = false): Promise<CachedGrant> {
    return (await this.resolve(trackId, quality, signal, force)).grant;
  }

  async getForResume(trackId: string, quality: PlaybackQuality, signal?: AbortSignal): Promise<PlaybackGrantResolution> {
    return this.resolve(trackId, quality, signal, false);
  }

  private async resolve(
    trackId: string,
    quality: PlaybackQuality,
    signal?: AbortSignal,
    force = false,
  ): Promise<PlaybackGrantResolution> {
    const generation = this.generation;
    const key = cacheKey(trackId, quality);
    const cached = this.grants.get(key);
    if (!force && cached && remainsValid(cached.expiresAt)) {
      this.grants.delete(key);
      this.grants.set(key, cached);
      return { grant: cached, refreshed: false };
    }
    if (cached) this.grants.delete(key);
    const grant = await this.playback.grant(trackId, quality, signal);
    if (generation !== this.generation || signal?.aborted) return { grant, refreshed: true };
    this.grants.set(key, grant);
    while (this.grants.size > this.maxEntries) {
      const oldest = this.grants.keys().next().value as string | undefined;
      if (!oldest) break;
      this.grants.delete(oldest);
    }
    return { grant, refreshed: true };
  }

  invalidate(trackId: string, quality: PlaybackQuality): void {
    this.grants.delete(cacheKey(trackId, quality));
  }

  clear(): void {
    this.generation += 1;
    this.grants.clear();
  }
}

function cacheKey(trackId: string, quality: PlaybackQuality): string {
  return `${trackId}:${quality}`;
}

function remainsValid(expiresAt: string): boolean {
  if (!expiresAt.trim()) return true;
  const expires = Date.parse(expiresAt);
  return Number.isFinite(expires) && expires - Date.now() > 30_000;
}

const DEFAULT_MAX_ENTRIES = 64;
