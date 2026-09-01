import type { ConcretePlaybackQuality, PlaybackGrant, StreamProtocol } from "../../domain/music";
import type { PlaybackUseCases } from "../use-cases/PlaybackUseCases";

interface CachedGrant {
  streamUrl: string;
  expiresAt: string;
  selectedQuality: ConcretePlaybackQuality;
  streamProtocol?: StreamProtocol;
  durationMs?: number;
  startPositionMs?: number;
  bitrate?: number;
  contentLength?: number;
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

  async get(
    trackId: string,
    quality: ConcretePlaybackQuality,
    signal?: AbortSignal,
    force = false,
    startPositionMs = 0,
  ): Promise<CachedGrant> {
    return (await this.resolve(trackId, quality, signal, force, startPositionMs)).grant;
  }

  async getForResume(trackId: string, quality: ConcretePlaybackQuality, signal?: AbortSignal, startPositionMs = 0): Promise<PlaybackGrantResolution> {
    return this.resolve(trackId, quality, signal, false, startPositionMs);
  }

  private async resolve(
    trackId: string,
    quality: ConcretePlaybackQuality,
    signal?: AbortSignal,
    force = false,
    startPositionMs = 0,
  ): Promise<PlaybackGrantResolution> {
    const generation = this.generation;
    const normalizedStartPositionMs = normalizeStartPosition(startPositionMs);
    const key = cacheKey(trackId, quality);
    const cached = this.grants.get(key);
    if (!normalizedStartPositionMs && !force && cached && remainsValid(cached.expiresAt)) {
      this.grants.delete(key);
      this.grants.set(key, cached);
      return { grant: cached, refreshed: false };
    }
    if (!normalizedStartPositionMs && cached) this.grants.delete(key);
    const grant = await this.playback.grant(trackId, quality, signal, normalizedStartPositionMs);
    if (generation !== this.generation || signal?.aborted) return { grant, refreshed: true };
    if (!normalizedStartPositionMs) this.grants.set(key, grant);
    while (this.grants.size > this.maxEntries) {
      const oldest = this.grants.keys().next().value as string | undefined;
      if (!oldest) break;
      this.grants.delete(oldest);
    }
    return { grant, refreshed: true };
  }

  invalidate(trackId: string, quality: ConcretePlaybackQuality): void {
    this.grants.delete(cacheKey(trackId, quality));
  }

  clear(): void {
    this.generation += 1;
    this.grants.clear();
  }
}

function cacheKey(trackId: string, quality: ConcretePlaybackQuality): string {
  return `${trackId}:${quality}:${streamProtocolForQuality(quality)}`;
}

function streamProtocolForQuality(quality: ConcretePlaybackQuality): StreamProtocol {
  return quality === "LOSSLESS" ? "PROGRESSIVE" : "HLS";
}

function remainsValid(expiresAt: string): boolean {
  if (!expiresAt.trim()) return true;
  const expires = Date.parse(expiresAt);
  return Number.isFinite(expires) && expires - Date.now() > 30_000;
}

function normalizeStartPosition(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.round(value) : 0;
}

const DEFAULT_MAX_ENTRIES = 64;
